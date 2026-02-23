package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/chupakbra/proxmox-cli/internal/actions"
)

var tagNameRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func validateTag(tag string) error {
	if tag == "" {
		return fmt.Errorf("tag must not be empty")
	}
	if !tagNameRegex.MatchString(tag) {
		return fmt.Errorf("invalid tag %q: use only letters, digits, hyphens, underscores, and dots", tag)
	}
	return nil
}

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
	cmd.AddCommand(vmTemplateCmd())
	cmd.AddCommand(vmDiskCmd())
	cmd.AddCommand(vmTagCmd())
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

	if len(vms) == 0 {
		if stdoutIsTerminal() {
			fmt.Fprintf(cmd.OutOrStdout(), "%sNo VMs available.%s\n", colorGold, colorReset)
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "No VMs available.")
		}
		return nil
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
			fmt.Fprintf(out, "%s%s%s\n", colorRed, line, colorReset)
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
		Example: `  pxve vm clone 101 UClone            # auto-assign next available ID
  pxve vm clone 101 UClone --newid 200`,
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

func vmTemplateCmd() *cobra.Command {
	var (
		nodeName string
		force    bool
	)
	cmd := &cobra.Command{
		Use:   "template <vmid>",
		Short: "Convert a VM to a template",
		Long: `Convert a VM to a template. The VM must be stopped.

This operation is irreversible — the VM becomes read-only and can only be cloned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			if !force {
				fmt.Fprintf(cmd.OutOrStdout(), "Convert VM %d to a template? This cannot be undone. [y/N]: ", vmid)
				var response string
				fmt.Fscan(cmd.InOrStdin(), &response)
				if strings.ToLower(strings.TrimSpace(response)) != "y" {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Converting to template...")
			task, err := actions.ConvertVMToTemplate(ctx, proxmoxClient, vmid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Converting VM %d to template...\n", vmid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "VM %d is now a template.\n", vmid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt")
	return cmd
}

// vmDiskCmd groups disk sub-commands.
func vmDiskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disk",
		Short: "Disk operations",
	}
	cmd.AddCommand(vmDiskResizeCmd(), vmDiskMoveCmd(), vmDiskDetachCmd())
	return cmd
}

func vmDiskResizeCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "resize <vmid> <disk> <size>",
		Short: "Grow a VM disk",
		Long: `Grow a VM disk by the specified size delta.

disk is the disk identifier, e.g. scsi0, virtio0, ide0, sata0.
size is a delta with a unit suffix, e.g. 10G, 512M. The '+' prefix is added automatically.`,
		Args: cobra.ExactArgs(3),
		Example: `  pxve vm disk resize 100 scsi0 +10G
  pxve vm disk resize 100 virtio0 +512M`,
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			disk := args[1]
			size, err := normalizeDiskSize(args[2])
			if err != nil {
				return err
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Resizing disk...")
			task, err := actions.ResizeVMDisk(ctx, proxmoxClient, vmid, nodeName, disk, size)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Resizing disk %s on VM %d by %s...\n", disk, vmid, size)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Disk %s resized.\n", disk)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func vmDiskMoveCmd() *cobra.Command {
	var (
		nodeName    string
		storage     string
		deleteAfter bool
		bwlimit     uint64
	)
	cmd := &cobra.Command{
		Use:   "move <vmid> [disk] --storage <target-storage>",
		Short: "Move a VM disk to a different storage",
		Long: `Move a VM disk to a different storage pool.

disk is the disk identifier, e.g. scsi0, virtio0. If omitted, the disk is
auto-selected when the VM has only one moveable disk; otherwise a prompt is shown.
The source disk is deleted after the move by default; use --delete=false to keep it.
Moving a disk on a running VM is supported (live migration).`,
		Args: cobra.RangeArgs(1, 2),
		Example: `  pxve vm disk move 100 --storage local-zfs           # auto-select disk
  pxve vm disk move 100 scsi0 --storage local-zfs
  pxve vm disk move 100 virtio0 --storage ceph-pool --delete --bwlimit 102400`,
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			if storage == "" {
				return fmt.Errorf("--storage is required")
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()

			var disk string
			if len(args) == 2 {
				disk = args[1]
			} else {
				s := startSpinner("Loading VM info...")
				vm, err := actions.FindVM(ctx, proxmoxClient, vmid, nodeName)
				s.Stop()
				if err != nil {
					return handleErr(err)
				}
				filtered := make(map[string]string)
				for k, v := range vm.VirtualMachineConfig.MergeDisks() {
					if v == "" || strings.Contains(v, "media=cdrom") {
						continue
					}
					// Skip disks already on the target storage.
					if parts := strings.SplitN(v, ":", 2); len(parts) > 0 && parts[0] == storage {
						continue
					}
					filtered[k] = v
				}
				disk, err = selectFromList(cmd, filtered, "disk")
				if err != nil {
					return err
				}
			}

			s := startSpinner("Moving disk...")
			task, err := actions.MoveVMDisk(ctx, proxmoxClient, vmid, nodeName, disk, storage, deleteAfter, bwlimit)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Moving disk %s on VM %d to %q...\n", disk, vmid, storage)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Disk %s moved to %q.\n", disk, storage)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().StringVar(&storage, "storage", "", "target storage name (required)")
	cmd.Flags().BoolVar(&deleteAfter, "delete", true, "delete original disk after move (default true; use --delete=false to keep)")
	cmd.Flags().Uint64Var(&bwlimit, "bwlimit", 0, "bandwidth limit in KiB/s (0 = unlimited)")
	return cmd
}

func vmDiskDetachCmd() *cobra.Command {
	var (
		nodeName   string
		deleteData bool
		force      bool
	)
	cmd := &cobra.Command{
		Use:   "detach <vmid> <disk>",
		Short: "Detach a disk from a VM",
		Long: `Detach a disk from the VM config.

Without --delete: disk data is preserved and the disk moves to an "unusedN" slot.
With --delete: disk data is permanently and irreversibly destroyed.

A confirmation prompt is shown when --delete is set, unless --force is also given.`,
		Args: cobra.ExactArgs(2),
		Example: `  pxve vm disk detach 100 scsi1              # moves to unused (data safe)
  pxve vm disk detach 100 scsi1 --delete     # prompts, then destroys data
  pxve vm disk detach 100 scsi1 --delete --force  # no prompt`,
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			disk := args[1]
			if deleteData && !force {
				fmt.Fprintf(cmd.OutOrStdout(), "WARNING: This will permanently delete disk %s data on VM %d. Continue? [y/N]: ", disk, vmid)
				var response string
				fmt.Fscan(cmd.InOrStdin(), &response)
				if strings.ToLower(strings.TrimSpace(response)) != "y" {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Detaching disk...")
			task, err := actions.DetachVMDisk(ctx, proxmoxClient, vmid, nodeName, disk, deleteData)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Detaching disk %s from VM %d...\n", disk, vmid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			if deleteData {
				fmt.Fprintf(cmd.OutOrStdout(), "Disk %s detached and data deleted.\n", disk)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Disk %s detached (data preserved as unused disk).\n", disk)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().BoolVar(&deleteData, "delete", false, "permanently delete disk data")
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt when --delete is set")
	return cmd
}

// normalizeDiskSize ensures size has a leading '+' and a valid unit suffix (G/M/K/T).
// The '+' is prepended automatically if omitted.
func normalizeDiskSize(size string) (string, error) {
	if !strings.HasPrefix(size, "+") {
		size = "+" + size
	}
	if len(size) < 2 {
		return "", fmt.Errorf("size %q is too short", size)
	}
	unit := strings.ToUpper(string(size[len(size)-1]))
	switch unit {
	case "G", "M", "K", "T":
		return size, nil
	default:
		return "", fmt.Errorf("size %q has unknown unit %q (use G, M, K, or T)", size, unit)
	}
}

// vmTagCmd groups tag sub-commands.
func vmTagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage VM tags",
	}
	cmd.AddCommand(vmTagListCmd(), vmTagAddCmd(), vmTagRemoveCmd())
	return cmd
}

func vmTagListCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "list <vmid>",
		Short: "List tags on a VM",
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
			tags, err := actions.VMTags(ctx, proxmoxClient, vmid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if tags == nil {
					tags = []string{}
				}
				return enc.Encode(tags)
			}
			if len(tags) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no tags)")
				return nil
			}
			for _, t := range tags {
				fmt.Fprintln(cmd.OutOrStdout(), t)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func vmTagAddCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "add <vmid> <tag>",
		Short: "Add a tag to a VM",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			tag := args[1]
			if err := validateTag(tag); err != nil {
				return err
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.AddVMTag(ctx, proxmoxClient, vmid, nodeName, tag)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Tag %q added to VM %d.\n", tag, vmid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func vmTagRemoveCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "remove <vmid> <tag>",
		Short: "Remove a tag from a VM",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			tag := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.RemoveVMTag(ctx, proxmoxClient, vmid, nodeName, tag)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Tag %q removed from VM %d.\n", tag, vmid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
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
