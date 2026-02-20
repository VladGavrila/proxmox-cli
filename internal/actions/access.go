package actions

import (
	"context"
	"fmt"

	proxmox "github.com/luthermonson/go-proxmox"
)

// --- Users ---

func ListUsers(ctx context.Context, c *proxmox.Client) (proxmox.Users, error) {
	return c.Users(ctx)
}

func GetUser(ctx context.Context, c *proxmox.Client, userid string) (*proxmox.User, error) {
	return c.User(ctx, userid)
}

func CreateUser(ctx context.Context, c *proxmox.Client, user *proxmox.NewUser) error {
	return c.NewUser(ctx, user)
}

func DeleteUser(ctx context.Context, c *proxmox.Client, userid string) error {
	u, err := c.User(ctx, userid)
	if err != nil {
		return err
	}
	return u.Delete(ctx)
}

func ChangePassword(ctx context.Context, c *proxmox.Client, userid, password string) error {
	return c.Password(ctx, userid, password)
}

// --- Tokens ---

func ListTokens(ctx context.Context, c *proxmox.Client, userid string) (proxmox.Tokens, error) {
	u, err := c.User(ctx, userid)
	if err != nil {
		return nil, err
	}
	return u.GetAPITokens(ctx)
}

func CreateToken(ctx context.Context, c *proxmox.Client, userid string, token proxmox.Token) (proxmox.NewAPIToken, error) {
	u, err := c.User(ctx, userid)
	if err != nil {
		return proxmox.NewAPIToken{}, err
	}
	return u.NewAPIToken(ctx, token)
}

func DeleteToken(ctx context.Context, c *proxmox.Client, userid, tokenid string) error {
	u, err := c.User(ctx, userid)
	if err != nil {
		return err
	}
	return u.DeleteAPIToken(ctx, tokenid)
}

// --- ACLs ---

func ListACLs(ctx context.Context, c *proxmox.Client) (proxmox.ACLs, error) {
	return c.ACL(ctx)
}

// GrantAccess grants a role to a user on one or more VM/CT paths.
// vmids are the numeric IDs (each maps to /vms/<id> in Proxmox).
func GrantAccess(ctx context.Context, c *proxmox.Client, userid string, vmids []int, role string, propagate bool) error {
	for _, vmid := range vmids {
		path := fmt.Sprintf("/vms/%d", vmid)
		opts := proxmox.ACLOptions{
			Path:  path,
			Users: userid,
			Roles: role,
		}
		if propagate {
			opts.Propagate = proxmox.IntOrBool(true)
		}
		if err := c.UpdateACL(ctx, opts); err != nil {
			return fmt.Errorf("granting access on %s: %w", path, err)
		}
	}
	return nil
}

// GrantAccessByPath grants a role to a user on an arbitrary Proxmox path.
func GrantAccessByPath(ctx context.Context, c *proxmox.Client, userid, path, role string, propagate bool) error {
	opts := proxmox.ACLOptions{
		Path:  path,
		Users: userid,
		Roles: role,
	}
	if propagate {
		opts.Propagate = proxmox.IntOrBool(true)
	}
	return c.UpdateACL(ctx, opts)
}

// RevokeAccess removes a role from a user on one or more VM/CT paths.
func RevokeAccess(ctx context.Context, c *proxmox.Client, userid string, vmids []int, role string) error {
	for _, vmid := range vmids {
		path := fmt.Sprintf("/vms/%d", vmid)
		opts := proxmox.ACLOptions{
			Path:   path,
			Users:  userid,
			Roles:  role,
			Delete: proxmox.IntOrBool(true),
		}
		if err := c.UpdateACL(ctx, opts); err != nil {
			return fmt.Errorf("revoking access on %s: %w", path, err)
		}
	}
	return nil
}

// RevokeAccessByPath removes a role from a user on an arbitrary Proxmox path.
func RevokeAccessByPath(ctx context.Context, c *proxmox.Client, userid, path, role string) error {
	return c.UpdateACL(ctx, proxmox.ACLOptions{
		Path:   path,
		Users:  userid,
		Roles:  role,
		Delete: proxmox.IntOrBool(true),
	})
}

// --- Roles ---

func ListRoles(ctx context.Context, c *proxmox.Client) (proxmox.Roles, error) {
	return c.Roles(ctx)
}
