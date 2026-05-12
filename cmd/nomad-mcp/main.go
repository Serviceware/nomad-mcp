package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/serviceware/nomad-mcp/internal/logging"
	client "github.com/serviceware/nomad-mcp/internal/nomad"
	"github.com/serviceware/nomad-mcp/internal/server"
)

func main() {
	logger := logging.New(os.Stderr, os.Getenv("NOMAD_MCP_LOG_LEVEL"))
	slog.SetDefault(logger)
	logger.Info("starting nomad-mcp")

	nomadClient, err := client.NewFromEnvironment()
	if err != nil {
		logger.Error("failed to initialize Nomad client", "error", err)
		os.Exit(1)
	}
	defer nomadClient.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		logger.Info("shutdown signal received", "error", context.Cause(ctx))
	}()

	if err := server.Run(ctx, nomadClient, logger); err != nil {
		logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}

	logger.Info("nomad-mcp stopped")
}
