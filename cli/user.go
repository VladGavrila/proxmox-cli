package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	proxmox "github.com/luthermonson/go-proxmox"
	"github.com/spf13/cobra"

	"github.com/chupakbra/proxmox-cli/internal/actions"
)

func userCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage Proxmox users and API tokens",
	}
	cmd.AddCommand(userListCmd())
	cmd.AddCommand(userCreateCmd())
	cmd.AddCommand(userDeleteCmd())
	cmd.AddCommand(userPasswordCmd())
	cmd.AddCommand(userTokenCmd())
	cmd.AddCommand(userGrantCmd())
	cmd.AddCommand(userRevokeCmd())
	return cmd
}

// userListCmd lists all users.
func userListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all users",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading users...")
			users, err := actions.ListUsers(ctx, proxmoxClient)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(users)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "USERID\tNAME\tEMAIL\tENABLED\tEXPIRES")
			for _, u := range users {
				name := strings.TrimSpace(u.Firstname + " " + u.Lastname)
				enabled := "yes"
				if !bool(u.Enable) {
					enabled = "no"
				}
				expires := "never"
				if u.Expire > 0 {
					expires = time.Unix(int64(u.Expire), 0).Format("2006-01-02")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					u.UserID, name, u.Email, enabled, expires)
			}
			return w.Flush()
		},
	}
}

// userCreateCmd creates a new user.
func userCreateCmd() *cobra.Command {
	var (
		password  string
		email     string
		firstname string
		lastname  string
		comment   string
		groups    []string
		expire    int
	)
	cmd := &cobra.Command{
		Use:   "create <userid>",
		Short: "Create a new user",
		Long: `Create a new Proxmox user. The userid must include a realm, e.g. alice@pam or bob@pve.

Common realms:
  @pam   — Linux PAM authentication (local system users)
  @pve   — Proxmox built-in authentication`,
		Example: `  pxve user create alice@pve --password secret --firstname Alice --lastname Smith
  pxve user create bob@pam --email bob@example.com`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userid := args[0]
			if err := validateUserID(userid); err != nil {
				return err
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Creating user...")
			err := actions.CreateUser(ctx, proxmoxClient, &proxmox.NewUser{
				UserID:    userid,
				Password:  password,
				Email:     email,
				Firstname: firstname,
				Lastname:  lastname,
				Comment:   comment,
				Groups:    groups,
				Expire:    expire,
				Enable:    true,
			})
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "User %q created.\n", userid)
			return nil
		},
	}
	cmd.Flags().StringVar(&password, "password", "", "initial password (only for @pve realm)")
	cmd.Flags().StringVar(&email, "email", "", "email address")
	cmd.Flags().StringVar(&firstname, "firstname", "", "first name")
	cmd.Flags().StringVar(&lastname, "lastname", "", "last name")
	cmd.Flags().StringVar(&comment, "comment", "", "comment")
	cmd.Flags().StringSliceVar(&groups, "groups", nil, "comma-separated list of groups")
	cmd.Flags().IntVar(&expire, "expire", 0, "expiry as Unix timestamp (0 = never)")
	return cmd
}

// userDeleteCmd deletes a user.
func userDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <userid>",
		Short: "Delete a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userid := args[0]
			if err := validateUserID(userid); err != nil {
				return err
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Deleting user...")
			err := actions.DeleteUser(ctx, proxmoxClient, userid)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "User %q deleted.\n", userid)
			return nil
		},
	}
}

// userPasswordCmd changes a user's password.
func userPasswordCmd() *cobra.Command {
	var password string
	cmd := &cobra.Command{
		Use:   "password <userid>",
		Short: "Change a user's password",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userid := args[0]
			if err := validateUserID(userid); err != nil {
				return err
			}
			if password == "" {
				return fmt.Errorf("--password is required")
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Updating password...")
			err := actions.ChangePassword(ctx, proxmoxClient, userid, password)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Password updated for %q.\n", userid)
			return nil
		},
	}
	cmd.Flags().StringVar(&password, "password", "", "new password (required)")
	return cmd
}

