package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	proxmox "github.com/luthermonson/go-proxmox"
	"github.com/spf13/cobra"

	"github.com/chupakbra/proxmox-cli/internal/actions"
)

func containerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ct",
		Short:   "Manage LXC containers",
		Aliases: []string{"container"},
	}
	cmd.AddCommand(ctListCmd())
	cmd.AddCommand(ctStartCmd())
	cmd.AddCommand(ctStopCmd())
	cmd.AddCommand(ctRebootCmd())
	cmd.AddCommand(ctInfoCmd())
	cmd.AddCommand(ctSnapshotCmd())
	return cmd
}

func ctListCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading containers...")
			cts, err := actions.ListContainers(ctx, proxmoxClient, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			return printContainers(cmd, cts)
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "filter by node name")
	return cmd
}

func printContainers(cmd *cobra.Command, cts proxmox.ClusterResources) error {
	if flagOutput == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(cts)
	}

	// Write to a buffer first so tabwriter aligns columns before we apply color.
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CTID\tNAME\tNODE\tSTATUS\tTMPL\tMEM\tDISK\tUPTIME")
	for _, ct := range cts {
		tmpl := ""
		if ct.Template == 1 {
			tmpl = "yes"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			ct.VMID,
			ct.Name,
			ct.Node,
			ct.Status,
			tmpl,
			formatBytes(ct.MaxMem),
			formatBytes(ct.MaxDisk),
			formatUptime(ct.Uptime),
		)
	}
	w.Flush()

	useColor := stdoutIsTerminal()
	out := cmd.OutOrStdout()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	for i, line := range lines {
		if useColor && i > 0 && cts[i-1].Template == 1 {
			fmt.Fprintf(out, "\033[31m%s\033[0m\n", line)
		} else {
			fmt.Fprintln(out, line)
		}
	}
	return nil
}

func ctStartCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "start <ctid>",
		Short: "Start a container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.StartContainer(ctx, proxmoxClient, ctid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Starting container %d...\n", ctid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Container %d started.\n", ctid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func ctStopCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "stop <ctid>",
		Short: "Stop a container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.StopContainer(ctx, proxmoxClient, ctid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Stopping container %d...\n", ctid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Container %d stopped.\n", ctid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func ctRebootCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "reboot <ctid>",
		Short: "Reboot a container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.RebootContainer(ctx, proxmoxClient, ctid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rebooting container %d...\n", ctid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Container %d rebooted.\n", ctid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func ctInfoCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "info <ctid>",
		Short: "Show information about a container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading...")
			ct, err := actions.FindContainer(ctx, proxmoxClient, ctid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(ct)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "CTID:\t%d\n", uint64(ct.VMID))
			fmt.Fprintf(w, "Name:\t%s\n", ct.Name)
			fmt.Fprintf(w, "Node:\t%s\n", ct.Node)
			fmt.Fprintf(w, "Status:\t%s\n", ct.Status)
			fmt.Fprintf(w, "CPUs:\t%d\n", ct.CPUs)
			fmt.Fprintf(w, "Memory:\t%s\n", formatBytes(ct.MaxMem))
			fmt.Fprintf(w, "Swap:\t%s\n", formatBytes(ct.MaxSwap))
			fmt.Fprintf(w, "Disk:\t%s\n", formatBytes(ct.MaxDisk))
			fmt.Fprintf(w, "Uptime:\t%s\n", formatUptime(ct.Uptime))
			if ct.Tags != "" {
				fmt.Fprintf(w, "Tags:\t%s\n", ct.Tags)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func ctSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage container snapshots",
	}
	cmd.AddCommand(ctSnapshotListCmd())
	cmd.AddCommand(ctSnapshotCreateCmd())
	cmd.AddCommand(ctSnapshotRollbackCmd())
	cmd.AddCommand(ctSnapshotDeleteCmd())
	return cmd
}

func ctSnapshotListCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "list <ctid>",
		Short: "List snapshots of a container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading...")
			snaps, err := actions.ContainerSnapshots(ctx, proxmoxClient, ctid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(snaps)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tPARENT\tCREATED\tDESCRIPTION")
			for _, s := range snaps {
				created := "-"
				if s.SnapshotCreationTime > 0 {
					created = time.Unix(s.SnapshotCreationTime, 0).Format("2006-01-02 15:04:05")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, s.Parent, created, s.Description)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func ctSnapshotCreateCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "create <ctid> <name>",
		Short: "Create a snapshot of a container",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			snapName := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.CreateContainerSnapshot(ctx, proxmoxClient, ctid, nodeName, snapName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Creating snapshot %q for container %d...\n", snapName, ctid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Snapshot %q created.\n", snapName)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func ctSnapshotRollbackCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "rollback <ctid> <name>",
		Short: "Rollback a container to a snapshot",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			snapName := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.RollbackContainerSnapshot(ctx, proxmoxClient, ctid, nodeName, snapName, false)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rolling back container %d to snapshot %q...\n", ctid, snapName)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rollback complete.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func ctSnapshotDeleteCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "delete <ctid> <name>",
		Short: "Delete a container snapshot",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			snapName := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.DeleteContainerSnapshot(ctx, proxmoxClient, ctid, nodeName, snapName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleting snapshot %q for container %d...\n", snapName, ctid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Snapshot %q deleted.\n", snapName)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}
