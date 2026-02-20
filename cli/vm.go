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

	"github.com/spf13/cobra"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/chupakbra/proxmox-cli/internal/actions"
)

func vmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vm",
		Short: "Manage virtual machines",
	}
	cmd.AddCommand(vmListCmd())
	cmd.AddCommand(vmStartCmd())
	cmd.AddCommand(vmStopCmd())
	cmd.AddCommand(vmShutdownCmd())
	cmd.AddCommand(vmRebootCmd())
	cmd.AddCommand(vmInfoCmd())
	cmd.AddCommand(vmCloneCmd())
	cmd.AddCommand(vmDeleteCmd())
	cmd.AddCommand(vmSnapshotCmd())
	return cmd
}

// vmListCmd lists VMs.
func vmListCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List virtual machines",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading VMs...")
			vms, err := actions.ListVMs(ctx, proxmoxClient, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			return printVMs(cmd, vms)
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "filter by node name")
	return cmd
}

func printVMs(cmd *cobra.Command, vms proxmox.ClusterResources) error {
	if flagOutput == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(vms)
	}

	// Write to a buffer first so tabwriter aligns columns before we apply color.
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "VMID\tNAME\tNODE\tSTATUS\tTMPL\tCPU\tMEM\tDISK\tUPTIME")
	for _, vm := range vms {
		tmpl := ""
		if vm.Template == 1 {
			tmpl = "yes"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			vm.VMID,
			vm.Name,
			vm.Node,
			vm.Status,
			tmpl,
			formatCPUPercent(vm.CPU),
			formatBytes(vm.Mem),
			formatBytes(vm.Disk),
			formatUptime(vm.Uptime),
		)
	}
	w.Flush()

	useColor := stdoutIsTerminal()
	out := cmd.OutOrStdout()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	for i, line := range lines {
		// Line 0 is the header; data rows start at index 1.
		if useColor && i > 0 && vms[i-1].Template == 1 {
			fmt.Fprintf(out, "\033[31m%s\033[0m\n", line)
		} else {
			fmt.Fprintln(out, line)
		}
	}
	return nil
}

// vmStartCmd starts a VM.
func vmStartCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "start <vmid>",
		Short: "Start a virtual machine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.StartVM(ctx, proxmoxClient, vmid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Starting VM %d...\n", vmid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "VM %d started.\n", vmid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

// vmStopCmd stops a VM.
func vmStopCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "stop <vmid>",
		Short: "Stop a virtual machine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.StopVM(ctx, proxmoxClient, vmid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Stopping VM %d...\n", vmid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "VM %d stopped.\n", vmid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

// vmShutdownCmd gracefully shuts down a VM via ACPI.
func vmShutdownCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "shutdown <vmid>",
		Short: "Gracefully shut down a VM (ACPI)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.ShutdownVM(ctx, proxmoxClient, vmid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Shutting down VM %d...\n", vmid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "VM %d shut down.\n", vmid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

// vmRebootCmd reboots a VM.
func vmRebootCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "reboot <vmid>",
		Short: "Reboot a virtual machine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.RebootVM(ctx, proxmoxClient, vmid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rebooting VM %d...\n", vmid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "VM %d rebooted.\n", vmid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

// vmInfoCmd shows detailed info about a VM.
func vmInfoCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "info <vmid>",
		Short: "Show information about a virtual machine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading...")
			vm, err := actions.FindVM(ctx, proxmoxClient, vmid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(vm)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "VMID:\t%d\n", uint64(vm.VMID))
			fmt.Fprintf(w, "Name:\t%s\n", vm.Name)
			fmt.Fprintf(w, "Node:\t%s\n", vm.Node)
			fmt.Fprintf(w, "Status:\t%s\n", vm.Status)
			fmt.Fprintf(w, "CPU:\t%s\n", formatCPUPercent(vm.CPU))
			fmt.Fprintf(w, "Memory:\t%s / %s\n", formatBytes(vm.Mem), formatBytes(vm.MaxMem))
			fmt.Fprintf(w, "Disk:\t%s / %s\n", formatBytes(vm.Disk), formatBytes(vm.MaxDisk))
			fmt.Fprintf(w, "Uptime:\t%s\n", formatUptime(vm.Uptime))
			if vm.Tags != "" {
				fmt.Fprintf(w, "Tags:\t%s\n", vm.Tags)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func vmDeleteCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "delete <vmid>",
		Short: "Delete a VM (must be stopped)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.DeleteVM(ctx, proxmoxClient, vmid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleting VM %d...\n", vmid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "VM %d deleted.\n", vmid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func vmCloneCmd() *cobra.Command {
	var (
		nodeName string
		newid    int
	)
	cmd := &cobra.Command{
		Use:   "clone <vmid> <name>",
		Short: "Clone a VM",
		Args:  cobra.ExactArgs(2),
		Example: `  proxmox-cli vm clone 101 UClone            # auto-assign next available ID
  proxmox-cli vm clone 101 UClone --newid 200`,
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			name := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			clonedID, task, err := actions.CloneVM(ctx, proxmoxClient, vmid, newid, nodeName, name)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cloning VM %d to %q (ID %d)...\n", vmid, name, clonedID)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Clone complete: %q (VMID %d).\n", name, clonedID)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().IntVar(&newid, "newid", 0, "ID for the new VM (default: next available)")
	return cmd
}

// vmSnapshotCmd groups snapshot sub-commands.
func vmSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage VM snapshots",
	}
	cmd.AddCommand(vmSnapshotListCmd())
	cmd.AddCommand(vmSnapshotCreateCmd())
	cmd.AddCommand(vmSnapshotRollbackCmd())
	cmd.AddCommand(vmSnapshotDeleteCmd())
	return cmd
}

func vmSnapshotListCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "list <vmid>",
		Short: "List snapshots of a VM",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading...")
			snaps, err := actions.VMSnapshots(ctx, proxmoxClient, vmid, nodeName)
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
				if s.Snaptime > 0 {
					created = time.Unix(s.Snaptime, 0).Format("2006-01-02 15:04:05")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, s.Parent, created, s.Description)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func vmSnapshotCreateCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "create <vmid> <name>",
		Short: "Create a snapshot of a VM",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			snapName := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.CreateVMSnapshot(ctx, proxmoxClient, vmid, nodeName, snapName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Creating snapshot %q for VM %d...\n", snapName, vmid)
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

func vmSnapshotDeleteCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "delete <vmid> <name>",
		Short: "Delete a VM snapshot",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			snapName := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.DeleteVMSnapshot(ctx, proxmoxClient, vmid, nodeName, snapName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleting snapshot %q for VM %d...\n", snapName, vmid)
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

func vmSnapshotRollbackCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "rollback <vmid> <name>",
		Short: "Rollback a VM to a snapshot",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			snapName := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.RollbackVMSnapshot(ctx, proxmoxClient, vmid, nodeName, snapName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rolling back VM %d to snapshot %q...\n", vmid, snapName)
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
