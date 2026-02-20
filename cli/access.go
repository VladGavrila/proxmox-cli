package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/chupakbra/proxmox-cli/internal/actions"
)

// aclCmd groups ACL commands.
func aclCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "acl",
		Short: "View access control list entries",
	}
	cmd.AddCommand(aclListCmd())
	return cmd
}

func aclListCmd() *cobra.Command {
	var filterUser string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all ACL entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading...")
			acls, err := actions.ListACLs(ctx, proxmoxClient)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			// Filter by user if requested
			if filterUser != "" {
				filtered := acls[:0]
				for _, a := range acls {
					if a.UGID == filterUser {
						filtered = append(filtered, a)
					}
				}
				acls = filtered
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(acls)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PATH\tTYPE\tSUBJECT\tROLE\tPROPAGATE")
			for _, a := range acls {
				propagate := "no"
				if bool(a.Propagate) {
					propagate = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					a.Path, a.Type, a.UGID, a.RoleID, propagate)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&filterUser, "user", "", "filter entries by user ID")
	return cmd
}

// roleCmd groups role commands.
func roleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "role",
		Short: "View available Proxmox roles",
	}
	cmd.AddCommand(roleListCmd())
	return cmd
}

func roleListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all roles",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading...")
			roles, err := actions.ListRoles(ctx, proxmoxClient)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(roles)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ROLE\tSPECIAL\tPRIVILEGES")
			for _, r := range roles {
				special := ""
				if bool(r.Special) {
					special = "yes"
				}
				privs := r.Privs
				if len(privs) > 80 {
					privs = privs[:77] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", r.RoleID, special, privs)
			}
			return w.Flush()
		},
	}
}
