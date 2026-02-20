package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/chupakbra/proxmox-cli/internal/actions"
)

func nodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Manage Proxmox nodes",
	}
	cmd.AddCommand(nodeListCmd())
	cmd.AddCommand(nodeStatusCmd())
	return cmd
}

func nodeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all nodes in the cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading nodes...")
			nodes, err := actions.ListNodes(ctx, proxmoxClient)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(nodes)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NODE\tSTATUS\tCPU\tMEM USED\tMEM TOTAL\tDISK\tUPTIME")
			for _, n := range nodes {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					n.Node,
					n.Status,
					formatCPUPercent(n.CPU),
					formatBytes(n.Mem),
					formatBytes(n.MaxMem),
					formatBytes(n.Disk),
					formatUptime(n.Uptime),
				)
			}
			return w.Flush()
		},
	}
}

func nodeStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <node>",
		Short: "Show detailed status for a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeName := args[0]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading...")
			node, err := actions.GetNode(ctx, proxmoxClient, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(node)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "Name:\t%s\n", node.Name)
			fmt.Fprintf(w, "PVE Version:\t%s\n", node.PVEVersion)
			fmt.Fprintf(w, "Kernel:\t%s\n", node.Kversion)
			fmt.Fprintf(w, "CPU Usage:\t%s\n", formatCPUPercent(node.CPU))
			fmt.Fprintf(w, "CPU Sockets:\t%d\n", node.CPUInfo.Sockets)
			fmt.Fprintf(w, "CPU Cores:\t%d\n", node.CPUInfo.Cores)
			fmt.Fprintf(w, "CPU Model:\t%s\n", node.CPUInfo.Model)
			fmt.Fprintf(w, "Memory Used:\t%s\n", formatBytes(node.Memory.Used))
			fmt.Fprintf(w, "Memory Total:\t%s\n", formatBytes(node.Memory.Total))
			fmt.Fprintf(w, "Swap Used:\t%s\n", formatBytes(node.Swap.Used))
			fmt.Fprintf(w, "Swap Total:\t%s\n", formatBytes(node.Swap.Total))
			fmt.Fprintf(w, "Root FS Used:\t%s\n", formatBytes(node.RootFS.Used))
			fmt.Fprintf(w, "Root FS Total:\t%s\n", formatBytes(node.RootFS.Total))
			fmt.Fprintf(w, "Uptime:\t%s\n", formatUptime(node.Uptime))
			if len(node.LoadAvg) > 0 {
				fmt.Fprintf(w, "Load Average:\t%s\n", node.LoadAvg[0])
			}
			return w.Flush()
		},
	}
}
