/*
Copyright © 2026 John Kutz <johnandrew.kutz@gmail.com>
*/
package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
)

// stopCmd represents the stop command
var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop API Observer",
	RunE: func(cmd *cobra.Command, args []string) error {
		pidPath, err := getPIDPath()
		if err != nil {
			return err
		}

		data, err := os.ReadFile(pidPath)
		if os.IsNotExist(err) {
			return fmt.Errorf("api-observer is not running")
		}

		if err != nil {
			return fmt.Errorf("failed to read PID file: %w", err)
		}

		pid, err := strconv.Atoi(
			strings.TrimSpace(string(data)),
		)
		if err != nil {
			return fmt.Errorf(
				"invalid PID file contents: %w",
				err,
			)
		}

		process, err := os.FindProcess(pid)
		if err != nil {
			return fmt.Errorf(
				"failed to find process %d: %w",
				pid,
				err,
			)
		}

		if err := process.Signal(syscall.SIGTERM); err != nil {
			if err == os.ErrProcessDone {
				_ = os.Remove(pidPath)

				return fmt.Errorf(
					"api-observer is not running",
				)
			}

			return fmt.Errorf(
				"failed to stop api-observer: %w",
				err,
			)
		}

		fmt.Printf(
			"stopping API Observer (PID %d)\n",
			pid,
		)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// stopCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// stopCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
