/*
Copyright © 2026 John Kutz <johnandrew.kutz@gmail.com>
*/
package cmd

import (
	"api-observer/internal/server"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

// startCmd represents the start command
var background bool

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start API Observer",
	RunE: func(cmd *cobra.Command, args []string) error {
		if background {
			child := exec.Command(
				os.Args[0],
				"start",
				"--background=false",
			)

			child.Stdin = nil
			child.Stdout = os.Stdout
			child.Stderr = os.Stderr

			child.SysProcAttr = &syscall.SysProcAttr{
				Setsid: true,
			}

			if err := child.Start(); err != nil {
				return fmt.Errorf(
					"failed to start API Observer in background: %w",
					err,
				)
			}

			fmt.Printf(
				"API Observer started in background with PID %d\n",
				child.Process.Pid,
			)

			return nil
		}

		// Everything below here runs in the actual server process.

		pidPath, err := getPIDPath()
		if err != nil {
			return err
		}

		if err := ensureNotRunning(pidPath); err != nil {
			return err
		}

		if err := writePID(pidPath); err != nil {
			return err
		}

		defer os.Remove(pidPath)

		ctx, stop := signal.NotifyContext(
			cmd.Context(),
			os.Interrupt,
			syscall.SIGTERM,
		)
		defer stop()

		return server.RunServer(ctx, background)
	},
}

func init() {
	rootCmd.AddCommand(startCmd)

	startCmd.Flags().BoolVarP(
		&background,
		"background",
		"b",
		true,
		"Run API Observer in the background",
	)
}

func getPIDPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(configDir, "api-observer")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	return filepath.Join(dir, "api-observer.pid"), nil
}

func ensureNotRunning(path string) error {
	data, err := os.ReadFile(path)

	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		// Bad/stale PID file — remove it.
		_ = os.Remove(path)
		return nil
	}

	// Signal 0 doesn't kill the process.
	// It only checks whether the PID exists.
	if err := syscall.Kill(pid, 0); err == nil {
		return fmt.Errorf(
			"api-observer is already running with PID %d",
			pid,
		)
	}

	// PID file exists, but the process does not.
	// Treat it as stale.
	_ = os.Remove(path)

	return nil
}

func writePID(path string) error {
	pid := os.Getpid()

	if err := os.WriteFile(
		path,
		[]byte(strconv.Itoa(pid)),
		0o600,
	); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	return nil
}
