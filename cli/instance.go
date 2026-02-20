package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/chupakbra/proxmox-cli/internal/client"
	"github.com/chupakbra/proxmox-cli/internal/config"
)

var tokenIDRegex = regexp.MustCompile(`^[^@]+@[^!]+![^!]+$`)

func instanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Manage configured Proxmox instances",
		Long:  "Add, remove, list, and switch between configured Proxmox instances.",
	}

	cmd.AddCommand(instanceListCmd())
	cmd.AddCommand(instanceAddCmd())
	cmd.AddCommand(instanceRemoveCmd())
	cmd.AddCommand(instanceUseCmd())
	cmd.AddCommand(instanceShowCmd())
	return cmd
}

// instanceListCmd lists all configured instances.
func instanceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configured instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if flagOutput == "json" {
				type row struct {
					Name    string `json:"name"`
					URL     string `json:"url"`
					Current bool   `json:"current"`
					Auth    string `json:"auth"`
				}
				var rows []row
				for name, inst := range cfg.Instances {
					auth := "token"
					if inst.Username != "" {
						auth = "credentials"
					}
					rows = append(rows, row{
						Name:    name,
						URL:     inst.URL,
						Current: name == cfg.CurrentInstance,
						Auth:    auth,
					})
				}
				return jsonOut(cmd, rows)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tURL\tAUTH\tCURRENT")
			for name, inst := range cfg.Instances {
				current := ""
				if name == cfg.CurrentInstance {
					current = "*"
				}
				auth := "token"
				if inst.Username != "" {
					auth = "credentials"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, inst.URL, auth, current)
			}
			return w.Flush()
		},
	}
}

// instanceAddCmd adds a new named instance to the config.
func instanceAddCmd() *cobra.Command {
	var (
		url         string
		tokenID     string
		tokenSecret string
		username    string
		password    string
		secure      bool
	)

	cmd := &cobra.Command{
		Use:          "add <name>",
		Short:        "Add a new Proxmox instance",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: false, // show usage when required flags are missing
		Example: `  pxve instance add home-lab \
    --url https://172.20.20.25:8006 \
    --token-id root@pam!cli \
    --token-secret xxxxxxxx

  pxve instance add work-cluster \
    --url https://proxmox.company.com:8006 \
    --username admin@pam \
    --password secret \
    --secure`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			cfg, err := config.Load()
			if err != nil {
				return err
			}

			hasToken := tokenID != "" && tokenSecret != ""
			hasCreds := username != "" && password != ""
			if !hasToken && !hasCreds {
				return fmt.Errorf("provide either --token-id + --token-secret or --username + --password")
			}

			// Validate token-id format: must be user@realm!tokenname
			if hasToken && !tokenIDRegex.MatchString(tokenID) {
				return fmt.Errorf("invalid --token-id %q: expected format user@realm!tokenname (e.g. root@pam!mytoken)", tokenID)
			}

			instCfg := config.InstanceConfig{
				URL:         url,
				TokenID:     tokenID,
				TokenSecret: tokenSecret,
				Username:    username,
				Password:    password,
				VerifyTLS:   secure,
			}

			// Verify connectivity and credentials before saving.
			s := startSpinner(fmt.Sprintf("Verifying connection to %s...", url))
			connErr := verifyInstance(&instCfg)
			s.Stop()
			if connErr != nil {
				return fmt.Errorf("connection check failed: %w\n\nHint: %s", connErr, connectionHint(&instCfg, connErr))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Connection verified.\n")

			cfg.Instances[name] = instCfg
			if err := config.Save(cfg); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Instance %q added.\n", name)
			if cfg.CurrentInstance == "" {
				cfg.CurrentInstance = name
				if err := config.Save(cfg); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Set %q as the default instance.\n", name)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&url, "url", "", "Proxmox API URL including port, e.g. https://192.168.1.10:8006")
	cmd.Flags().StringVar(&tokenID, "token-id", "", "API token ID (e.g. root@pam!mytoken)")
	cmd.Flags().StringVar(&tokenSecret, "token-secret", "", "API token secret")
	cmd.Flags().StringVar(&username, "username", "", "Username for password auth (e.g. root@pam)")
	cmd.Flags().StringVar(&password, "password", "", "Password for password auth")
	cmd.Flags().BoolVar(&secure, "secure", false, "Enforce TLS certificate verification (default is to skip verification)")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}

// instanceRemoveCmd removes a named instance from the config.
func instanceRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a configured instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if _, ok := cfg.Instances[name]; !ok {
				return fmt.Errorf("instance %q not found", name)
			}
			delete(cfg.Instances, name)

			if cfg.CurrentInstance == name {
				cfg.CurrentInstance = ""
			}

			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Instance %q removed.\n", name)
			return nil
		},
	}
}

