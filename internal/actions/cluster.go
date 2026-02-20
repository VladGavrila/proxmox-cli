package actions

import (
	"context"

	proxmox "github.com/luthermonson/go-proxmox"
)

// GetCluster returns the cluster object.
func GetCluster(ctx context.Context, c *proxmox.Client) (*proxmox.Cluster, error) {
	return c.Cluster(ctx)
}

// ClusterResources returns all cluster resources, optionally filtered by type.
func ClusterResources(ctx context.Context, c *proxmox.Client, filters ...string) (proxmox.ClusterResources, error) {
	cl, err := c.Cluster(ctx)
	if err != nil {
		return nil, err
	}
	return cl.Resources(ctx, filters...)
}

// ClusterTasks returns recent cluster tasks.
func ClusterTasks(ctx context.Context, c *proxmox.Client) (proxmox.Tasks, error) {
	cl, err := c.Cluster(ctx)
	if err != nil {
		return nil, err
	}
	return cl.Tasks(ctx)
}
