package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// startAction sets the busy state with a status message and batches the given
// command with a spinner tick.  It is the 3-line setup repeated by every
// action in handleNormalMode.
func (m detailModel) startAction(msg string, cmd tea.Cmd) (detailModel, tea.Cmd) {
	m.actionBusy = true
	m.statusMsg = msg
	m.statusErr = false
	return m, tea.Batch(cmd, m.spinner.Tick)
}

func (m detailModel) handleInputNameMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = detailNormal
		m.input.Reset()
		m.input.Blur()
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.input.Value())
		m.input.Reset()
		m.input.Blur()
		m.mode = detailNormal
		if name != "" {
			m.actionBusy = true
			m.statusMsg = fmt.Sprintf("Creating snapshot %q...", name)
			m.statusErr = false
			return m, tea.Batch(m.createSnapshotCmd(name), m.spinner.Tick)
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

func (m detailModel) handleConfirmDeleteMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		name := m.selectedSnapshotName()
		m.mode = detailNormal
		if name == "" {
			return m, nil
		}
		m.actionBusy = true
		m.statusMsg = fmt.Sprintf("Deleting snapshot %q...", name)
		m.statusErr = false
		return m, tea.Batch(m.deleteSnapshotCmd(name), m.spinner.Tick)
	case "esc":
		m.mode = detailNormal
		return m, nil
	}
	return m, nil
}

func (m detailModel) handleConfirmRollbackMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		name := m.selectedSnapshotName()
		m.mode = detailNormal
		if name == "" {
			return m, nil
		}
		m.actionBusy = true
		m.statusMsg = fmt.Sprintf("Rolling back to %q...", name)
		m.statusErr = false
		return m, tea.Batch(m.rollbackSnapshotCmd(name), m.spinner.Tick)
	case "esc":
		m.mode = detailNormal
		return m, nil
	}
	return m, nil
}

func (m detailModel) handleConfirmDeleteBackupMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		volid, storage := m.selectedBackupInfo()
		m.mode = detailNormal
		if volid == "" {
			return m, nil
		}
		m.actionBusy = true
		m.statusMsg = "Deleting backup..."
		m.statusErr = false
		return m, tea.Batch(m.deleteBackupCmd(volid, storage), m.spinner.Tick)
	case "esc":
		m.mode = detailNormal
		return m, nil
	}
	return m, nil
}

func (m detailModel) handleConfirmDeleteResourceMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = fmt.Sprintf("Deleting %s %d...", m.typeStr(), m.resource.VMID)
		m.statusErr = false
		return m, tea.Batch(m.deleteResourceCmd(), m.spinner.Tick)
	case "esc":
		m.mode = detailNormal
		return m, nil
	}
	return m, nil
}

func (m detailModel) handleConfirmTemplateMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = fmt.Sprintf("Converting %s %d to template...", m.typeStr(), m.resource.VMID)
		m.statusErr = false
		return m, tea.Batch(m.convertToTemplateCmd(), m.spinner.Tick)
	case "esc":
		m.mode = detailNormal
		return m, nil
	}
	return m, nil
}

func (m detailModel) handleCloneInputMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = detailNormal
		m.cloneIDInput.Reset()
		m.cloneNameInput.Reset()
		m.cloneIDInput.Blur()
		m.cloneNameInput.Blur()
		m.statusMsg = ""
		m.statusErr = false
		m.loading = true
		m.loadErr = nil
		m.backupLoading = true
		m.backupLoadErr = nil
		return m, tea.Batch(m.loadSnapshotsCmd(), m.loadBackupsCmd(), m.refreshResourceCmd(), m.spinner.Tick)
	case "tab", "down":
		if m.cloneField == 0 {
			m.cloneField = 1
			m.cloneIDInput.Blur()
			m.cloneNameInput.Focus()
			return m, textinput.Blink
		}
		m.cloneField = 0
		m.cloneNameInput.Blur()
		m.cloneIDInput.Focus()
		return m, textinput.Blink
	case "shift+tab", "up":
		if m.cloneField == 1 {
			m.cloneField = 0
			m.cloneNameInput.Blur()
			m.cloneIDInput.Focus()
			return m, textinput.Blink
		}
		m.cloneField = 1
		m.cloneIDInput.Blur()
		m.cloneNameInput.Focus()
		return m, textinput.Blink
	case "enter":
		if m.cloneField == 0 {
			idStr := strings.TrimSpace(m.cloneIDInput.Value())
			if idStr == "" {
				return m, nil
			}
			vmid, err := strconv.Atoi(idStr)
			if err != nil || vmid < 100 {
				m.statusMsg = "Invalid VMID (must be >= 100)"
				m.statusErr = true
				return m, nil
			}
			_ = vmid
			m.cloneField = 1
			m.cloneIDInput.Blur()
			m.cloneNameInput.Focus()
			return m, textinput.Blink
		}
		idStr := strings.TrimSpace(m.cloneIDInput.Value())
		name := strings.TrimSpace(m.cloneNameInput.Value())
		m.cloneIDInput.Blur()
		m.cloneNameInput.Blur()
		vmid, _ := strconv.Atoi(idStr)
		if name == "" {
			name = m.resource.Name + "-clone"
		}
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = "Cloning..."
		m.statusErr = false
		return m, tea.Batch(m.cloneResourceCmd(vmid, name), m.spinner.Tick)
	default:
		var cmd tea.Cmd
		if m.cloneField == 0 {
			m.cloneIDInput, cmd = m.cloneIDInput.Update(msg)
		} else {
			m.cloneNameInput, cmd = m.cloneNameInput.Update(msg)
		}
		return m, cmd
	}
}

