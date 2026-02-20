package actions

import (
	"context"
	"fmt"
	"sort"

	proxmox "github.com/luthermonson/go-proxmox"
)

// ListVMs returns all VMs the authenticated user can see, optionally filtered by node.
func ListVMs(ctx context.Context, c *proxmox.Client, nodeName string) (proxmox.ClusterResources, error) {
	cl, err := c.Cluster(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := cl.Resources(ctx, "vm")
	if err != nil {
		return nil, err
	}
	var vms proxmox.ClusterResources
	for _, r := range resources {
		if r.Type == "qemu" && (nodeName == "" || r.Node == nodeName) {
			vms = append(vms, r)
		}
	}
	sort.Slice(vms, func(i, j int) bool { return vms[i].VMID < vms[j].VMID })
	return vms, nil
}

// FindVM locates a VM by VMID. If nodeName is empty, all nodes are scanned.
func FindVM(ctx context.Context, c *proxmox.Client, vmid int, nodeName string) (*proxmox.VirtualMachine, error) {
	if nodeName != "" {
		node, err := c.Node(ctx, nodeName)
		if err != nil {
			return nil, fmt.Errorf("getting node %s: %w", nodeName, err)
		}
		return node.VirtualMachine(ctx, vmid)
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
		vm, err := node.VirtualMachine(ctx, vmid)
		if err != nil {
			if proxmox.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		return vm, nil
	}
	return nil, proxmox.ErrNotFound
}

// StartVM starts a VM and returns the resulting task.
func StartVM(ctx context.Context, c *proxmox.Client, vmid int, nodeName string) (*proxmox.Task, error) {
	vm, err := FindVM(ctx, c, vmid, nodeName)
	if err != nil {
		return nil, err
	}
	return vm.Start(ctx)
}

// StopVM stops a VM and returns the resulting task.
func StopVM(ctx context.Context, c *proxmox.Client, vmid int, nodeName string) (*proxmox.Task, error) {
	vm, err := FindVM(ctx, c, vmid, nodeName)
	if err != nil {
		return nil, err
	}
	return vm.Stop(ctx)
}

// ShutdownVM gracefully shuts down a VM via ACPI and returns the resulting task.
func ShutdownVM(ctx context.Context, c *proxmox.Client, vmid int, nodeName string) (*proxmox.Task, error) {
	vm, err := FindVM(ctx, c, vmid, nodeName)
	if err != nil {
		return nil, err
	}
	return vm.Shutdown(ctx)
}

// RebootVM reboots a VM and returns the resulting task.
func RebootVM(ctx context.Context, c *proxmox.Client, vmid int, nodeName string) (*proxmox.Task, error) {
	vm, err := FindVM(ctx, c, vmid, nodeName)
	if err != nil {
		return nil, err
	}
	return vm.Reboot(ctx)
}

// VMSnapshots returns the list of snapshots for a VM.
func VMSnapshots(ctx context.Context, c *proxmox.Client, vmid int, nodeName string) ([]*proxmox.Snapshot, error) {
	vm, err := FindVM(ctx, c, vmid, nodeName)
	if err != nil {
		return nil, err
	}
	return vm.Snapshots(ctx)
}

// CreateVMSnapshot creates a snapshot for a VM and returns the task.
func CreateVMSnapshot(ctx context.Context, c *proxmox.Client, vmid int, nodeName, name string) (*proxmox.Task, error) {
	vm, err := FindVM(ctx, c, vmid, nodeName)
	if err != nil {
		return nil, err
	}
	return vm.NewSnapshot(ctx, name)
}

// RollbackVMSnapshot rolls back a VM to a snapshot and returns the task.
func RollbackVMSnapshot(ctx context.Context, c *proxmox.Client, vmid int, nodeName, name string) (*proxmox.Task, error) {
	vm, err := FindVM(ctx, c, vmid, nodeName)
	if err != nil {
		return nil, err
	}
	return vm.SnapshotRollback(ctx, name)
}

// DeleteVM deletes a VM and returns the task.
func DeleteVM(ctx context.Context, c *proxmox.Client, vmid int, nodeName string) (*proxmox.Task, error) {
	vm, err := FindVM(ctx, c, vmid, nodeName)
	if err != nil {
		return nil, err
	}
	return vm.Delete(ctx)
}

// CloneVM clones a VM to a new ID. If newid is 0, the next available ID is used.
func CloneVM(ctx context.Context, c *proxmox.Client, vmid, newid int, nodeName, name string) (int, *proxmox.Task, error) {
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
	vm, err := FindVM(ctx, c, vmid, nodeName)
	if err != nil {
		return 0, nil, err
	}
	clonedID, task, err := vm.Clone(ctx, &proxmox.VirtualMachineCloneOptions{
		NewID: newid,
		Name:  name,
	})
	return clonedID, task, err
}

// DeleteVMSnapshot deletes a VM snapshot. The go-proxmox library does not expose
// this method, so we call the Proxmox REST API directly.
func DeleteVMSnapshot(ctx context.Context, c *proxmox.Client, vmid int, nodeName, name string) (*proxmox.Task, error) {
	vm, err := FindVM(ctx, c, vmid, nodeName)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/snapshot/%s", vm.Node, vmid, name)
	var upid proxmox.UPID
	if err := c.Delete(ctx, path, &upid); err != nil {
		return nil, err
	}
	return proxmox.NewTask(upid, c), nil
}
