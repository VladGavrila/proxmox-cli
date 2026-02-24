package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// typeStr returns "VM" for qemu resources and "CT" for lxc resources.
func (m detailModel) typeStr() string {
	if m.resource.Type == "lxc" {
		return "CT"
	}
	return "VM"
}

func (m detailModel) view() string {
	if m.width == 0 {
		return ""
	}

	r := m.resource

	var statusStyled string
	if r.Status == "running" {
		statusStyled = StyleRunning.Render(r.Status)
	} else {
		statusStyled = StyleStopped.Render(r.Status)
	}

	title := StyleTitle.Render(fmt.Sprintf("%s %d: %s", m.typeStr(), r.VMID, r.Name)) +
		"  " + statusStyled

	diskLoc := m.diskLocation
	if diskLoc == "" {
		diskLoc = "…"
	}
	stats := StyleSubtitle.Render(fmt.Sprintf(
		"Node: %s   Disk: %s   CPU: %s   Mem: %s / %s   Uptime: %s",
		r.Node,
		diskLoc,
		formatPercent(r.CPU),
		formatBytes(r.Mem),
		formatBytes(r.MaxMem),
		formatUptime(r.Uptime),
	))

	sep := StyleDim.Render(strings.Repeat("─", m.width-6))

	var lines []string
	lines = append(lines, headerLine(title, m.width, m.lastRefreshed))
	lines = append(lines, stats)

	if m.agentAvailable && m.agentOsInfo != nil {
		agentLine := StyleDim.Render("OS: ") + StyleAgent.Render(m.agentOsInfo.PrettyName)
		if ip := m.primaryAgentIP(); ip != "" {
			agentLine += StyleDim.Render("   IP: ") + StyleAgent.Render(ip)
		}
		lines = append(lines, agentLine)
	}

	if r.Tags != "" {
		tags := parseTags(r.Tags)
		lines = append(lines, StyleDim.Render("Tags: ")+StyleTag.Render(strings.Join(tags, ", ")))
	}

	lines = append(lines, renderHelp("[s] start  [S] stop  [U] shutdown  [R] reboot  [c] clone  [D] delete  [T] template  [E] edit"))
	lines = append(lines, renderHelp("[Alt+z] resize disk  [Alt+m] move disk  [Alt+t] tags"))
	lines = append(lines, sep)

	// Tab bar
	lines = append(lines, m.renderTabBar())
	lines = append(lines, "")

	// Tab content
	if m.activeTab == 0 {
		lines = append(lines, m.viewSnapshotsTab()...)
	} else {
		lines = append(lines, m.viewBackupsTab()...)
	}

	// Status/spinner feedback line.
	switch {
	case m.actionBusy:
		lines = append(lines, StyleWarning.Render(m.spinner.View()+" "+m.statusMsg))
	case m.statusMsg != "" && m.statusErr:
		lines = append(lines, StyleError.Render(m.statusMsg))
	case m.statusMsg != "":
		lines = append(lines, StyleSuccess.Render(m.statusMsg))
	default:
		lines = append(lines, "")
	}

	// Overlay modes.
	lines = append(lines, m.viewOverlay()...)

	lines = append(lines, renderHelp("[Esc] back   [Q] quit"))

	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (m detailModel) renderTabBar() string {
	var snapLabel, backupLabel string
	if m.snapFilter.hasActiveFilter() {
		snapLabel = fmt.Sprintf("Snapshots (%d/%d)", len(m.filteredSnapIndices), len(m.snapshots))
	} else {
		snapLabel = fmt.Sprintf("Snapshots (%d)", len(m.snapshots))
	}
	if m.backupFilter.hasActiveFilter() {
		backupLabel = fmt.Sprintf("Backups (%d/%d)", len(m.filteredBackupIndices), len(m.backups))
	} else {
		backupLabel = fmt.Sprintf("Backups (%d)", len(m.backups))
	}

	if m.activeTab == 0 {
		return StyleTitle.Render(snapLabel) + "  " + StyleDim.Render(backupLabel) +
			"                " + renderHelp("[Tab] switch")
	}
	return StyleDim.Render(snapLabel) + "  " + StyleTitle.Render(backupLabel) +
		"                " + renderHelp("[Tab] switch")
}

func (m detailModel) viewSnapshotsTab() []string {
	var lines []string
	switch {
	case m.loading:
		lines = append(lines, StyleWarning.Render(m.spinner.View()+" Loading snapshots..."))
	case m.loadErr != nil:
		lines = append(lines, StyleError.Render("  Error: "+m.loadErr.Error()))
		lines = append(lines, renderHelp("  [ctrl+r] retry"))
	case len(m.snapshots) == 0:
		lines = append(lines, StyleDim.Render("  No snapshots"))
	default:
		lines = append(lines, m.snapTable.View())
	}
	// Filter line.
	if fl := m.snapFilter.renderLine(); fl != "" {
		lines = append(lines, fl)
	} else {
		lines = append(lines, "")
	}
	return lines
}

func (m detailModel) viewBackupsTab() []string {
	var lines []string
	switch {
	case m.backupLoading:
		lines = append(lines, StyleWarning.Render(m.spinner.View()+" Loading backups..."))
	case m.backupLoadErr != nil:
		lines = append(lines, StyleError.Render("  Error: "+m.backupLoadErr.Error()))
		lines = append(lines, renderHelp("  [ctrl+r] retry"))
	case len(m.backups) == 0:
		lines = append(lines, StyleDim.Render("  No backups"))
	default:
		lines = append(lines, m.backupTable.View())
	}
	// Filter line.
	if fl := m.backupFilter.renderLine(); fl != "" {
		lines = append(lines, fl)
	} else {
		lines = append(lines, "")
	}
	return lines
}

func (m detailModel) viewOverlay() []string {
	var lines []string

	switch m.mode {
	case detailInputName:
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render("New snapshot name: ")+m.input.View())
		lines = append(lines, renderHelp("[Enter] confirm   [Esc] cancel"))

	case detailConfirmDelete:
		snapName := m.selectedSnapshotName()
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Delete snapshot %q? [Enter] confirm   [Esc] cancel", snapName),
		))

	case detailConfirmRollback:
		snapName := m.selectedSnapshotName()
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Rollback to %q? [Enter] confirm   [Esc] cancel", snapName),
		))

	case detailConfirmDeleteBackup:
		volid, _ := m.selectedBackupInfo()
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Delete backup %q? [Enter] confirm   [Esc] cancel", volid),
		))

	case detailConfirmDeleteResource:
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Delete %s %d (%s)? This cannot be undone. [Enter] confirm   [Esc] cancel", m.typeStr(), m.resource.VMID, m.resource.Name),
		))

	case detailConfirmTemplate:
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Convert %s %d (%s) to a template? This cannot be undone. [Enter] confirm   [Esc] cancel", m.typeStr(), m.resource.VMID, m.resource.Name),
		))

	case detailCloneInput:
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(fmt.Sprintf("Clone %s %d (%s)", m.typeStr(), m.resource.VMID, m.resource.Name)))
		idLabel := StyleDim.Render("  VMID: ")
		nameLabel := StyleDim.Render("  Name: ")
		if m.cloneField == 0 {
			idLabel = StyleWarning.Render("> VMID: ")
		} else {
			nameLabel = StyleWarning.Render("> Name: ")
		}
		lines = append(lines, idLabel+m.cloneIDInput.View())
		lines = append(lines, nameLabel+m.cloneNameInput.View())
		lines = append(lines, renderHelp("[Tab] switch field  [Enter] confirm  [Esc] cancel"))

	case detailRestoreInputID:
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render("Restore backup"))
		idLabel := StyleDim.Render("  VMID: ")
		nameLabel := StyleDim.Render("  Name: ")
		if m.restoreField == 0 {
			idLabel = StyleWarning.Render("> VMID: ")
		} else {
			nameLabel = StyleWarning.Render("> Name: ")
		}
		lines = append(lines, idLabel+m.restoreIDInput.View())
		lines = append(lines, nameLabel+m.restoreNameInput.View())
		lines = append(lines, renderHelp("[Tab] switch field  [Enter] confirm  [Esc] cancel"))

	case detailSelectBackupStorage:
		if len(m.availableStorages) > 0 {
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render("Select backup storage:"))
			for i, s := range m.availableStorages {
				cursor := "  "
				if i == m.storageIdx {
					cursor = "> "
				}
				lines = append(lines, StyleWarning.Render(
					fmt.Sprintf("%s%s (%s free, %s)", cursor, s.Name, s.Avail, s.Type),
				))
			}
			lines = append(lines, renderHelp("[Enter] select  [Esc] cancel"))
		}

	case detailTagManage:
		tags := parseTags(m.resource.Tags)
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render("Manage tags:"))
		if len(tags) == 0 {
			lines = append(lines, StyleDim.Render("  (none)"))
		} else {
			for i, t := range tags {
				if i == m.tagIdx {
					lines = append(lines, StyleWarning.Render("> ")+StyleTag.Render(t))
				} else {
					lines = append(lines, "  "+StyleTag.Render(t))
				}
			}
		}
		lines = append(lines, renderHelp("[a] add  [d/backspace] remove selected  [Esc] close"))

	case detailTagSelect:
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render("Select a tag to add:"))
		for i, t := range m.instanceTags {
			if i == m.tagSelectIdx {
				lines = append(lines, StyleWarning.Render("> ")+StyleTag.Render(t))
			} else {
				lines = append(lines, "  "+StyleTag.Render(t))
			}
		}
		newCursor := "  "
		if m.tagSelectIdx == len(m.instanceTags) {
			newCursor = "> "
		}
		lines = append(lines, StyleDim.Render(newCursor+"New tag..."))
		lines = append(lines, renderHelp("[Enter] select  [Esc] back"))

	case detailTagAdd:
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render("New tag: ")+m.tagInput.View())
		lines = append(lines, renderHelp("[Enter] add  [Esc] back"))

	case detailResizeDisk:
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(fmt.Sprintf("Resize disk on %s %d (%s)", m.typeStr(), m.resource.VMID, m.resource.Name)))
		diskLabel := StyleDim.Render("  Disk: ")
		sizeLabel := StyleDim.Render("  Size: ")
		if m.resizeDiskField == 0 {
			diskLabel = StyleWarning.Render("> Disk: ")
		} else {
			sizeLabel = StyleWarning.Render("> Size: ")
		}
		lines = append(lines, diskLabel+m.resizeDiskInput.View())
		lines = append(lines, sizeLabel+m.resizeSizeInput.View())
		lines = append(lines, renderHelp("[Tab] switch field  [Enter] confirm  [Esc] cancel"))

	case detailRestoreSelectStorage:
		if len(m.availableStorages) > 0 {
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render("Select target storage for restored disks:"))
			for i, s := range m.availableStorages {
				cursor := "  "
				if i == m.storageIdx {
					cursor = "> "
				}
				lines = append(lines, StyleWarning.Render(
					fmt.Sprintf("%s%s (%s free, %s)", cursor, s.Name, s.Avail, s.Type),
				))
			}
			lines = append(lines, renderHelp("[Enter] select  [Esc] cancel"))
		}

	case detailSelectMoveDisk:
		diskLabel := "disk"
		if m.resource.Type == "lxc" {
			diskLabel = "volume"
		}
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render("Select "+diskLabel+" to move:"))
		for i, k := range m.diskMoveKeys {
			cursor := "  "
			if i == m.diskMoveIdx {
				cursor = "> "
			}
			lines = append(lines, StyleWarning.Render(fmt.Sprintf("%s%s  %s", cursor, k, m.availableDisks[k])))
		}
		lines = append(lines, renderHelp("[↑/↓] navigate   [Enter] select   [Esc] cancel"))

	case detailSelectMoveStorage:
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(fmt.Sprintf("Select target storage for %s:", m.pendingMoveDisk)))
		for i, s := range m.moveStorages {
			cursor := "  "
			if i == m.moveStorageIdx {
				cursor = "> "
			}
			lines = append(lines, StyleWarning.Render(fmt.Sprintf("%s%s (%s free, %s)", cursor, s.Name, s.Avail, s.Type)))
		}
		lines = append(lines, renderHelp("[↑/↓] navigate   [Enter] select   [Esc] cancel"))

	case detailEditConfig:
		nameLabel := "Name"
		if m.resource.Type == "lxc" {
			nameLabel = "Hostname"
		}
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(fmt.Sprintf("Edit %s %d config", m.typeStr(), m.resource.VMID)))
		nLabel := StyleDim.Render(fmt.Sprintf("  %s: ", nameLabel))
		dLabel := StyleDim.Render("  Description: ")
		if m.editField == 0 {
			nLabel = StyleWarning.Render(fmt.Sprintf("> %s: ", nameLabel))
		} else {
			dLabel = StyleWarning.Render("> Description: ")
		}
		lines = append(lines, nLabel+m.editNameInput.View())
		lines = append(lines, dLabel+m.editDescInput.View())
		lines = append(lines, renderHelp("[Tab] switch field  [Enter] confirm  [Esc] cancel"))

	default:
		lines = append(lines, "")
		if m.activeTab == 0 {
			if len(m.snapshots) > 0 {
				lines = append(lines, renderHelp("[Alt+s] new  [Alt+d] delete  [Alt+r] rollback  [/] filter  |  [ctrl+r] refresh"))
			} else {
				lines = append(lines, renderHelp("[Alt+s] new snapshot"))
			}
		} else {
			if len(m.backups) > 0 {
				lines = append(lines, renderHelp("[Alt+b] backup  [Alt+d] delete  [Alt+r] restore  [/] filter  |  [ctrl+r] refresh"))
			} else {
				lines = append(lines, renderHelp("[Alt+b] new backup"))
			}
		}
	}

	return lines
}

// primaryAgentIP returns the first non-loopback IPv4 address from agent interfaces.
func (m detailModel) primaryAgentIP() string {
	for _, iface := range m.agentNetIfaces {
		if iface.Name == "lo" {
			continue
		}
		for _, addr := range iface.IPAddresses {
			if addr.IPAddressType == "ipv4" {
				return addr.IPAddress
			}
		}
	}
	return ""
}

// formatUptime converts seconds to a human-readable uptime string.
func formatUptime(seconds uint64) string {
	if seconds == 0 {
		return "-"
	}
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
