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
	cmd.AddCommand(ctShutdownCmd())
	cmd.AddCommand(ctRebootCmd())
	cmd.AddCommand(ctInfoCmd())
	cmd.AddCommand(ctCloneCmd())
	cmd.AddCommand(ctDeleteCmd())
	cmd.AddCommand(ctSnapshotCmd())
	cmd.AddCommand(ctTemplateCmd())
	cmd.AddCommand(ctDiskCmd())
	cmd.AddCommand(ctTagCmd())
	cmd.AddCommand(ctConfigCmd())
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

	if len(cts) == 0 {
		if stdoutIsTerminal() {
			fmt.Fprintf(cmd.OutOrStdout(), "%sNo containers available.%s\n", colorGold, colorReset)
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "No containers available.")
		}
		return nil
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
			fmt.Fprintf(out, "%s%s%s\n", colorRed, line, colorReset)
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

func ctShutdownCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "shutdown <ctid>",
		Short: "Gracefully shut down a container",
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
			task, err := actions.ShutdownContainer(ctx, proxmoxClient, ctid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Shutting down container %d...\n", ctid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Container %d shut down.\n", ctid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func ctDeleteCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "delete <ctid>",
		Short: "Delete a container (must be stopped)",
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
			task, err := actions.DeleteContainer(ctx, proxmoxClient, ctid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleting container %d...\n", ctid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Container %d deleted.\n", ctid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func ctCloneCmd() *cobra.Command {
	var (
		nodeName string
		newid    int
	)
	cmd := &cobra.Command{
		Use:   "clone <ctid> <name>",
		Short: "Clone a container",
		Args:  cobra.ExactArgs(2),
		Example: `  pxve ct clone 101 myClone            # auto-assign next available ID
  pxve ct clone 101 myClone --newid 200`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			name := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			clonedID, task, err := actions.CloneContainer(ctx, proxmoxClient, ctid, newid, nodeName, name)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cloning container %d to %q (ID %d)...\n", ctid, name, clonedID)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Clone complete: %q (CTID %d).\n", name, clonedID)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().IntVar(&newid, "newid", 0, "ID for the new container (default: next available)")
	return cmd
}

func ctTemplateCmd() *cobra.Command {
	var (
		nodeName string
		force    bool
	)
	cmd := &cobra.Command{
		Use:   "template <ctid>",
		Short: "Convert a container to a template",
		Long: `Convert a container to a template. The container must be stopped.

This operation is irreversible — the container becomes read-only and can only be cloned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			if !force {
				fmt.Fprintf(cmd.OutOrStdout(), "Convert container %d to a template? This cannot be undone. [y/N]: ", ctid)
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
			err = actions.ConvertContainerToTemplate(ctx, proxmoxClient, ctid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Container %d is now a template.\n", ctid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt")
	return cmd
}

// ctDiskCmd groups disk sub-commands.
func ctDiskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disk",
		Short: "Disk operations",
	}
	cmd.AddCommand(ctDiskResizeCmd(), ctDiskMoveCmd())
	return cmd
}

func ctDiskResizeCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "resize <ctid> <disk> <size>",
		Short: "Grow a container disk",
		Long: `Grow a container disk by the specified size delta.

disk is the disk identifier, e.g. rootfs, mp0, mp1.
size is a delta with a unit suffix, e.g. 10G, 512M. The '+' prefix is added automatically.`,
		Args: cobra.ExactArgs(3),
		Example: `  pxve ct disk resize 101 rootfs +10G
  pxve ct disk resize 101 mp0 +5G`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
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
			task, err := actions.ResizeContainerDisk(ctx, proxmoxClient, ctid, nodeName, disk, size)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Resizing disk %s on container %d by %s...\n", disk, ctid, size)
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

func ctDiskMoveCmd() *cobra.Command {
	var (
		nodeName    string
		storage     string
		deleteAfter bool
		bwlimit     uint64
	)
	cmd := &cobra.Command{
		Use:   "move <ctid> [disk] --storage <target-storage>",
		Short: "Move a container volume to a different storage",
		Long: `Move a container volume to a different storage pool.

disk is the volume identifier, e.g. rootfs, mp0, mp1. If omitted, the volume
is auto-selected when the container has only one moveable volume; otherwise a
prompt is shown.
The source volume is deleted after the move by default; use --delete=false to keep it.`,
		Args: cobra.RangeArgs(1, 2),
		Example: `  pxve ct disk move 101 --storage local-zfs             # auto-select volume
  pxve ct disk move 101 rootfs --storage local-zfs
  pxve ct disk move 101 mp0 --storage ceph-pool --delete`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
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
				s := startSpinner("Loading container info...")
				ct, err := actions.FindContainer(ctx, proxmoxClient, ctid, nodeName)
				s.Stop()
				if err != nil {
					return handleErr(err)
				}
				vols := make(map[string]string)
				if ct.ContainerConfig.RootFS != "" {
					// Skip rootfs if it's already on the target storage.
					if parts := strings.SplitN(ct.ContainerConfig.RootFS, ":", 2); len(parts) == 0 || parts[0] != storage {
						vols["rootfs"] = ct.ContainerConfig.RootFS
					}
				}
				for k, v := range ct.ContainerConfig.MergeMps() {
					if parts := strings.SplitN(v, ":", 2); len(parts) > 0 && parts[0] == storage {
						continue
					}
					vols[k] = v
				}
				disk, err = selectFromList(cmd, vols, "volume")
				if err != nil {
					return err
				}
			}

			s := startSpinner("Moving volume...")
			task, err := actions.MoveContainerVolume(ctx, proxmoxClient, ctid, nodeName, disk, storage, deleteAfter, bwlimit)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Moving volume %s on container %d to %q...\n", disk, ctid, storage)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Volume %s moved to %q.\n", disk, storage)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().StringVar(&storage, "storage", "", "target storage name (required)")
	cmd.Flags().BoolVar(&deleteAfter, "delete", true, "delete original volume after move (default true; use --delete=false to keep)")
	cmd.Flags().Uint64Var(&bwlimit, "bwlimit", 0, "bandwidth limit in KiB/s (0 = unlimited)")
	return cmd
}

// ctTagCmd groups tag sub-commands.
func ctTagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag",
		Short: "Manage container tags",
	}
	cmd.AddCommand(ctTagListCmd(), ctTagAddCmd(), ctTagRemoveCmd())
	return cmd
}

func ctTagListCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "list <ctid>",
		Short: "List tags on a container",
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
			tags, err := actions.ContainerTags(ctx, proxmoxClient, ctid, nodeName)
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

func ctTagAddCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "add <ctid> <tag>",
		Short: "Add a tag to a container",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
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
			task, err := actions.AddContainerTag(ctx, proxmoxClient, ctid, nodeName, tag)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Tag %q added to container %d.\n", tag, ctid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func ctTagRemoveCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "remove <ctid> <tag>",
		Short: "Remove a tag from a container",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			tag := args[1]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Connecting...")
			task, err := actions.RemoveContainerTag(ctx, proxmoxClient, ctid, nodeName, tag)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Tag %q removed from container %d.\n", tag, ctid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	return cmd
}

func ctConfigCmd() *cobra.Command {
	var (
		nodeName    string
		hostname    string
		description string
		cores       int
		memory      int
		swap        int
		onboot      bool
		noOnboot    bool
		protection  bool
		noProtect   bool
	)
	cmd := &cobra.Command{
		Use:   "config <ctid>",
		Short: "View or modify container configuration",
		Long: `View or modify a container's configuration.

Without modification flags, displays the current config.
With flags, updates the specified configuration options.`,
		Args: cobra.ExactArgs(1),
		Example: `  pxve ct config 101
  pxve ct config 101 --output json
  pxve ct config 101 --hostname my-ct --memory 2048
  pxve ct config 101 --onboot
  pxve ct config 101 --no-protection`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid CTID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()

			// Build options from changed flags.
			var opts []proxmox.ContainerOption
			if cmd.Flags().Changed("hostname") {
				opts = append(opts, proxmox.ContainerOption{Name: "hostname", Value: hostname})
			}
			if cmd.Flags().Changed("description") {
				opts = append(opts, proxmox.ContainerOption{Name: "description", Value: description})
			}
			if cmd.Flags().Changed("cores") {
				opts = append(opts, proxmox.ContainerOption{Name: "cores", Value: cores})
			}
			if cmd.Flags().Changed("memory") {
				opts = append(opts, proxmox.ContainerOption{Name: "memory", Value: memory})
			}
			if cmd.Flags().Changed("swap") {
				opts = append(opts, proxmox.ContainerOption{Name: "swap", Value: swap})
			}
			if cmd.Flags().Changed("onboot") {
				opts = append(opts, proxmox.ContainerOption{Name: "onboot", Value: 1})
			}
			if cmd.Flags().Changed("no-onboot") {
				opts = append(opts, proxmox.ContainerOption{Name: "onboot", Value: 0})
			}
			if cmd.Flags().Changed("protection") {
				opts = append(opts, proxmox.ContainerOption{Name: "protection", Value: 1})
			}
			if cmd.Flags().Changed("no-protection") {
				opts = append(opts, proxmox.ContainerOption{Name: "protection", Value: 0})
			}

			if len(opts) > 0 {
				// Modify mode.
				s := startSpinner("Updating config...")
				task, err := actions.ConfigContainer(ctx, proxmoxClient, ctid, nodeName, opts)
				s.Stop()
				if err != nil {
					return handleErr(err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Updating container %d config...\n", ctid)
				if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
					return handleErr(err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Container %d config updated.\n", ctid)
				return nil
			}

			// Read-only mode.
			s := startSpinner("Loading...")
			cfg, err := actions.GetContainerConfig(ctx, proxmoxClient, ctid, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(cfg)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "Hostname:\t%s\n", cfg.Hostname)
			fmt.Fprintf(w, "Description:\t%s\n", cfg.Description)
			fmt.Fprintf(w, "Cores:\t%d\n", cfg.Cores)
			fmt.Fprintf(w, "Memory:\t%d MiB\n", cfg.Memory)
			fmt.Fprintf(w, "Swap:\t%d MiB\n", cfg.Swap)
			fmt.Fprintf(w, "OnBoot:\t%s\n", yesNoBool(bool(cfg.OnBoot)))
			fmt.Fprintf(w, "Protection:\t%s\n", yesNoBool(bool(cfg.Protection)))
			fmt.Fprintf(w, "OS Type:\t%s\n", cfg.OSType)
			fmt.Fprintf(w, "Arch:\t%s\n", cfg.Arch)
			fmt.Fprintf(w, "Unprivileged:\t%s\n", yesNoBool(bool(cfg.Unprivileged)))
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name")
	cmd.Flags().StringVar(&hostname, "hostname", "", "set container hostname")
	cmd.Flags().StringVar(&description, "description", "", "set container description")
	cmd.Flags().IntVar(&cores, "cores", 0, "set number of CPU cores")
	cmd.Flags().IntVar(&memory, "memory", 0, "set memory in MiB")
	cmd.Flags().IntVar(&swap, "swap", 0, "set swap in MiB")
	cmd.Flags().BoolVar(&onboot, "onboot", false, "enable start on boot")
	cmd.Flags().BoolVar(&noOnboot, "no-onboot", false, "disable start on boot")
	cmd.Flags().BoolVar(&protection, "protection", false, "enable protection")
	cmd.Flags().BoolVar(&noProtect, "no-protection", false, "disable protection")
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
