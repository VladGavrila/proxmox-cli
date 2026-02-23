package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/chupakbra/proxmox-cli/internal/actions"
)

func (m detailModel) loadSnapshotsCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var entries []snapshotEntry

		if r.Type == "qemu" {
			snaps, err := actions.VMSnapshots(ctx, c, int(r.VMID), r.Node)
			if err != nil {
				return detailLoadedMsg{err: err}
			}
			for _, s := range snaps {
				if s.Name == "current" {
					continue
				}
				entries = append(entries, snapshotEntry{
					Name:        s.Name,
					Parent:      s.Parent,
					Description: s.Description,
					Date:        formatSnapTime(s.Snaptime),
				})
			}
		} else {
			snaps, err := actions.ContainerSnapshots(ctx, c, int(r.VMID), r.Node)
			if err != nil {
				return detailLoadedMsg{err: err}
			}
			for _, s := range snaps {
				if s.Name == "current" {
					continue
				}
				entries = append(entries, snapshotEntry{
					Name:        s.Name,
					Parent:      s.Parent,
					Description: s.Description,
					Date:        formatSnapTime(s.SnapshotCreationTime),
				})
			}
		}
		return detailLoadedMsg{snapshots: entries}
	}
}

func (m detailModel) loadBackupsCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		list, err := actions.ListBackups(ctx, c, r.Node, "", int(r.VMID))
		if err != nil {
			return backupsLoadedMsg{err: err}
		}
		var entries []backupEntry
		for _, b := range list {
			entries = append(entries, backupEntry{
				Volid:   b.Volid,
				Type:    b.Type,
				Size:    formatBytes(b.Size),
				Date:    formatSnapTime(b.Ctime),
				Notes:   b.Notes,
				Storage: b.Storage,
				Node:    b.Node,
				VMID:    fmt.Sprintf("%d", b.VMID),
			})
		}
		return backupsLoadedMsg{backups: entries}
	}
}

func (m detailModel) loadBackupStoragesCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		storages, err := actions.ListBackupStorages(ctx, c, r.Node)
		if err != nil {
			return backupStoragesLoadedMsg{err: err}
		}
		var choices []storageChoice
		for _, s := range storages {
			choices = append(choices, storageChoice{
				Name:  s.Name,
				Avail: formatBytes(s.Avail),
				Type:  s.Type,
			})
		}
		return backupStoragesLoadedMsg{storages: choices}
	}
}

func (m detailModel) loadRestoreStoragesCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		storages, err := actions.ListRestoreStorages(ctx, c, r.Node, r.Type)
		if err != nil {
			return backupStoragesLoadedMsg{err: err}
		}
		var choices []storageChoice
		for _, s := range storages {
			choices = append(choices, storageChoice{
				Name:  s.Name,
				Avail: formatBytes(s.Avail),
				Type:  s.Type,
			})
		}
		return backupStoragesLoadedMsg{storages: choices}
	}
}

func (m detailModel) loadNextIDCmd() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		id, err := actions.NextID(ctx, c)
		return nextIDLoadedMsg{id: id, err: err}
	}
}

func (m detailModel) loadCloneNextIDCmd() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		id, err := actions.NextID(ctx, c)
		return cloneNextIDMsg{id: id, err: err}
	}
}

func (m detailModel) cloneResourceCmd(newid int, name string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		vmid := int(r.VMID)
		var assignedID int
		var task *proxmox.Task
		var err error
		if r.Type == "qemu" {
			assignedID, task, err = actions.CloneVM(ctx, c, vmid, newid, r.Node, name)
		} else {
			assignedID, task, err = actions.CloneContainer(ctx, c, vmid, newid, r.Node, name)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 600); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: fmt.Sprintf("Cloned to VMID %d", assignedID), needRefresh: true}
	}
}

func (m detailModel) createBackupCmd(storageName string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		task, err := actions.CreateBackup(ctx, c, int(r.VMID), r.Node, storageName, "snapshot", "zstd")
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 600); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return reloadBackupsMsg{message: "Backup created"}
	}
}

func (m detailModel) deleteBackupCmd(volid, storage string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		task, err := actions.DeleteBackup(ctx, c, r.Node, storage, volid)
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return reloadBackupsMsg{message: "Backup deleted"}
	}
}

func (m detailModel) restoreBackupCmd(volid string, vmid int, name, restoreStorage string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		assignedID, task, err := actions.RestoreBackup(ctx, c, r.Node, volid, vmid, name, restoreStorage)
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 600); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: fmt.Sprintf("Restored to VMID %d", assignedID)}
	}
}

