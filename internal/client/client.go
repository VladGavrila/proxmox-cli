package client

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/chupakbra/proxmox-cli/internal/config"
)

const apiPath = "/api2/json"

// New builds a proxmox.Client from an InstanceConfig.
func New(cfg *config.InstanceConfig) (*proxmox.Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("instance URL is not set")
	}

	// go-proxmox requires the full API base URL ending in /api2/json.
	// Append it automatically so users only need to provide host:port.
	baseURL := strings.TrimRight(cfg.URL, "/")
	if !strings.HasSuffix(baseURL, apiPath) {
		baseURL += apiPath
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: !cfg.VerifyTLS, //nolint:gosec
			},
		},
	}

	opts := []proxmox.Option{
		proxmox.WithHTTPClient(httpClient),
	}

	switch {
	case cfg.TokenID != "" && cfg.TokenSecret != "":
		opts = append(opts, proxmox.WithAPIToken(cfg.TokenID, cfg.TokenSecret))
	case cfg.Username != "" && cfg.Password != "":
		opts = append(opts, proxmox.WithCredentials(&proxmox.Credentials{
			Username: cfg.Username,
			Password: cfg.Password,
		}))
	default:
		return nil, fmt.Errorf("instance has no authentication configured (need token-id+token-secret or username+password)")
	}

	return proxmox.NewClient(baseURL, opts...), nil
}
