package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/chupakbra/proxmox-cli/internal/actions"
)

func backupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage backups (vzdump)",
	}
	cmd.AddCommand(backupStoragesCmd())
	cmd.AddCommand(backupListCmd())
	cmd.AddCommand(backupCreateCmd())
	cmd.AddCommand(backupDeleteCmd())
	cmd.AddCommand(backupRestoreCmd())
	cmd.AddCommand(backupInfoCmd())
	return cmd
}

func backupStoragesCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "storages",
		Short: "List backup-capable storages",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading storages...")
			storages, err := actions.ListBackupStorages(ctx, proxmoxClient, nodeName)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(storages)
			}

			if len(storages) == 0 {
				if stdoutIsTerminal() {
					fmt.Fprintf(cmd.OutOrStdout(), "%sNo backup storages found.%s\n", colorGold, colorReset)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "No backup storages found.")
				}
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tNODE\tTYPE\tAVAIL\tUSED\tTOTAL")
			for _, st := range storages {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					st.Name, st.Node, st.Type,
					formatBytes(st.Avail), formatBytes(st.Used), formatBytes(st.Total),
				)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "filter by node name")
	return cmd
}

func backupListCmd() *cobra.Command {
	var (
		nodeName    string
		storageName string
		vmid        int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List backups",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading backups...")
			backups, err := actions.ListBackups(ctx, proxmoxClient, nodeName, storageName, vmid)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}

			if flagOutput == "json" {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(backups)
			}

			if len(backups) == 0 {
				if stdoutIsTerminal() {
					fmt.Fprintf(cmd.OutOrStdout(), "%sNo backups found.%s\n", colorGold, colorReset)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "No backups found.")
				}
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "VOLID\tVMID\tTYPE\tSIZE\tDATE\tNOTES")
			for _, b := range backups {
				date := "-"
				if b.Ctime > 0 {
					date = time.Unix(b.Ctime, 0).Format("2006-01-02 15:04:05")
				}
				fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\n",
					b.Volid, b.VMID, b.Type, formatBytes(b.Size), date, b.Notes,
				)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "filter by node name")
	cmd.Flags().StringVar(&storageName, "storage", "", "filter by storage name")
	cmd.Flags().IntVar(&vmid, "vmid", 0, "filter by VMID")
	return cmd
}

func backupCreateCmd() *cobra.Command {
	var (
		nodeName    string
		storageName string
		mode        string
		compress    string
	)
	cmd := &cobra.Command{
		Use:   "create <vmid>",
		Short: "Create a backup (vzdump)",
		Args:  cobra.ExactArgs(1),
		Example: `  pxve backup create 101
  pxve backup create 101 --storage local --mode snapshot --compress zstd`,
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid VMID %q", args[0])
			}
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Creating backup...")
			task, err := actions.CreateBackup(ctx, proxmoxClient, vmid, nodeName, storageName, mode, compress)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Backing up VMID %d...\n", vmid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Backup of VMID %d completed.\n", vmid)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name (auto-resolved if omitted)")
	cmd.Flags().StringVar(&storageName, "storage", "", "target backup storage")
	cmd.Flags().StringVar(&mode, "mode", "snapshot", "backup mode: snapshot, suspend, stop")
	cmd.Flags().StringVar(&compress, "compress", "zstd", "compression: zstd, lzo, gzip, 0")
	return cmd
}

func backupDeleteCmd() *cobra.Command {
	var (
		nodeName    string
		storageName string
	)
	cmd := &cobra.Command{
		Use:   "delete <volid>",
		Short: "Delete a backup",
		Args:  cobra.ExactArgs(1),
		Example: `  pxve backup delete local:backup/vzdump-qemu-101-2025_01_01-00_00_00.vma.zst --node pve`,
		RunE: func(cmd *cobra.Command, args []string) error {
			volid := args[0]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Deleting backup...")
			task, err := actions.DeleteBackup(ctx, proxmoxClient, nodeName, storageName, volid)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleting %s...\n", volid)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Backup deleted.")
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name (required)")
	cmd.Flags().StringVar(&storageName, "storage", "", "storage name (auto-parsed from volid if omitted)")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func backupRestoreCmd() *cobra.Command {
	var (
		nodeName       string
		vmid           int
		name           string
		restoreStorage string
	)
	cmd := &cobra.Command{
		Use:   "restore <volid>",
		Short: "Restore a VM or CT from a backup",
		Args:  cobra.ExactArgs(1),
		Example: `  pxve backup restore local:backup/vzdump-qemu-101-2025_01_01-00_00_00.vma.zst --node pve
  pxve backup restore local:backup/vzdump-lxc-102-2025_01_01-00_00_00.tar.zst --node pve --vmid 200 --name my-ct --storage local-lvm`,
		RunE: func(cmd *cobra.Command, args []string) error {
			volid := args[0]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Restoring backup...")
			assignedID, task, err := actions.RestoreBackup(ctx, proxmoxClient, nodeName, volid, vmid, name, restoreStorage)
			s.Stop()
			if err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Restoring %s to VMID %d...\n", volid, assignedID)
			if err := watchTask(ctx, cmd.OutOrStdout(), task); err != nil {
				return handleErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Restore complete (VMID %d).\n", assignedID)
			return nil
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name (required)")
	cmd.Flags().IntVar(&vmid, "vmid", 0, "target VMID (default: next available)")
	cmd.Flags().StringVar(&name, "name", "", "name for the restored VM / CT hostname (default: from backup)")
	cmd.Flags().StringVar(&restoreStorage, "storage", "", "target storage for restored disks (must support images for VMs, rootdir for CTs)")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

func backupInfoCmd() *cobra.Command {
	var nodeName string
	cmd := &cobra.Command{
		Use:   "info <volid>",
		Short: "Show embedded configuration of a backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			volid := args[0]
			if err := initClient(cmd); err != nil {
				return err
			}
			ctx := context.Background()
			s := startSpinner("Loading config...")
			cfg, err := actions.BackupConfig(ctx, proxmoxClient, nodeName, volid)
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
			printVzdumpConfig(w, cfg)
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&nodeName, "node", "", "node name (required)")
	_ = cmd.MarkFlagRequired("node")
	return cmd
}

// printVzdumpConfig prints non-empty fields of a VzdumpConfig as key-value pairs.
func printVzdumpConfig(w *tabwriter.Writer, cfg interface{}) {
	v := reflect.ValueOf(cfg)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		val := v.Field(i)

		if !val.IsValid() || !field.IsExported() {
			continue
		}

		// Skip zero/empty values.
		if val.IsZero() {
			continue
		}

		// Use json tag for display name if available.
		name := field.Name
		if tag := field.Tag.Get("json"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				name = parts[0]
			}
		}

		fmt.Fprintf(w, "%s:\t%v\n", name, val.Interface())
	}
}