func (m detailModel) deleteResourceCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(r.VMID)

		if r.Type == "qemu" {
			task, err = actions.DeleteVM(ctx, c, vmid, r.Node)
		} else {
			task, err = actions.DeleteContainer(ctx, c, vmid, r.Node)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		typeStr := "VM"
		if r.Type == "lxc" {
			typeStr = "CT"
		}
		return resourceDeletedMsg{message: fmt.Sprintf("%s %d deleted", typeStr, vmid)}
	}
}

func (m detailModel) convertToTemplateCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		vmid := int(r.VMID)
		typeStr := "VM"
		if r.Type == "lxc" {
			typeStr = "CT"
			err := actions.ConvertContainerToTemplate(ctx, c, vmid, r.Node)
			if err != nil {
				return actionResultMsg{err: err}
			}
			return resourceDeletedMsg{message: fmt.Sprintf("%s %d converted to template", typeStr, vmid)}
		}
		task, err := actions.ConvertVMToTemplate(ctx, c, vmid, r.Node)
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 120); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return resourceDeletedMsg{message: fmt.Sprintf("%s %d converted to template", typeStr, vmid)}
	}
}

// refreshResourceCmd re-fetches the current resource's status from the cluster.
func (m detailModel) refreshResourceCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		cl, err := c.Cluster(ctx)
		if err != nil {
			return resourceRefreshedMsg{err: err}
		}
		all, err := cl.Resources(ctx, "vm")
		if err != nil {
			return resourceRefreshedMsg{err: err}
		}
		for _, res := range all {
			if res.VMID == r.VMID && res.Node == r.Node {
				return resourceRefreshedMsg{resource: *res}
			}
		}
		return resourceRefreshedMsg{err: fmt.Errorf("resource not found after refresh")}
	}
}

func (m detailModel) powerCmd(action string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(r.VMID)

		if r.Type == "qemu" {
			switch action {
			case "start":
				task, err = actions.StartVM(ctx, c, vmid, r.Node)
			case "stop":
				task, err = actions.StopVM(ctx, c, vmid, r.Node)
			case "shutdown":
				task, err = actions.ShutdownVM(ctx, c, vmid, r.Node)
			case "reboot":
				task, err = actions.RebootVM(ctx, c, vmid, r.Node)
			}
		} else {
			switch action {
			case "start":
				task, err = actions.StartContainer(ctx, c, vmid, r.Node)
			case "stop":
				task, err = actions.StopContainer(ctx, c, vmid, r.Node)
			case "shutdown":
				task, err = actions.ShutdownContainer(ctx, c, vmid, r.Node)
			case "reboot":
				task, err = actions.RebootContainer(ctx, c, vmid, r.Node)
			}
		}

		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: action + " completed", needRefresh: true}
	}
}

func (m detailModel) loadPrimaryDiskCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		if r.Type == "qemu" {
			vm, err := actions.FindVM(ctx, c, int(r.VMID), r.Node)
			if err != nil {
				return primaryDiskLoadedMsg{}
			}
			keys := make([]string, 0)
			for k, v := range vm.VirtualMachineConfig.MergeDisks() {
				if v != "" && !strings.Contains(v, "media=cdrom") {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				spec := vm.VirtualMachineConfig.MergeDisks()[k]
				if idx := strings.Index(spec, ":"); idx >= 0 {
					return primaryDiskLoadedMsg{location: spec[:idx]}
				}
			}
		} else {
			ct, err := actions.FindContainer(ctx, c, int(r.VMID), r.Node)
			if err != nil {
				return primaryDiskLoadedMsg{}
			}
			if idx := strings.Index(ct.ContainerConfig.RootFS, ":"); idx >= 0 {
				return primaryDiskLoadedMsg{location: ct.ContainerConfig.RootFS[:idx]}
			}
		}
		return primaryDiskLoadedMsg{}
	}
}

func (m detailModel) loadAllTagsCmd() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		tags, err := actions.AllInstanceTags(ctx, c)
		return allTagsLoadedMsg{tags: tags, err: err}
	}
}

func (m detailModel) addTagCmd(tag string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		if r.Type == "qemu" {
			task, err = actions.AddVMTag(ctx, c, int(r.VMID), r.Node, tag)
		} else {
			task, err = actions.AddContainerTag(ctx, c, int(r.VMID), r.Node, tag)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 60); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		// Compute the new tag string locally so the display updates immediately.
		existing := parseTags(r.Tags)
		newTags := strings.Join(append(existing, tag), ";")
		return tagUpdatedMsg{newTags: newTags, message: fmt.Sprintf("Tag %q added", tag)}
	}
}