// userGrantCmd grants a user access to VMs/CTs.
func userGrantCmd() *cobra.Command {
	var (
		vmids     []string
		role      string
		path      string
		propagate bool
	)
	cmd := &cobra.Command{
		Use:   "grant <userid>",
		Short: "Grant a user access to VMs or containers",
		Long: `Grant a Proxmox role to a user on one or more VMs/containers.

Use --vmid to target specific VMs or containers by ID (works for both).
Use --path for arbitrary Proxmox resource paths.

Common built-in roles:
  PVEVMUser    — start, stop, console (recommended for regular users)
  PVEVMAdmin   — full VM management including clone, delete, snapshot
  PVEAdmin     — full cluster management (except user management)
  Administrator — unrestricted access
  PVEAuditor   — read-only access`,
		Example: `  pxve user grant alice@pve --vmid 100,101 --role PVEVMUser
  pxve user grant alice@pve --vmid 100 --vmid 101 --role PVEVMAdmin
  pxve user grant alice@pve --path /storage/local --role PVEDatastoreUser`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userid := args[0]
			if err := validateUserID(userid); err != nil {
				return err
			}
			if role == "" {
				return fmt.Errorf("--role is required (e.g. PVEVMUser)")
			}
			if len(vmids) == 0 && path == "" {
				return fmt.Errorf("provide --vmid <id> or --path <path>")
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Updating ACL...")

			if path != "" {
				err := actions.GrantAccessByPath(ctx, proxmoxClient, userid, path, role, propagate)
				s.Stop()
				if err != nil {
					return handleErr(err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Granted role %q to %q on %s.\n", role, userid, path)
				return nil
			}

			ids, err := parseVMIDs(vmids)
			if err != nil {
				s.Stop()
				return err
			}
			err = actions.GrantAccess(ctx, proxmoxClient, userid, ids, role, propagate)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Granted role %q to %q on VM/CT(s): %s.\n",
				role, userid, joinInts(ids))
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&vmids, "vmid", nil, "VM or CT ID(s) — may be repeated or comma-separated")
	cmd.Flags().StringVar(&role, "role", "", "Proxmox role to grant (required)")
	cmd.Flags().StringVar(&path, "path", "", "arbitrary Proxmox path (e.g. /storage/local)")
	cmd.Flags().BoolVar(&propagate, "propagate", true, "propagate permission to child objects")
	return cmd
}

// userRevokeCmd revokes a user's access to VMs/CTs.
func userRevokeCmd() *cobra.Command {
	var (
		vmids []string
		role  string
		path  string
	)
	cmd := &cobra.Command{
		Use:     "revoke <userid>",
		Short:   "Revoke a user's access to VMs or containers",
		Example: `  pxve user revoke alice@pve --vmid 100 --role PVEVMUser`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userid := args[0]
			if err := validateUserID(userid); err != nil {
				return err
			}
			if role == "" {
				return fmt.Errorf("--role is required")
			}
			if len(vmids) == 0 && path == "" {
				return fmt.Errorf("provide --vmid <id> or --path <path>")
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Updating ACL...")

			if path != "" {
				err := actions.RevokeAccessByPath(ctx, proxmoxClient, userid, path, role)
				s.Stop()
				if err != nil {
					return handleErr(err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Revoked role %q from %q on %s.\n", role, userid, path)
				return nil
			}

			ids, err := parseVMIDs(vmids)
			if err != nil {
				s.Stop()
				return err
			}
			err = actions.RevokeAccess(ctx, proxmoxClient, userid, ids, role)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked role %q from %q on VM/CT(s): %s.\n",
				role, userid, joinInts(ids))
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&vmids, "vmid", nil, "VM or CT ID(s) — may be repeated or comma-separated")
	cmd.Flags().StringVar(&role, "role", "", "Proxmox role to revoke (required)")
	cmd.Flags().StringVar(&path, "path", "", "arbitrary Proxmox path")
	return cmd
}

// userTokenCmd groups token sub-commands.
func userTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage API tokens for a user",
	}
	cmd.AddCommand(tokenListCmd())
	cmd.AddCommand(tokenCreateCmd())
	cmd.AddCommand(tokenDeleteCmd())
	return cmd
}

func tokenListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <userid>",
		Short: "List API tokens for a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			userid := args[0]
			if err := validateUserID(userid); err != nil {
				return err
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading tokens...")
			tokens, err := actions.ListTokens(ctx, proxmoxClient, userid)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(tokens)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TOKEN ID\tCOMMENT\tPRIV SEP\tEXPIRES")
			for _, t := range tokens {
				privsep := "yes"
				if !bool(t.Privsep) {
					privsep = "no"
				}
				expires := "never"
				if t.Expire > 0 {
					expires = time.Unix(int64(t.Expire), 0).Format("2006-01-02")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					t.TokenID, t.Comment, privsep, expires)
			}
			return w.Flush()
		},
	}
}

