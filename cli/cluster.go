package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/chupakbra/proxmox-cli/internal/actions"
)

func clusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Show cluster information",
	}
	cmd.AddCommand(clusterStatusCmd())
	cmd.AddCommand(clusterResourcesCmd())
	cmd.AddCommand(clusterTasksCmd())
	return cmd
}

func clusterStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show cluster status",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading...")
			cl, err := actions.GetCluster(ctx, proxmoxClient)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(cl)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "Name:\t%s\n", cl.Name)
			fmt.Fprintf(w, "ID:\t%s\n", cl.ID)
			fmt.Fprintf(w, "Version:\t%d\n", cl.Version)
			fmt.Fprintf(w, "Quorate:\t%d\n", cl.Quorate)
			fmt.Fprintf(w, "Nodes:\t%d\n", len(cl.Nodes))
			return w.Flush()
		},
	}
}

func clusterResourcesCmd() *cobra.Command {
	var filterType string
	cmd := &cobra.Command{
		Use:   "resources",
		Short: "Show cluster resources",
		Long: `Show all resources in the cluster. Use --type to filter by resource type.
Valid types: vm, storage, node, pool`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			var filters []string
			if filterType != "" {
				filters = append(filters, filterType)
			}
			s := startSpinner("Loading resources...")
			resources, err := actions.ClusterResources(ctx, proxmoxClient, filters...)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(resources)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTYPE\tNAME\tNODE\tSTATUS\tCPU\tMEM\tDISK")
			for _, r := range resources {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					r.ID,
					r.Type,
					r.Name,
					r.Node,
					r.Status,
					formatCPUPercent(r.CPU),
					formatBytes(r.Mem),
					formatBytes(r.Disk),
				)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&filterType, "type", "", "filter by resource type (vm, storage, node, pool)")
	return cmd
}

func clusterTasksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tasks",
		Short: "List recent cluster tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading tasks...")
			tasks, err := actions.ClusterTasks(ctx, proxmoxClient)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(tasks)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "UPID\tNODE\tTYPE\tUSER\tSTATUS\tSTARTED\tDURATION")
			for _, t := range tasks {
				started := "-"
				if !t.StartTime.IsZero() {
					started = t.StartTime.Format("2006-01-02 15:04:05")
				}
				duration := "-"
				if t.Duration > 0 {
					duration = t.Duration.Round(1e9).String()
				}
				status := t.Status
				if t.ExitStatus != "" && t.ExitStatus != "running" {
					status = t.ExitStatus
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					t.UPID,
					t.Node,
					t.Type,
					t.User,
					status,
					started,
					duration,
				)
			}
			return w.Flush()
		},
	}
}