func (m detailModel) removeTagCmd(tag string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		if r.Type == "qemu" {
			task, err = actions.RemoveVMTag(ctx, c, int(r.VMID), r.Node, tag)
		} else {
			task, err = actions.RemoveContainerTag(ctx, c, int(r.VMID), r.Node, tag)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 60); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		// Compute the new tag string locally so the display updates immediately.
		existing := parseTags(r.Tags)
		var remaining []string
		for _, t := range existing {
			if t != tag {
				remaining = append(remaining, t)
			}
		}
		newTags := strings.Join(remaining, ";")
		return tagUpdatedMsg{newTags: newTags, message: fmt.Sprintf("Tag %q removed", tag)}
	}
}

func (m detailModel) loadDisksCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		disks := make(map[string]string)
		if r.Type == "qemu" {
			vm, err := actions.FindVM(ctx, c, int(r.VMID), r.Node)
			if err != nil {
				return diskListLoadedMsg{err: err}
			}
			for k, v := range vm.VirtualMachineConfig.MergeDisks() {
				if v != "" && !strings.Contains(v, "media=cdrom") {
					disks[k] = v
				}
			}
		} else {
			ct, err := actions.FindContainer(ctx, c, int(r.VMID), r.Node)
			if err != nil {
				return diskListLoadedMsg{err: err}
			}
			if ct.ContainerConfig.RootFS != "" {
				disks["rootfs"] = ct.ContainerConfig.RootFS
			}
			for k, v := range ct.ContainerConfig.MergeMps() {
				disks[k] = v
			}
		}
		return diskListLoadedMsg{disks: disks}
	}
}

func (m detailModel) loadMoveStoragesCmd() tea.Cmd {
	c := m.client
	r := m.resource
	// Determine current storage of the disk to filter it from the target list.
	currentStorage := ""
	if spec, ok := m.availableDisks[m.pendingMoveDisk]; ok {
		if idx := strings.Index(spec, ":"); idx >= 0 {
			currentStorage = spec[:idx]
		}
	}
	return func() tea.Msg {
		ctx := context.Background()
		storages, err := actions.ListRestoreStorages(ctx, c, r.Node, r.Type)
		if err != nil {
			return moveStoragesLoadedMsg{err: err}
		}
		var choices []storageChoice
		for _, s := range storages {
			if s.Name == currentStorage {
				continue // skip the storage the disk already lives on
			}
			choices = append(choices, storageChoice{
				Name:  s.Name,
				Avail: formatBytes(s.Avail),
				Type:  s.Type,
			})
		}
		return moveStoragesLoadedMsg{storages: choices}
	}
}

func (m detailModel) moveDiskCmd(disk, storage string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		vmid := int(r.VMID)
		var task *proxmox.Task
		var err error
		if r.Type == "qemu" {
			task, err = actions.MoveVMDisk(ctx, c, vmid, r.Node, disk, storage, true, 0)
		} else {
			task, err = actions.MoveContainerVolume(ctx, c, vmid, r.Node, disk, storage, true, 0)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 600); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: fmt.Sprintf("Disk %s moved to %s", disk, storage)}
	}
}

func (m detailModel) resizeDiskCmd(disk, size string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		vmid := int(r.VMID)
		var task *proxmox.Task
		var err error
		if r.Type == "qemu" {
			task, err = actions.ResizeVMDisk(ctx, c, vmid, r.Node, disk, size)
		} else {
			task, err = actions.ResizeContainerDisk(ctx, c, vmid, r.Node, disk, size)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: fmt.Sprintf("Disk %s resized by %s", disk, size)}
	}
}

func (m detailModel) createSnapshotCmd(name string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(r.VMID)

		if r.Type == "qemu" {
			task, err = actions.CreateVMSnapshot(ctx, c, vmid, r.Node, name)
		} else {
			task, err = actions.CreateContainerSnapshot(ctx, c, vmid, r.Node, name)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return reloadSnapshotsMsg{message: fmt.Sprintf("Snapshot %q created", name)}
	}
}

func (m detailModel) deleteSnapshotCmd(name string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(r.VMID)

		if r.Type == "qemu" {
			task, err = actions.DeleteVMSnapshot(ctx, c, vmid, r.Node, name)
		} else {
			task, err = actions.DeleteContainerSnapshot(ctx, c, vmid, r.Node, name)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return reloadSnapshotsMsg{message: fmt.Sprintf("Snapshot %q deleted", name)}
	}
}

func (m detailModel) rollbackSnapshotCmd(name string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(r.VMID)

		if r.Type == "qemu" {
			task, err = actions.RollbackVMSnapshot(ctx, c, vmid, r.Node, name)
		} else {
			task, err = actions.RollbackContainerSnapshot(ctx, c, vmid, r.Node, name, false)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: fmt.Sprintf("Rolled back to %q", name), needRefresh: true}
	}
}
