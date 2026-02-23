package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/spf13/cobra"

	proxmox "github.com/luthermonson/go-proxmox"
)

// ANSI color codes used across CLI output functions.
const (
	colorReset = "\033[0m"
	colorRed   = "\033[31m"         // template VMs / containers
	colorGold  = "\033[38;5;220m"   // empty-list notices
)

// Spinner shows an animated braille spinner on stderr while work is in progress.
// It is a no-op when stderr is not a terminal (e.g. when output is piped).
type Spinner struct {
	stop chan struct{}
	wg   sync.WaitGroup
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// startSpinner starts the spinner with the given message and returns it.
// Call Stop() when the operation completes.
func startSpinner(msg string) *Spinner {
	s := &Spinner{stop: make(chan struct{})}
	if !stderrIsTerminal() {
		return s
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		tick := time.NewTicker(80 * time.Millisecond)
		defer tick.Stop()
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Fprint(os.Stderr, "\r\033[K") // clear the spinner line
				return
			case <-tick.C:
				fmt.Fprintf(os.Stderr, "\r%s %s", spinnerFrames[i%len(spinnerFrames)], msg)
				i++
			}
		}
	}()
	return s
}

// Stop halts the spinner and clears its line. Safe to call on a no-op spinner.
func (s *Spinner) Stop() {
	select {
	case <-s.stop:
		// already closed (non-tty path where the channel was never used)
	default:
		close(s.stop)
	}
	s.wg.Wait()
}

func stderrIsTerminal() bool {
	fi, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// formatBytes converts bytes to a human-readable string (GiB/MiB/KiB).
func formatBytes(b uint64) string {
	const (
		gib = 1024 * 1024 * 1024
		mib = 1024 * 1024
		kib = 1024
	)
	switch {
	case b >= gib:
		return fmt.Sprintf("%.1f GiB", float64(b)/gib)
	case b >= mib:
		return fmt.Sprintf("%.1f MiB", float64(b)/mib)
	case b >= kib:
		return fmt.Sprintf("%.1f KiB", float64(b)/kib)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// formatUptime converts seconds to a human-readable uptime string.
func formatUptime(seconds uint64) string {
	if seconds == 0 {
		return "-"
	}
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// formatCPUPercent formats a CPU usage float as a percentage string.
func formatCPUPercent(cpu float64) string {
	return fmt.Sprintf("%.1f%%", cpu*100)
}

// yesNo returns "yes" or "no" for an int flag (0/1).
func yesNo(v int) string {
	if v != 0 {
		return "yes"
	}
	return "no"
}

// yesNoBool returns "yes" or "no" for a bool flag.
func yesNoBool(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// selectFromList auto-selects when items has exactly one entry, or prompts the
// user to pick from a numbered list when there are multiple. noun is the
// human-readable name of what is being selected (e.g. "disk", "volume").
// items maps identifier → description string shown to the user.
func selectFromList(cmd *cobra.Command, items map[string]string, noun string) (string, error) {
	keys := make([]string, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	switch len(keys) {
	case 0:
		return "", fmt.Errorf("no moveable %s found", noun)
	case 1:
		fmt.Fprintf(cmd.OutOrStdout(), "Auto-selected %s: %s\n", noun, keys[0])
		return keys[0], nil
	default:
		fmt.Fprintf(cmd.OutOrStdout(), "Multiple %ss found:\n", noun)
		for i, k := range keys {
			fmt.Fprintf(cmd.OutOrStdout(), "  [%d] %s  %s\n", i+1, k, items[k])
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Select %s [1-%d]: ", noun, len(keys))
		var idx int
		fmt.Fscan(cmd.InOrStdin(), &idx)
		if idx < 1 || idx > len(keys) {
			return "", fmt.Errorf("invalid selection %d", idx)
		}
		return keys[idx-1], nil
	}
}

// watchTask streams task log lines to w. Falls back to WaitFor if Watch
// returns an error (e.g. no logs yet available).
func watchTask(ctx context.Context, w io.Writer, task *proxmox.Task) error {
	ch, err := task.Watch(ctx, 0)
	if err != nil {
		// No log output available — just wait for completion
		fmt.Fprintf(w, "Task %s started (no log output available)...\n", task.UPID)
		return task.WaitFor(ctx, 300)
	}
	for line := range ch {
		if line != "" && line != "no content" {
			fmt.Fprintln(w, line)
		}
	}
	// After channel closes, ping to get final status
	if err := task.Ping(ctx); err != nil {
		return err
	}
	if task.IsFailed {
		return fmt.Errorf("task failed: %s", task.ExitStatus)
	}
	return nil
}
