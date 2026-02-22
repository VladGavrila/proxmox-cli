package actions

import (
	"context"
	"fmt"
	"sort"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"
)

// BackupEntry is a unified representation of a backup item from Proxmox storage.
type BackupEntry struct {
	Volid        string
	Storage      string
	Node         string
	Type         string // "qemu" or "lxc"
	Format       string
	Notes        string
	Verification string
	VMID         uint64
	Size         uint64
	Ctime        int64
	Protected    bool
}

// BackupStorageInfo describes a backup-capable storage on a node.
type BackupStorageInfo struct {
	Name  string
	Node  string
	Type  string
	Avail uint64
	Used  uint64
	Total uint64
}

// ListBackupStorages returns storages that support backup content.
// If nodeName is empty, all nodes are scanned.
func ListBackupStorages(ctx context.Context, c *proxmox.Client, nodeName string) ([]BackupStorageInfo, error) {
	nodeNames, err := resolveNodeNames(ctx, c, nodeName)
	if err != nil {
		return nil, err
	}

	var result []BackupStorageInfo
	for _, nn := range nodeNames {
		node, err := c.Node(ctx, nn)
		if err != nil {
			continue
		}
		storages, err := node.Storages(ctx)
		if err != nil {
			continue
		}
		for _, s := range storages {
			if strings.Contains(s.Content, "backup") {
				result = append(result, BackupStorageInfo{
					Name:  s.Name,
					Node:  nn,
					Type:  s.Type,
					Avail: s.Avail,
					Used:  s.Used,
					Total: s.Total,
				})
			}
		}
	}
	return result, nil
}

// ListBackups returns backup entries, optionally filtered by storage and VMID.
// If nodeName is empty, all nodes are scanned. If storageName is empty, the
// default backup storage for each node is used.
func ListBackups(ctx context.Context, c *proxmox.Client, nodeName, storageName string, vmid int) ([]BackupEntry, error) {
	nodeNames, err := resolveNodeNames(ctx, c, nodeName)
	if err != nil {
		return nil, err
	}

	var result []BackupEntry
	seen := make(map[string]bool) // deduplicate shared storages by volid

	for _, nn := range nodeNames {
		node, err := c.Node(ctx, nn)
		if err != nil {
			continue
		}

		storageNames, err := resolveBackupStorageNames(ctx, node, storageName)
		if err != nil {
			continue
		}

		for _, sn := range storageNames {
			storage, err := node.Storage(ctx, sn)
			if err != nil {
				continue
			}
			content, err := storage.GetContent(ctx)
			if err != nil {
				continue
			}
			for _, item := range content {
				if !strings.Contains(item.Volid, "backup/vzdump-") {
					continue
				}
				if vmid > 0 && item.VMID != uint64(vmid) {
					continue
				}
				if seen[item.Volid] {
					continue
				}
				seen[item.Volid] = true
				result = append(result, BackupEntry{
					Volid:        item.Volid,
					Storage:      sn,
					Node:         nn,
					Type:         parseBackupType(item.Volid),
					Format:       item.Format,
					Notes:        item.Notes,
					Verification: item.Verification,
					VMID:         item.VMID,
					Size:         item.Size,
					Ctime:        int64(item.Ctime),
					Protected:    bool(item.Protection),
				})
			}
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Ctime > result[j].Ctime })
	return result, nil
}

// CreateBackup starts a vzdump backup for the given VMID and returns the task.
// If nodeName is empty, it is auto-resolved from cluster resources.
func CreateBackup(ctx context.Context, c *proxmox.Client, vmid int, nodeName, storageName, mode, compress string) (*proxmox.Task, error) {
	if nodeName == "" {
		resolved, err := resolveNodeForVMID(ctx, c, vmid)
		if err != nil {
			return nil, err
		}
		nodeName = resolved
	}

	node, err := c.Node(ctx, nodeName)
	if err != nil {
		return nil, fmt.Errorf("getting node %s: %w", nodeName, err)
	}

	opts := &proxmox.VirtualMachineBackupOptions{
		VMID: uint64(vmid),
	}
	if storageName != "" {
		opts.Storage = storageName
	}
	if mode != "" {
		opts.Mode = proxmox.VirtualMachineBackupMode(mode)
	}
	if compress != "" {
		opts.Compress = proxmox.VirtualMachineBackupCompress(compress)
	}

	return node.Vzdump(ctx, opts)
}

// DeleteBackup deletes a backup identified by volid from the given storage.
// The storageName can be auto-parsed from the volid prefix if empty.
func DeleteBackup(ctx context.Context, c *proxmox.Client, nodeName, storageName, volid string) (*proxmox.Task, error) {
	if storageName == "" {
		storageName = storageFromVolid(volid)
	}

	node, err := c.Node(ctx, nodeName)
	if err != nil {
		return nil, fmt.Errorf("getting node %s: %w", nodeName, err)
	}

	storage, err := node.Storage(ctx, storageName)
	if err != nil {
		return nil, fmt.Errorf("getting storage %s: %w", storageName, err)
	}

	// Extract the filename from the volid (part after "backup/")
	parts := strings.SplitN(volid, "backup/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid backup volid %q", volid)
	}
	filename := parts[1]

	backup, err := storage.Backup(ctx, filename)
	if err != nil {
		return nil, fmt.Errorf("getting backup %s: %w", filename, err)
	}

	return backup.Delete(ctx)
}

