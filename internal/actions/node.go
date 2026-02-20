package actions

import (
	"context"

	proxmox "github.com/luthermonson/go-proxmox"
)

// ListNodes returns all node statuses from the cluster.
func ListNodes(ctx context.Context, c *proxmox.Client) (proxmox.NodeStatuses, error) {
	return c.Nodes(ctx)
}

// GetNode returns the full status for a named node.
func GetNode(ctx context.Context, c *proxmox.Client, name string) (*proxmox.Node, error) {
	return c.Node(ctx, name)
}