func tokenCreateCmd() *cobra.Command {
	var (
		comment   string
		expire    int
		noPrivsep bool
	)
	cmd := &cobra.Command{
		Use:   "create <userid> <tokenid>",
		Short: "Create an API token for a user",
		Long: `Create an API token for a user.

The token secret is shown only once — save it immediately.

Privilege separation (privsep, enabled by default) means the token's effective
permissions are the intersection of the user's permissions and any ACLs granted
directly to the token. Disable with --no-privsep to inherit all user permissions.`,
		Example: `  pxve user token create alice@pve cli-token
  pxve user token create alice@pve automation --comment "CI pipeline" --no-privsep`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			userid := args[0]
			if err := validateUserID(userid); err != nil {
				return err
			}
			tokenid := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Creating token...")
			result, err := actions.CreateToken(ctx, proxmoxClient, userid, proxmox.Token{
				TokenID: tokenid,
				Comment: comment,
				Expire:  expire,
				Privsep: proxmox.IntOrBool(!noPrivsep),
			})
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Token created successfully.\n\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Token ID:  %s\n", result.FullTokenID)
			fmt.Fprintf(cmd.OutOrStdout(), "Secret:    %s\n\n", result.Value)
			fmt.Fprintf(cmd.OutOrStdout(), "The secret is shown only once — save it now.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&comment, "comment", "", "description for the token")
	cmd.Flags().IntVar(&expire, "expire", 0, "expiry as Unix timestamp (0 = never)")
	cmd.Flags().BoolVar(&noPrivsep, "no-privsep", false, "disable privilege separation (inherit all user permissions)")
	return cmd
}

func tokenDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <userid> <tokenid>",
		Short: "Delete an API token",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			userid := args[0]
			if err := validateUserID(userid); err != nil {
				return err
			}
			tokenid := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Deleting token...")
			err := actions.DeleteToken(ctx, proxmoxClient, userid, tokenid)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Token %q deleted from user %q.\n", tokenid, userid)
			return nil
		},
	}
}

// validUserID checks that a userid has the required user@realm format.
var useridRe = regexp.MustCompile(`^[a-zA-Z0-9_\-.]+@[a-zA-Z0-9_\-.]+$`)

func validateUserID(userid string) error {
	if !useridRe.MatchString(userid) {
		return fmt.Errorf("invalid user ID %q — must be in user@realm format (e.g. alice@pve, root@pam)", userid)
	}
	return nil
}

// parseVMIDs converts a slice of string IDs (may include comma-separated values) to ints.
func parseVMIDs(raw []string) ([]int, error) {
	var ids []int
	seen := map[int]bool{}
	for _, s := range raw {
		for _, part := range strings.Split(s, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid VM/CT ID %q", part)
			}
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no valid VM/CT IDs provided")
	}
	return ids, nil
}

func joinInts(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ", ")
}