func (m detailModel) handleRestoreInputIDMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = detailNormal
		m.restoreIDInput.Blur()
		m.restoreNameInput.Blur()
		m.statusMsg = ""
		m.statusErr = false
		return m, nil
	case "tab", "down":
		if m.restoreField == 0 {
			m.restoreField = 1
			m.restoreIDInput.Blur()
			m.restoreNameInput.Focus()
			return m, textinput.Blink
		}
		m.restoreField = 0
		m.restoreNameInput.Blur()
		m.restoreIDInput.Focus()
		return m, textinput.Blink
	case "shift+tab", "up":
		if m.restoreField == 1 {
			m.restoreField = 0
			m.restoreNameInput.Blur()
			m.restoreIDInput.Focus()
			return m, textinput.Blink
		}
		m.restoreField = 1
		m.restoreIDInput.Blur()
		m.restoreNameInput.Focus()
		return m, textinput.Blink
	case "enter":
		idStr := strings.TrimSpace(m.restoreIDInput.Value())
		name := strings.TrimSpace(m.restoreNameInput.Value())
		m.restoreIDInput.Blur()
		m.restoreNameInput.Blur()
		if idStr == "" {
			return m, nil
		}
		vmid, err := strconv.Atoi(idStr)
		if err != nil || vmid < 100 {
			m.statusMsg = "Invalid VMID (must be >= 100)"
			m.statusErr = true
			return m, nil
		}
		m.pendingRestoreID = vmid
		m.pendingRestoreName = name
		m.mode = detailRestoreSelectStorage
		m.actionBusy = true
		m.statusMsg = "Loading storages..."
		m.statusErr = false
		return m, tea.Batch(m.loadRestoreStoragesCmd(), m.spinner.Tick)
	default:
		var cmd tea.Cmd
		if m.restoreField == 0 {
			m.restoreIDInput, cmd = m.restoreIDInput.Update(msg)
		} else {
			m.restoreNameInput, cmd = m.restoreNameInput.Update(msg)
		}
		return m, cmd
	}
}

func (m detailModel) handleSelectBackupStorageMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.storageIdx > 0 {
			m.storageIdx--
		}
		return m, nil
	case "down", "j":
		if m.storageIdx < len(m.availableStorages)-1 {
			m.storageIdx++
		}
		return m, nil
	case "enter":
		storageName := m.availableStorages[m.storageIdx].Name
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = "Creating backup..."
		m.statusErr = false
		return m, tea.Batch(m.createBackupCmd(storageName), m.spinner.Tick)
	case "esc":
		m.mode = detailNormal
		m.actionBusy = false
		return m, nil
	}
	return m, nil
}

func (m detailModel) handleRestoreSelectStorageMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.storageIdx > 0 {
			m.storageIdx--
		}
		return m, nil
	case "down", "j":
		if m.storageIdx < len(m.availableStorages)-1 {
			m.storageIdx++
		}
		return m, nil
	case "enter":
		storageName := m.availableStorages[m.storageIdx].Name
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = "Restoring backup..."
		m.statusErr = false
		return m, tea.Batch(m.restoreBackupCmd(m.pendingVolid, m.pendingRestoreID, m.pendingRestoreName, storageName), m.spinner.Tick)
	case "esc":
		m.mode = detailNormal
		m.actionBusy = false
		m.pendingVolid = ""
		return m, nil
	}
	return m, nil
}

