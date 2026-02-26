package actions

import (
	"context"
	"fmt"
	"sort"

	proxmox "github.com/luthermonson/go-proxmox"
)

// ListContainers returns all containers the authenticated user can see,
// optionally filtered by node.
func ListContainers(ctx context.Context, c *proxmox.Client, nodeName string) (proxmox.ClusterResources, error) {
	cl, err := c.Cluster(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := cl.Resources(ctx, "vm")
	if err != nil {
		return nil, err
	}
	var cts proxmox.ClusterResources
	for _, r := range resources {
		if r.Type == "lxc" && (nodeName == "" || r.Node == nodeName) {
			cts = append(cts, r)
		}
	}
	sort.Slice(cts, func(i, j int) bool { return cts[i].VMID < cts[j].VMID })
	return cts, nil
}

// FindContainer locates a container by CTID. If nodeName is empty, all nodes are scanned.
func FindContainer(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) (*proxmox.Container, error) {
	if nodeName != "" {
		node, err := c.Node(ctx, nodeName)
		if err != nil {
			return nil, fmt.Errorf("getting node %s: %w", nodeName, err)
		}
		return node.Container(ctx, ctid)
	}

	nodes, err := c.Nodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	for _, ns := range nodes {
		node, err := c.Node(ctx, ns.Node)
		if err != nil {
			continue
		}
		ct, err := node.Container(ctx, ctid)
		if err != nil {
			if proxmox.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		return ct, nil
	}
	return nil, proxmox.ErrNotFound
}

// StartContainer starts a container and returns the resulting task.
func StartContainer(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.Start(ctx)
}

// StopContainer stops a container and returns the resulting task.
func StopContainer(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.Stop(ctx)
}

// RebootContainer reboots a container and returns the resulting task.
func RebootContainer(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.Reboot(ctx)
}

// ContainerSnapshots returns the list of snapshots for a container, sorted newest first.
func ContainerSnapshots(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) ([]*proxmox.ContainerSnapshot, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	snaps, err := ct.Snapshots(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].SnapshotCreationTime > snaps[j].SnapshotCreationTime })
	return snaps, nil
}

// CreateContainerSnapshot creates a snapshot for a container and returns the task.
func CreateContainerSnapshot(ctx context.Context, c *proxmox.Client, ctid int, nodeName, name string) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.NewSnapshot(ctx, name)
}

// RollbackContainerSnapshot rolls back a container to a snapshot and returns the task.
func RollbackContainerSnapshot(ctx context.Context, c *proxmox.Client, ctid int, nodeName, name string, start bool) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.RollbackSnapshot(ctx, name, start)
}

// DeleteContainerSnapshot deletes a snapshot for a container and returns the task.
func DeleteContainerSnapshot(ctx context.Context, c *proxmox.Client, ctid int, nodeName, name string) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.DeleteSnapshot(ctx, name)
}

// ShutdownContainer gracefully shuts down a container and returns the resulting task.
func ShutdownContainer(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.Shutdown(ctx, false, 0)
}

// ConvertContainerToTemplate converts a container to a template.
func ConvertContainerToTemplate(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) error {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return err
	}
	return ct.Template(ctx)
}

// ContainerTags returns the list of tags on a container.
func ContainerTags(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) ([]string, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return splitTagsStr(ct.Tags), nil
}

// AddContainerTag adds a tag to a container and returns the task.
func AddContainerTag(ctx context.Context, c *proxmox.Client, ctid int, nodeName, tag string) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.AddTag(ctx, tag)
}

// RemoveContainerTag removes a tag from a container and returns the task.
func RemoveContainerTag(ctx context.Context, c *proxmox.Client, ctid int, nodeName, tag string) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.RemoveTag(ctx, tag)
}

// ResizeContainerDisk grows a container disk by the given delta. disk is e.g.
// "rootfs", "mp0"; size must start with '+', e.g. "+10G".
// go-proxmox uses POST for ct.Resize but Proxmox requires PUT, so we call the
// API directly (same workaround as DeleteVMSnapshot).
func ResizeContainerDisk(ctx context.Context, c *proxmox.Client, ctid int, nodeName, disk, size string) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/nodes/%s/lxc/%d/resize", ct.Node, ctid)
	var upid proxmox.UPID
	if err := c.Put(ctx, path, map[string]interface{}{"disk": disk, "size": size}, &upid); err != nil {
		return nil, err
	}
	return proxmox.NewTask(upid, c), nil
}

// DeleteContainer deletes a container and returns the resulting task.
func DeleteContainer(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.Delete(ctx)
}

// MoveContainerVolume moves a container volume to a different storage.
// If deleteAfter is true, the original volume is removed after the move.
func MoveContainerVolume(ctx context.Context, c *proxmox.Client, ctid int, nodeName, disk, storage string, deleteAfter bool, bwlimit uint64) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	opts := &proxmox.VirtualMachineMoveDiskOptions{
		Disk:    disk,
		Storage: storage,
	}
	if deleteAfter {
		opts.Delete = 1
	}
	if bwlimit > 0 {
		opts.BWLimit = bwlimit
	}
	return ct.MoveVolume(ctx, opts)
}

// GetContainerConfig returns the full configuration of a container.
func GetContainerConfig(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) (*proxmox.ContainerConfig, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.ContainerConfig, nil
}

// ConfigContainer updates a container's configuration and returns the resulting task.
func ConfigContainer(ctx context.Context, c *proxmox.Client, ctid int, nodeName string, opts []proxmox.ContainerOption) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	expanded := make([]proxmox.ContainerOption, len(opts))
	copy(expanded, opts)
	return ct.Config(ctx, expanded...)
}

// CloneContainer clones a container to a new ID. If newid is 0, the next available ID is used.
func CloneContainer(ctx context.Context, c *proxmox.Client, ctid, newid int, nodeName, name string) (int, *proxmox.Task, error) {
	if newid == 0 {
		cl, err := c.Cluster(ctx)
		if err != nil {
			return 0, nil, fmt.Errorf("getting cluster for next ID: %w", err)
		}
		newid, err = cl.NextID(ctx)
		if err != nil {
			return 0, nil, fmt.Errorf("getting next available ID: %w", err)
		}
	}
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return 0, nil, err
	}
	clonedID, task, err := ct.Clone(ctx, &proxmox.ContainerCloneOptions{
		NewID:    newid,
		Hostname: name,
		Full:     1,
	})
	return clonedID, task, err
}
