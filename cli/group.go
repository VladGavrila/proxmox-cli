package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/chupakbra/proxmox-cli/internal/actions"
)

var groupIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func groupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "group",
		Aliases: []string{"grp"},
		Short:   "Manage Proxmox user groups",
	}
	cmd.AddCommand(groupListCmd())
	cmd.AddCommand(groupCreateCmd())
	cmd.AddCommand(groupDeleteCmd())
	cmd.AddCommand(groupShowCmd())
	cmd.AddCommand(groupAddMemberCmd())
	cmd.AddCommand(groupRemoveMemberCmd())
	return cmd
}

func groupListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all groups",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading groups...")
			groups, err := actions.ListGroups(ctx, proxmoxClient)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(groups)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "GROUPID\tCOMMENT\tUSERS")
			for _, g := range groups {
				users := g.Users
				if users == "" {
					users = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", g.GroupID, g.Comment, users)
			}
			return w.Flush()
		},
	}
}

func groupCreateCmd() *cobra.Command {
	var comment string
	cmd := &cobra.Command{
		Use:   "create <groupid>",
		Short: "Create a new group",
		Example: `  pxve group create admins --comment "Administrators"
  pxve group create dev-team`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupid := args[0]
			if !groupIDRe.MatchString(groupid) {
				return fmt.Errorf("invalid group ID %q — must contain only letters, digits, hyphens, and underscores", groupid)
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Creating group...")
			err := actions.CreateGroup(ctx, proxmoxClient, groupid, comment)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Group %q created.\n", groupid)
			return nil
		},
	}
	cmd.Flags().StringVar(&comment, "comment", "", "description for the group")
	return cmd
}

func groupDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <groupid>",
		Short: "Delete a group",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupid := args[0]
			if !groupIDRe.MatchString(groupid) {
				return fmt.Errorf("invalid group ID %q", groupid)
			}
			if !force {
				fmt.Fprintf(cmd.OutOrStdout(), "Delete group %q? [y/N]: ", groupid)
				var answer string
				fmt.Fscan(cmd.InOrStdin(), &answer)
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
					return nil
				}
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Deleting group...")
			err := actions.DeleteGroup(ctx, proxmoxClient, groupid)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Group %q deleted.\n", groupid)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt")
	return cmd
}

func groupShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <groupid>",
		Short: "Show group details and members",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupid := args[0]
			if !groupIDRe.MatchString(groupid) {
				return fmt.Errorf("invalid group ID %q", groupid)
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading group...")
			group, err := actions.GetGroup(ctx, proxmoxClient, groupid)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(group)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Group:   %s\n", group.GroupID)
			comment := group.Comment
			if comment == "" {
				comment = "-"
			}
			fmt.Fprintf(out, "Comment: %s\n", comment)
			fmt.Fprintln(out)
			if len(group.Members) == 0 {
				fmt.Fprintln(out, "No members.")
			} else {
				fmt.Fprintf(out, "Members (%d):\n", len(group.Members))
				for _, m := range group.Members {
					fmt.Fprintf(out, "  %s\n", m)
				}
			}
			return nil
		},
	}
}

func groupAddMemberCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-member <groupid> <userid>",
		Short: "Add a user to a group",
		Example: `  pxve group add-member admins alice@pve
  pxve group add-member dev-team bob@pam`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupid := args[0]
			userid := args[1]
			if !groupIDRe.MatchString(groupid) {
				return fmt.Errorf("invalid group ID %q — must contain only letters, digits, hyphens, and underscores", groupid)
			}
			if err := validateUserID(userid); err != nil {
				return err
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Adding member...")
			err := actions.AddUserToGroup(ctx, proxmoxClient, userid, groupid)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "User %q added to group %q.\n", userid, groupid)
			return nil
		},
	}
}

func groupRemoveMemberCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-member <groupid> <userid>",
		Short: "Remove a user from a group",
		Example: `  pxve group remove-member admins alice@pve
  pxve group remove-member dev-team bob@pam`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupid := args[0]
			userid := args[1]
			if !groupIDRe.MatchString(groupid) {
				return fmt.Errorf("invalid group ID %q — must contain only letters, digits, hyphens, and underscores", groupid)
			}
			if err := validateUserID(userid); err != nil {
				return err
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Removing member...")
			err := actions.RemoveUserFromGroup(ctx, proxmoxClient, userid, groupid)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "User %q removed from group %q.\n", userid, groupid)
			return nil
		},
	}
}
