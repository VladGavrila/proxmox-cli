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

// ContainerSnapshots returns the list of snapshots for a container.
func ContainerSnapshots(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) ([]*proxmox.ContainerSnapshot, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.Snapshots(ctx)
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

// DeleteContainer deletes a container and returns the resulting task.
func DeleteContainer(ctx context.Context, c *proxmox.Client, ctid int, nodeName string) (*proxmox.Task, error) {
	ct, err := FindContainer(ctx, c, ctid, nodeName)
	if err != nil {
		return nil, err
	}
	return ct.Delete(ctx)
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
