// Package server handles the application runtime
package server

import (
	"api-observer/internal/audit"
	"api-observer/internal/config"
	"api-observer/internal/dashboard"
	"api-observer/internal/ingest"
	"api-observer/internal/nodes"
	ingestv1 "api-observer/proto/ingest/v1"

	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func RunServer(ctx context.Context, background bool) error {
	// Load configuration.
	cfg, err := config.LoadConfigurationFile()
	if err != nil {
		return fmt.Errorf(
			"failed to load configuration: %w",
			err,
		)
	}

	appLogger, appLogFile, err := newLogger(
		cfg.AppLog,
		!background,
		log.Ldate|log.Ltime,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to configure application logger: %w",
			err,
		)
	}
	defer appLogFile.Close()

	findingsLogger, findingsLogFile, err := newLogger(
		cfg.FindingsLog,
		false,
		0,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to configure findings logger: %w",
			err,
		)
	}
	defer findingsLogFile.Close()

	log.SetOutput(appLogger.Writer())

	var rs *audit.RuleSet

	if cfg.RuleSetPath == "" {
		rs = audit.NewRuleSet()
	} else {
		fc, err := loadFile(cfg.RuleSetPath)
		if err != nil {
			return fmt.Errorf(
				"failed to load rule set: %w",
				err,
			)
		}

		rs, err = audit.ParseRuleSet(fc)
		if err != nil {
			return fmt.Errorf(
				"failed to parse rule set: %w",
				err,
			)
		}
	}

	queue := audit.NewQueue(cfg.QueueSize)

	grpcListener, err := net.Listen(
		"tcp",
		cfg.IngestPort,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to create gRPC listener: %w",
			err,
		)
	}
	defer grpcListener.Close()

	httpListener, err := net.Listen(
		"tcp",
		cfg.DashboardPort,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to create HTTP listener: %w",
			err,
		)
	}
	defer httpListener.Close()

	grpcServer := grpc.NewServer()

	ingestv1.RegisterIngestServiceServer(
		grpcServer,
		ingest.NewServer(queue),
	)

	reflection.Register(grpcServer)

	mux := http.NewServeMux()

	fileServer := http.FileServer(
		http.Dir("./internal/dashboard/views/assets"),
	)

	mux.Handle(
		"GET /static/",
		http.StripPrefix("/static/", fileServer),
	)

	nm := nodes.NewNodeManager()

	for _, node := range cfg.Nodes {
		if err := nm.Add(ctx, node.Name, node.Addr); err != nil {
			return fmt.Errorf("failed to add node %q: %w", node.Name, err)
		}
	}

	dashboardHandler := dashboard.NewHandler(rs, nm, cfg)
	dashboardHandler.RegisterRoutes(mux)

	httpServer := &http.Server{
		Handler: mux,

		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,

		ErrorLog: appLogger,
	}

	wg := queue.StartWorkers(
		ctx,
		rs,
		cfg.WorkerCount,
		appLogger,
		findingsLogger,
	)

	defer func() {
		queue.Close()
		wg.Wait()

		appLogger.Println("workers stopped")
		appLogger.Println("API Observer stopped")
	}()

	type serverResult struct {
		name string
		err  error
	}

	serverErr := make(chan serverResult, 2)

	go func() {
		serverErr <- serverResult{
			name: "gRPC",
			err:  grpcServer.Serve(grpcListener),
		}
	}()

	go func() {
		serverErr <- serverResult{
			name: "HTTP",
			err:  httpServer.Serve(httpListener),
		}
	}()

	appLogger.Printf(
		"gRPC ingest listening on %s",
		grpcListener.Addr(),
	)

	appLogger.Printf(
		"HTTP dashboard listening on %s",
		httpListener.Addr(),
	)

	appLogger.Println("API Observer started")

	var runErr error

	select {
	case <-ctx.Done():
		appLogger.Println(
			"shutdown signal received",
		)

	case result := <-serverErr:
		runErr = fmt.Errorf(
			"%s server stopped unexpectedly: %v",
			result.name,
			result.err,
		)

		appLogger.Println(runErr)
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		appLogger.Printf(
			"HTTP graceful shutdown failed: %v",
			err,
		)

		if closeErr := httpServer.Close(); closeErr != nil {
			appLogger.Printf(
				"HTTP forced shutdown failed: %v",
				closeErr,
			)
		}
	}

	appLogger.Println("HTTP server stopped")

	stopped := make(chan struct{})

	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		appLogger.Println("gRPC server stopped")

	case <-time.After(5 * time.Second):
		appLogger.Println(
			"gRPC shutdown timed out; forcing shutdown",
		)

		grpcServer.Stop()
		<-stopped
	}

	return runErr
}

func newLogger(
	logPath string,
	writeStdout bool,
	flag int,
) (*log.Logger, *os.File, error) {
	dir := filepath.Dir(logPath)

	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
	}

	file, err := os.OpenFile(
		logPath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o644,
	)
	if err != nil {
		return nil, nil, err
	}

	var writer io.Writer = file

	if writeStdout {
		writer = io.MultiWriter(
			file,
			os.Stdout,
		)
	}

	logger := log.New(
		writer,
		"",
		flag,
	)

	return logger, file, nil
}

func loadFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