func (m detailModel) handleResizeDiskMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = detailNormal
		m.resizeDiskInput.Reset()
		m.resizeSizeInput.Reset()
		m.resizeDiskInput.Blur()
		m.resizeSizeInput.Blur()
		m.statusMsg = ""
		m.statusErr = false
		return m, nil
	case "tab", "down":
		if m.resizeDiskField == 0 {
			m.resizeDiskField = 1
			m.resizeDiskInput.Blur()
			m.resizeSizeInput.Focus()
			return m, textinput.Blink
		}
		m.resizeDiskField = 0
		m.resizeSizeInput.Blur()
		m.resizeDiskInput.Focus()
		return m, textinput.Blink
	case "shift+tab", "up":
		if m.resizeDiskField == 1 {
			m.resizeDiskField = 0
			m.resizeSizeInput.Blur()
			m.resizeDiskInput.Focus()
			return m, textinput.Blink
		}
		m.resizeDiskField = 1
		m.resizeDiskInput.Blur()
		m.resizeSizeInput.Focus()
		return m, textinput.Blink
	case "enter":
		if m.resizeDiskField == 0 {
			if strings.TrimSpace(m.resizeDiskInput.Value()) == "" {
				return m, nil
			}
			m.resizeDiskField = 1
			m.resizeDiskInput.Blur()
			m.resizeSizeInput.Focus()
			return m, textinput.Blink
		}
		disk := strings.TrimSpace(m.resizeDiskInput.Value())
		size := strings.TrimSpace(m.resizeSizeInput.Value())
		m.resizeDiskInput.Blur()
		m.resizeSizeInput.Blur()
		if disk == "" || size == "" {
			m.mode = detailNormal
			return m, nil
		}
		if !strings.HasPrefix(size, "+") {
			size = "+" + size
		}
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = fmt.Sprintf("Resizing disk %s by %s...", disk, size)
		m.statusErr = false
		return m, tea.Batch(m.resizeDiskCmd(disk, size), m.spinner.Tick)
	default:
		var cmd tea.Cmd
		if m.resizeDiskField == 0 {
			m.resizeDiskInput, cmd = m.resizeDiskInput.Update(msg)
		} else {
			m.resizeSizeInput, cmd = m.resizeSizeInput.Update(msg)
		}
		return m, cmd
	}
}

func (m detailModel) handleTagManageMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	if m.actionBusy {
		return m, nil
	}
	tags := parseTags(m.resource.Tags)
	// Bug fix: clamp cursor correctly when the tag list empties.
	if len(tags) == 0 {
		m.tagIdx = 0
	} else if m.tagIdx >= len(tags) {
		m.tagIdx = len(tags) - 1
	}
	switch msg.String() {
	case "esc":
		m.mode = detailNormal
		return m, nil
	case "a":
		m.actionBusy = true
		m.statusMsg = "Fetching instance tags..."
		m.statusErr = false
		return m, tea.Batch(m.loadAllTagsCmd(), m.spinner.Tick)
	case "up", "k":
		if m.tagIdx > 0 {
			m.tagIdx--
		}
		return m, nil
	case "down", "j":
		if m.tagIdx < len(tags)-1 {
			m.tagIdx++
		}
		return m, nil
	case "d", "backspace":
		if len(tags) == 0 {
			return m, nil
		}
		tag := tags[m.tagIdx]
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = fmt.Sprintf("Removing tag %q...", tag)
		m.statusErr = false
		return m, tea.Batch(m.removeTagCmd(tag), m.spinner.Tick)
	}
	return m, nil
}

func (m detailModel) handleTagSelectMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	total := len(m.instanceTags) + 1
	switch msg.String() {
	case "esc":
		m.mode = detailTagManage
		return m, nil
	case "up", "k":
		if m.tagSelectIdx > 0 {
			m.tagSelectIdx--
		}
		return m, nil
	case "down", "j":
		if m.tagSelectIdx < total-1 {
			m.tagSelectIdx++
		}
		return m, nil
	case "enter":
		if m.tagSelectIdx == len(m.instanceTags) {
			m.tagInput.Reset()
			m.tagInput.Focus()
			m.mode = detailTagAdd
			return m, textinput.Blink
		}
		tag := m.instanceTags[m.tagSelectIdx]
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = fmt.Sprintf("Adding tag %q...", tag)
		m.statusErr = false
		return m, tea.Batch(m.addTagCmd(tag), m.spinner.Tick)
	}
	return m, nil
}

