package actions

import (
	"context"
	"fmt"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"
)

func ListGroups(ctx context.Context, c *proxmox.Client) (proxmox.Groups, error) {
	return c.Groups(ctx)
}

func GetGroup(ctx context.Context, c *proxmox.Client, groupid string) (*proxmox.Group, error) {
	return c.Group(ctx, groupid)
}

func CreateGroup(ctx context.Context, c *proxmox.Client, groupid, comment string) error {
	return c.NewGroup(ctx, groupid, comment)
}

func DeleteGroup(ctx context.Context, c *proxmox.Client, groupid string) error {
	group, err := c.Group(ctx, groupid)
	if err != nil {
		return fmt.Errorf("getting group %q: %w", groupid, err)
	}
	return group.Delete(ctx)
}

func UpdateGroupComment(ctx context.Context, c *proxmox.Client, groupid, comment string) error {
	group, err := c.Group(ctx, groupid)
	if err != nil {
		return fmt.Errorf("getting group %q: %w", groupid, err)
	}
	group.Comment = comment
	return group.Update(ctx)
}

// AddUserToGroup adds a user to a group by updating the user's group list.
// Proxmox manages group membership from the user side (PUT /access/users/{userid}).
func AddUserToGroup(ctx context.Context, c *proxmox.Client, userid, groupid string) error {
	// Verify the group exists first.
	group, err := c.Group(ctx, groupid)
	if err != nil {
		return fmt.Errorf("getting group %q: %w", groupid, err)
	}
	for _, m := range group.Members {
		if m == userid {
			return fmt.Errorf("user %q is already a member of group %q", userid, groupid)
		}
	}
	// Read user's current groups, append the new one, and update.
	user, err := c.User(ctx, userid)
	if err != nil {
		return fmt.Errorf("getting user %q: %w", userid, err)
	}
	updated := append(user.Groups, groupid)
	return putUserGroups(ctx, c, userid, updated)
}

// RemoveUserFromGroup removes a user from a group by updating the user's group list.
func RemoveUserFromGroup(ctx context.Context, c *proxmox.Client, userid, groupid string) error {
	// Verify the group exists and user is a member.
	group, err := c.Group(ctx, groupid)
	if err != nil {
		return fmt.Errorf("getting group %q: %w", groupid, err)
	}
	found := false
	for _, m := range group.Members {
		if m == userid {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("user %q is not a member of group %q", userid, groupid)
	}
	// Read user's current groups, remove the target, and update.
	user, err := c.User(ctx, userid)
	if err != nil {
		return fmt.Errorf("getting user %q: %w", userid, err)
	}
	filtered := make([]string, 0, len(user.Groups))
	for _, g := range user.Groups {
		if g != groupid {
			filtered = append(filtered, g)
		}
	}
	return putUserGroups(ctx, c, userid, filtered)
}

// putUserGroups updates a user's group list via a raw PUT call.
// go-proxmox's UserOptions has Groups tagged with omitempty, which drops
// the field when the list is empty — preventing removal from the last group.
// This raw call always includes the groups key.
func putUserGroups(ctx context.Context, c *proxmox.Client, userid string, groups []string) error {
	data := map[string]string{
		"groups": strings.Join(groups, ","),
	}
	return c.Put(ctx, fmt.Sprintf("/access/users/%s", userid), data, nil)
}
