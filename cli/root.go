package cli

import (
	"fmt"
	"os"

	proxmox "github.com/luthermonson/go-proxmox"
	"github.com/spf13/cobra"

	"github.com/chupakbra/proxmox-cli/internal/client"
	"github.com/chupakbra/proxmox-cli/internal/config"
	clierrors "github.com/chupakbra/proxmox-cli/internal/errors"
	"github.com/chupakbra/proxmox-cli/internal/upgrade"
	"github.com/chupakbra/proxmox-cli/tui"
)

// version is set at build time via -X github.com/chupakbra/proxmox-cli/cli.version=<ver>.
var version = "1.8.0"

var (
	// global state resolved in PersistentPreRunE
	proxmoxClient   *proxmox.Client
	resolvedConfig  *config.Config
	resolvedInstURL string

	// global flags
	flagInstance    string
	flagURL         string
	flagTokenID     string
	flagTokenSecret string
	flagUsername    string
	flagPassword    string
	flagSecure      bool
	flagOutput      string
	flagTUI         bool
	flagUpgrade     bool
)

// rootCmd is the base command.
var rootCmd = &cobra.Command{
	Use:     "pxve",
	Version: version,
	Short:   "A CLI for managing Proxmox VE infrastructure",
	Long: `pxve provides a command-line interface to manage Proxmox VE
clusters, nodes, virtual machines, and containers.

Configure a Proxmox instance with:
  pxve instance add home-lab --url https://192.168.1.10:8006 \
    --token-id root@pam!cli --token-secret <secret>
  pxve instance use home-lab`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Run: func(cmd *cobra.Command, args []string) {
		if flagUpgrade {
			if err := upgrade.Run(version); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			return
		}
		if flagTUI {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			if err := tui.LaunchTUI(cfg); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				os.Exit(1)
			}
			return
		}
		cmd.Help() //nolint:errcheck
	},
}

// Execute wires the command tree and runs it.
func Execute() {
	rootCmd.SetVersionTemplate("pxve {{.Version}}\n")

	// Local flags (root command only)
	rootCmd.Flags().BoolVar(&flagTUI, "tui", false, "launch interactive terminal UI")
	rootCmd.Flags().BoolVar(&flagUpgrade, "upgrade", false, "upgrade pxve to the latest release")

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&flagInstance, "instance", "i", "", "named Proxmox instance from config (overrides current-instance)")
	rootCmd.PersistentFlags().StringVar(&flagURL, "url", "", "Proxmox URL (e.g. https://192.168.1.10:8006) — one-shot, no config needed")
	rootCmd.PersistentFlags().StringVar(&flagTokenID, "token-id", "", "API token ID (e.g. root@pam!cli)")
	rootCmd.PersistentFlags().StringVar(&flagTokenSecret, "token-secret", "", "API token secret")
	rootCmd.PersistentFlags().StringVar(&flagUsername, "username", "", "Proxmox username (e.g. root@pam)")
	rootCmd.PersistentFlags().StringVar(&flagPassword, "password", "", "Proxmox password")
	rootCmd.PersistentFlags().BoolVar(&flagSecure, "secure", false, "enforce TLS certificate verification (default is to skip verification)")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "table", "output format: table or json")

	// Sub-command groups
	rootCmd.AddCommand(instanceCmd())
	rootCmd.AddCommand(vmCmd())
	rootCmd.AddCommand(containerCmd())
	rootCmd.AddCommand(nodeCmd())
	rootCmd.AddCommand(clusterCmd())
	rootCmd.AddCommand(userCmd())
	rootCmd.AddCommand(aclCmd())
	rootCmd.AddCommand(roleCmd())
	rootCmd.AddCommand(backupCmd())
	rootCmd.AddCommand(groupCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// initClient is called by command RunE functions that need a Proxmox client.
// It resolves the instance config and builds the client.
func initClient(cmd *cobra.Command) error {
	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	resolvedConfig = cfg

	// Inline flags take precedence over everything when --url is provided
	if flagURL != "" {
		inst := &config.InstanceConfig{
			URL:         flagURL,
			TokenID:     flagTokenID,
			TokenSecret: flagTokenSecret,
			Username:    flagUsername,
			Password:    flagPassword,
			VerifyTLS:   flagSecure,
		}
		resolvedInstURL = flagURL
		c, err := client.New(inst)
		if err != nil {
			return err
		}
		proxmoxClient = c
		return nil
	}

	// Resolve named instance
	inst, _, err := cfg.Resolve(flagInstance)
	if err != nil {
		return err
	}
	// --secure flag overrides config
	if flagSecure {
		inst.VerifyTLS = true
	}
	resolvedInstURL = inst.URL

	c, err := client.New(inst)
	if err != nil {
		return err
	}
	proxmoxClient = c
	return nil
}

// handleErr maps an error through the error handler with the resolved URL for
// connection error messages. Commands call this in their RunE return.
func handleErr(err error) error {
	return clierrors.Handle(resolvedInstURL, err)
}
