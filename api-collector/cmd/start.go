/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"api-collector/internal/identity"
	"api-collector/internal/indexer"
	"api-collector/internal/query"
	"api-collector/internal/watcher"
	queryv1 "api-collector/proto/query/v1"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// startCmd represents the start command
var background bool
var file string

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start API Collector",
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
					"failed to start API Collector in background: %w",
					err,
				)
			}

			fmt.Printf(
				"API Collector started in background with PID %d\n",
				child.Process.Pid,
			)

			return nil
		} else {
			fmt.Println("API Collector started")
		}

		// Everything below here runs in the actual server process.

		pidPath, err := getPIDPath()
		if err != nil {
			return err
		}

		if err = ensureNotRunning(pidPath); err != nil {
			return err
		}

		if err = writePID(pidPath); err != nil {
			return err
		}

		defer os.Remove(pidPath)

		collectorID, err := identity.GetOrCreateCollectorID()
		if err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(
			cmd.Context(),
			os.Interrupt,
			syscall.SIGTERM,
		)
		defer stop()

		indexBlocks, err := indexer.ReadIndexFile(file)
		if err != nil {
			return err
		}

		tree := indexer.NewBPlusTree[int64, indexer.IndexBlock](128)

		for _, block := range indexBlocks {
			tree.Insert(block.TimestampStart, block)
		}

		_, lastBlock, ok := tree.Last()
		if !ok {
			lastBlock = indexer.IndexBlock{
				EndOffset: 0,
				BlockSize: 100,
			}
		}

		grpcListener, err := net.Listen(
			"tcp",
			":24899",
		)
		if err != nil {
			return fmt.Errorf("failed to create gRPC listener: %w", err)
		}
		defer grpcListener.Close()

		grpcServer := grpc.NewServer()

		queryServer := query.NewServer(
			file,
			tree,
			collectorID,
		)

		queryv1.RegisterLogServiceServer(
			grpcServer,
			queryServer,
		)

		reflection.Register(grpcServer)

		go func() {
			if err := grpcServer.Serve(grpcListener); err != nil {
				fmt.Printf("gRPC server error: %v\n", err)
			}
		}()

		return watcher.Watch(ctx, file, lastBlock, tree)
	},
}

func init() {
	rootCmd.AddCommand(startCmd)

	startCmd.Flags().BoolVarP(
		&background,
		"background",
		"b",
		true,
		"Run API Collector in the background",
	)

	startCmd.Flags().StringVarP(
		&file,
		"file-path",
		"f",
		"",
		"The file API Collector will be watching",
	)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// startCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// startCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func getPIDPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(configDir, "api-collector")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	return filepath.Join(dir, "api-collector.pid"), nil
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
