package actions

import (
	"context"
	"sort"
	"strings"

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

// AllInstanceTags returns a sorted, deduplicated list of all tags used across
// every VM and CT in the cluster.
func AllInstanceTags(ctx context.Context, c *proxmox.Client) ([]string, error) {
	resources, err := ClusterResources(ctx, c, "vm")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	for _, r := range resources {
		if r.Tags == "" {
			continue
		}
		for _, t := range strings.Split(r.Tags, ";") {
			t = strings.TrimSpace(t)
			if t != "" {
				seen[t] = true
			}
		}
	}
	tags := make([]string, 0, len(seen))
	for t := range seen {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags, nil
}