// instanceUseCmd sets the default instance.
func instanceUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the default instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if _, ok := cfg.Instances[name]; !ok {
				return fmt.Errorf("instance %q not found — add it first with 'instance add'", name)
			}

			cfg.CurrentInstance = name
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Default instance set to %q.\n", name)
			return nil
		},
	}
}

// instanceShowCmd shows config for the current or named instance.
func instanceShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "Show config for the current or named instance",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			name := cfg.CurrentInstance
			if len(args) > 0 {
				name = args[0]
			}
			if name == "" {
				return fmt.Errorf("no instance selected and no name provided")
			}

			inst, ok := cfg.Instances[name]
			if !ok {
				return fmt.Errorf("instance %q not found", name)
			}

			if flagOutput == "json" {
				type out struct {
					Name      string `json:"name"`
					URL       string `json:"url"`
					TokenID   string `json:"token-id,omitempty"`
					Username  string `json:"username,omitempty"`
					VerifyTLS bool   `json:"verify-tls"`
					Current   bool   `json:"current"`
				}
				return jsonOut(cmd, out{
					Name:      name,
					URL:       inst.URL,
					TokenID:   inst.TokenID,
					Username:  inst.Username,
					VerifyTLS: inst.VerifyTLS,
					Current:   name == cfg.CurrentInstance,
				})
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "Name:\t%s\n", name)
			fmt.Fprintf(w, "URL:\t%s\n", inst.URL)
			if inst.TokenID != "" {
				fmt.Fprintf(w, "Token ID:\t%s\n", inst.TokenID)
				fmt.Fprintf(w, "Token Secret:\t%s\n", mask(inst.TokenSecret))
			}
			if inst.Username != "" {
				fmt.Fprintf(w, "Username:\t%s\n", inst.Username)
				fmt.Fprintf(w, "Password:\t%s\n", mask(inst.Password))
			}
			fmt.Fprintf(w, "Verify TLS:\t%v\n", inst.VerifyTLS)
			fmt.Fprintf(w, "Current:\t%v\n", name == cfg.CurrentInstance)
			return w.Flush()
		},
	}
}

// mask replaces all but the last 4 characters with asterisks.
func mask(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	masked := make([]byte, len(s))
	for i := range masked {
		if i < len(s)-4 {
			masked[i] = '*'
		} else {
			masked[i] = s[i]
		}
	}
	return string(masked)
}

// jsonOut marshals v to JSON and writes it to cmd's output.
func jsonOut(cmd *cobra.Command, v interface{}) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// verifyInstance builds a client from instCfg, confirms the server is reachable
// and the credentials are accepted, then (for token auth) checks that the realm
// in the token-id actually exists on the server.
func verifyInstance(instCfg *config.InstanceConfig) error {
	c, err := client.New(instCfg)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if _, err := c.Version(ctx); err != nil {
		return err
	}

	// For token auth, validate the realm from the token-id against /access/domains.
	if instCfg.TokenID != "" {
		realm, err := realmFromTokenID(instCfg.TokenID)
		if err != nil {
			return err
		}
		var domains []struct {
			Realm string `json:"realm"`
		}
		if err := c.Get(ctx, "/access/domains", &domains); err != nil {
			// Non-fatal: if we can't list domains, skip the realm check.
			return nil
		}
		known := make([]string, 0, len(domains))
		for _, d := range domains {
			if d.Realm == realm {
				return nil
			}
			known = append(known, d.Realm)
		}
		return fmt.Errorf("realm %q not found on server (available: %s)", realm, strings.Join(known, ", "))
	}
	return nil
}

// realmFromTokenID extracts the realm from a token-id of the form user@realm!tokenname.
func realmFromTokenID(tokenID string) (string, error) {
	atIdx := strings.LastIndex(tokenID, "@")
	bangIdx := strings.Index(tokenID, "!")
	if atIdx < 0 || bangIdx < 0 || atIdx >= bangIdx {
		return "", fmt.Errorf("invalid token-id %q: expected user@realm!tokenname", tokenID)
	}
	return tokenID[atIdx+1 : bangIdx], nil
}

// connectionHint returns a human-readable hint based on the error type.
func connectionHint(instCfg *config.InstanceConfig, err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "dial"):
		return fmt.Sprintf("check that %s is reachable and the port is correct", instCfg.URL)
	case strings.Contains(msg, "certificate") || strings.Contains(msg, "x509"):
		return "the server uses a self-signed certificate — TLS verification is skipped by default, this may indicate a network interception"
	case strings.Contains(msg, "not authorized"):
		if instCfg.TokenID != "" {
			return fmt.Sprintf("verify that token-id %q and token-secret are correct", instCfg.TokenID)
		}
		return fmt.Sprintf("verify that username %q and password are correct", instCfg.Username)
	default:
		return "check the URL, credentials, and network connectivity"
	}
}