func (m detailModel) handleTagAddMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.tagInput.Reset()
		m.tagInput.Blur()
		m.mode = detailTagManage
		return m, nil
	case "enter":
		tag := strings.TrimSpace(m.tagInput.Value())
		m.tagInput.Reset()
		m.tagInput.Blur()
		if tag == "" {
			m.mode = detailTagManage
			return m, nil
		}
		if !tagInputRegex.MatchString(tag) {
			m.statusMsg = fmt.Sprintf("invalid tag %q: use letters, digits, hyphens, underscores, dots", tag)
			m.statusErr = true
			m.mode = detailNormal
			return m, nil
		}
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = fmt.Sprintf("Adding tag %q...", tag)
		m.statusErr = false
		return m, tea.Batch(m.addTagCmd(tag), m.spinner.Tick)
	default:
		var cmd tea.Cmd
		m.tagInput, cmd = m.tagInput.Update(msg)
		return m, cmd
	}
}

func (m detailModel) handleEditConfigMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = detailNormal
		m.editNameInput.Blur()
		m.editDescInput.Blur()
		m.statusMsg = ""
		m.statusErr = false
		return m, nil
	case "tab", "down":
		if m.editField == 0 {
			m.editField = 1
			m.editNameInput.Blur()
			m.editDescInput.Focus()
			return m, textinput.Blink
		}
		m.editField = 0
		m.editDescInput.Blur()
		m.editNameInput.Focus()
		return m, textinput.Blink
	case "shift+tab", "up":
		if m.editField == 1 {
			m.editField = 0
			m.editDescInput.Blur()
			m.editNameInput.Focus()
			return m, textinput.Blink
		}
		m.editField = 1
		m.editNameInput.Blur()
		m.editDescInput.Focus()
		return m, textinput.Blink
	case "enter":
		if m.editField == 0 {
			name := strings.TrimSpace(m.editNameInput.Value())
			if name == "" {
				m.statusMsg = "Name cannot be empty"
				m.statusErr = true
				return m, nil
			}
			m.editField = 1
			m.editNameInput.Blur()
			m.editDescInput.Focus()
			return m, textinput.Blink
		}
		name := strings.TrimSpace(m.editNameInput.Value())
		desc := m.editDescInput.Value()
		m.editNameInput.Blur()
		m.editDescInput.Blur()
		if name == "" {
			m.statusMsg = "Name cannot be empty"
			m.statusErr = true
			return m, nil
		}
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = "Updating config..."
		m.statusErr = false
		return m, tea.Batch(m.updateConfigCmd(name, desc), m.spinner.Tick)
	default:
		var cmd tea.Cmd
		if m.editField == 0 {
			m.editNameInput, cmd = m.editNameInput.Update(msg)
		} else {
			m.editDescInput, cmd = m.editDescInput.Update(msg)
		}
		return m, cmd
	}
}

func (m detailModel) handleSelectMoveDiskMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.diskMoveIdx > 0 {
			m.diskMoveIdx--
		}
	case "down", "j":
		if m.diskMoveIdx < len(m.diskMoveKeys)-1 {
			m.diskMoveIdx++
		}
	case "enter":
		m.pendingMoveDisk = m.diskMoveKeys[m.diskMoveIdx]
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = "Loading storages..."
		return m, tea.Batch(m.loadMoveStoragesCmd(), m.spinner.Tick)
	case "esc":
		m.mode = detailNormal
	}
	return m, nil
}

func (m detailModel) handleSelectMoveStorageMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.moveStorageIdx > 0 {
			m.moveStorageIdx--
		}
	case "down", "j":
		if m.moveStorageIdx < len(m.moveStorages)-1 {
			m.moveStorageIdx++
		}
	case "enter":
		storageName := m.moveStorages[m.moveStorageIdx].Name
		m.mode = detailNormal
		m.actionBusy = true
		m.statusMsg = fmt.Sprintf("Moving disk %s...", m.pendingMoveDisk)
		return m, tea.Batch(m.moveDiskCmd(m.pendingMoveDisk, storageName), m.spinner.Tick)
	case "esc":
		m.mode = detailNormal
		m.pendingMoveDisk = ""
	}
	return m, nil
}