// NextID returns the next available VMID from the cluster.
func NextID(ctx context.Context, c *proxmox.Client) (int, error) {
	cl, err := c.Cluster(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting cluster: %w", err)
	}
	return cl.NextID(ctx)
}

// RestoreBackup restores a backup to a VM or CT. If vmid is 0, the next
// available ID is assigned. name sets the VM name / CT hostname (empty = use
// the name embedded in the backup). Returns (assignedID, task, error).
func RestoreBackup(ctx context.Context, c *proxmox.Client, nodeName, volid string, vmid int, name, restoreStorage string) (int, *proxmox.Task, error) {
	if vmid == 0 {
		var err error
		vmid, err = NextID(ctx, c)
		if err != nil {
			return 0, nil, fmt.Errorf("getting next available ID: %w", err)
		}
	}

	backupType := parseBackupType(volid)

	var upid proxmox.UPID
	if backupType == "qemu" {
		params := map[string]interface{}{
			"vmid":    vmid,
			"archive": volid,
		}
		if restoreStorage != "" {
			params["storage"] = restoreStorage
		}
		if name != "" {
			params["name"] = name
		}
		path := fmt.Sprintf("/nodes/%s/qemu", nodeName)
		if err := c.Post(ctx, path, params, &upid); err != nil {
			return 0, nil, err
		}
	} else {
		params := map[string]interface{}{
			"vmid":       vmid,
			"ostemplate": volid,
			"restore":    1,
		}
		if restoreStorage != "" {
			params["storage"] = restoreStorage
		}
		if name != "" {
			params["hostname"] = name
		}
		path := fmt.Sprintf("/nodes/%s/lxc", nodeName)
		if err := c.Post(ctx, path, params, &upid); err != nil {
			return 0, nil, err
		}
	}

	return vmid, proxmox.NewTask(upid, c), nil
}

// BackupConfig extracts the embedded configuration from a backup archive.
func BackupConfig(ctx context.Context, c *proxmox.Client, nodeName, volid string) (*proxmox.VzdumpConfig, error) {
	node, err := c.Node(ctx, nodeName)
	if err != nil {
		return nil, fmt.Errorf("getting node %s: %w", nodeName, err)
	}
	return node.VzdumpExtractConfig(ctx, volid)
}

// parseBackupType returns "qemu" or "lxc" based on the vzdump filename in the volid.
func parseBackupType(volid string) string {
	if strings.Contains(volid, "vzdump-lxc-") {
		return "lxc"
	}
	return "qemu"
}

// storageFromVolid extracts the storage name from a volid like "local:backup/vzdump-...".
func storageFromVolid(volid string) string {
	if idx := strings.Index(volid, ":"); idx >= 0 {
		return volid[:idx]
	}
	return volid
}

// ListRestoreStorages returns storages suitable as restore targets for the given
// resource type. VMs need "images" content; containers need "rootdir" content.
// If nodeName is empty, all nodes are scanned.
func ListRestoreStorages(ctx context.Context, c *proxmox.Client, nodeName, resourceType string) ([]BackupStorageInfo, error) {
	contentType := "images"
	if resourceType == "lxc" {
		contentType = "rootdir"
	}

	nodeNames, err := resolveNodeNames(ctx, c, nodeName)
	if err != nil {
		return nil, err
	}

	var result []BackupStorageInfo
	for _, nn := range nodeNames {
		node, err := c.Node(ctx, nn)
		if err != nil {
			continue
		}
		storages, err := node.Storages(ctx)
		if err != nil {
			continue
		}
		for _, s := range storages {
			if strings.Contains(s.Content, contentType) {
				result = append(result, BackupStorageInfo{
					Name:  s.Name,
					Node:  nn,
					Type:  s.Type,
					Avail: s.Avail,
					Used:  s.Used,
					Total: s.Total,
				})
			}
		}
	}
	return result, nil
}

// resolveNodeForVMID finds the node hosting the given VMID via cluster resources.
func resolveNodeForVMID(ctx context.Context, c *proxmox.Client, vmid int) (string, error) {
	cl, err := c.Cluster(ctx)
	if err != nil {
		return "", fmt.Errorf("getting cluster: %w", err)
	}
	resources, err := cl.Resources(ctx, "vm")
	if err != nil {
		return "", fmt.Errorf("listing cluster resources: %w", err)
	}
	for _, r := range resources {
		if r.VMID == uint64(vmid) {
			return r.Node, nil
		}
	}
	return "", fmt.Errorf("VMID %d not found in cluster", vmid)
}

// resolveNodeNames returns node names to scan. If nodeName is provided, returns
// just that; otherwise returns all nodes in the cluster.
func resolveNodeNames(ctx context.Context, c *proxmox.Client, nodeName string) ([]string, error) {
	if nodeName != "" {
		return []string{nodeName}, nil
	}
	nodes, err := c.Nodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	names := make([]string, len(nodes))
	for i, ns := range nodes {
		names[i] = ns.Node
	}
	return names, nil
}

// resolveBackupStorageNames returns storage names to scan for backups. If
// storageName is provided, returns just that; otherwise finds all backup-capable
// storages on the node.
func resolveBackupStorageNames(ctx context.Context, node *proxmox.Node, storageName string) ([]string, error) {
	if storageName != "" {
		return []string{storageName}, nil
	}
	storages, err := node.Storages(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range storages {
		if strings.Contains(s.Content, "backup") {
			names = append(names, s.Name)
		}
	}
	return names, nil
}
