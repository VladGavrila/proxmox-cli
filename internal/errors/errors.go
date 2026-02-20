package errors

import (
	"fmt"
	"net/url"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"
)

// Handle maps library sentinel errors to friendly user-facing messages and
// returns a formatted error that Cobra will print before exiting with code 1.
func Handle(instanceURL string, err error) error {
	if err == nil {
		return nil
	}

	switch {
	case proxmox.IsNotAuthorized(err):
		return fmt.Errorf("permission denied — your account does not have access to perform this operation")
	case proxmox.IsNotFound(err):
		return fmt.Errorf("not found — the requested resource does not exist")
	case proxmox.IsTimeout(err):
		return fmt.Errorf("the operation timed out — check your Proxmox server is reachable")
	case isConnectionError(err):
		if instanceURL != "" {
			return fmt.Errorf("could not connect to Proxmox at %s — check the instance URL and your network", instanceURL)
		}
		return fmt.Errorf("could not connect to Proxmox — check the instance URL and your network")
	default:
		return err
	}
}

func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// url.Error wraps connection refused, no such host, etc.
	var urlErr *url.Error
	_ = urlErr
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "EOF")
}