// handleNormalMode handles key events in detailNormal mode.  It includes table
// delegation for unmatched keys so that arrow-key navigation continues to work
// when no action overlay is active.
func (m detailModel) handleNormalMode(msg tea.KeyMsg) (detailModel, tea.Cmd) {
	// Filter input mode.
	if m.activeTab == 0 && m.snapFilter.active {
		var rebuild bool
		m.snapFilter, rebuild = m.snapFilter.handleKey(msg)
		if rebuild {
			m = m.withRebuiltSnapTable()
		}
		return m, nil
	}
	if m.activeTab == 1 && m.backupFilter.active {
		var rebuild bool
		m.backupFilter, rebuild = m.backupFilter.handleKey(msg)
		if rebuild {
			m = m.withRebuiltBackupTable()
		}
		return m, nil
	}

	switch msg.String() {
	case "/":
		if m.activeTab == 0 {
			m.snapFilter.active = true
		} else {
			m.backupFilter.active = true
		}
		return m, nil
	case "ctrl+u":
		if m.activeTab == 0 && m.snapFilter.hasActiveFilter() {
			m.snapFilter.text = ""
			m = m.withRebuiltSnapTable()
		} else if m.activeTab == 1 && m.backupFilter.hasActiveFilter() {
			m.backupFilter.text = ""
			m = m.withRebuiltBackupTable()
		}
		return m, nil
	case "s":
		return m.startAction("Starting...", m.powerCmd("start"))
	case "S":
		return m.startAction("Stopping...", m.powerCmd("stop"))
	case "U":
		return m.startAction("Shutting down...", m.powerCmd("shutdown"))
	case "R":
		return m.startAction("Rebooting...", m.powerCmd("reboot"))
	case "c":
		return m, m.loadCloneNextIDCmd()
	case "D":
		m.mode = detailConfirmDeleteResource
		return m, nil
	case "T":
		if m.resource.Template == 1 {
			m.statusMsg = "Already a template"
			m.statusErr = true
			return m, nil
		}
		m.mode = detailConfirmTemplate
		return m, nil
	case "E":
		return m.startAction("Loading config...", m.loadConfigCmd())
	case "alt+z", "Ω":
		defaultDisk := "scsi0"
		if m.resource.Type == "lxc" {
			defaultDisk = "rootfs"
		}
		m.resizeDiskInput.SetValue(defaultDisk)
		m.resizeSizeInput.Reset()
		m.resizeDiskField = 1
		m.resizeDiskInput.Blur()
		m.resizeSizeInput.Focus()
		m.mode = detailResizeDisk
		return m, textinput.Blink
	case "alt+t", "†":
		tags := parseTags(m.resource.Tags)
		if m.tagIdx >= len(tags) {
			m.tagIdx = 0
		}
		m.mode = detailTagManage
		return m, nil
	case "alt+m", "µ":
		return m.startAction("Loading disk info...", m.loadDisksCmd())
	case "ctrl+r", "f5":
		m.loading = true
		m.loadErr = nil
		m.backupLoading = true
		m.backupLoadErr = nil
		m.statusMsg = ""
		m.statusErr = false
		return m, tea.Batch(m.loadSnapshotsCmd(), m.loadBackupsCmd(), m.refreshResourceCmd(), m.spinner.Tick)
	case "tab":
		if m.activeTab == 0 {
			m.activeTab = 1
		} else {
			m.activeTab = 0
		}
		return m, nil
	}

	// Tab-specific keys in normal mode.
	if m.activeTab == 0 {
		switch msg.String() {
		case "alt+s", "ß":
			m.mode = detailInputName
			m.input.Focus()
			return m, textinput.Blink
		case "alt+d", "∂":
			if len(m.snapshots) == 0 {
				return m, nil
			}
			m.mode = detailConfirmDelete
			return m, nil
		case "alt+r", "®":
			if len(m.snapshots) == 0 {
				return m, nil
			}
			m.mode = detailConfirmRollback
			return m, nil
		case "esc":
			return m, nil
		}
	} else {
		switch msg.String() {
		case "alt+b", "∫":
			m.mode = detailSelectBackupStorage
			m.actionBusy = true
			m.statusMsg = "Loading storages..."
			m.statusErr = false
			return m, tea.Batch(m.loadBackupStoragesCmd(), m.spinner.Tick)
		case "alt+d", "∂":
			if len(m.backups) == 0 {
				return m, nil
			}
			m.mode = detailConfirmDeleteBackup
			return m, nil
		case "alt+r", "®":
			if len(m.backups) == 0 {
				return m, nil
			}
			volid, _ := m.selectedBackupInfo()
			m.pendingVolid = volid
			return m, m.loadNextIDCmd()
		case "esc":
			return m, nil
		}
	}

	// Unmatched key: delegate to the active tab's table for navigation.
	var cmd tea.Cmd
	if m.activeTab == 0 {
		m.snapTable, cmd = m.snapTable.Update(msg)
	} else {
		m.backupTable, cmd = m.backupTable.Update(msg)
	}
	return m, cmd
}
