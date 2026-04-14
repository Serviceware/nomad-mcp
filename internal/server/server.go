package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	client "github.com/serviceware/nomad-mcp/internal/nomad"
	"github.com/serviceware/nomad-mcp/internal/tools"
)

const (
	serverName    = "nomad-mcp"
	serverVersion = "0.1.0"
)

func New(nomadClient client.Facade, logger *slog.Logger) *mcp.Server {
	if logger == nil {
		logger = slog.Default()
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	server.AddReceivingMiddleware(loggingMiddleware(logger))
	tools.Register(server, nomadClient)
	tools.RegisterResources(server, nomadClient)
	tools.RegisterPrompts(server, nomadClient)
	logger.Info("configured MCP server", "name", serverName, "version", serverVersion, "nomad_address", nomadClient.Address())

	return server
}

func Run(ctx context.Context, nomadClient client.Facade, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	logger.Info("starting MCP stdio server", "name", serverName, "version", serverVersion)
	err := New(nomadClient, logger).Run(ctx, &mcp.StdioTransport{})
	if err != nil {
		logger.Error("MCP stdio server stopped with error", "error", err)
		return err
	}

	logger.Info("MCP stdio server stopped cleanly")
	return nil
}

func loggingMiddleware(logger *slog.Logger) mcp.Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			start := time.Now()

			attrs := []any{"method", method}
			if toolRequest, ok := req.(*mcp.CallToolRequest); ok {
				attrs = append(attrs, "tool", toolRequest.Params.Name)
			}

			logger.Info("mcp request started", attrs...)
			result, err := next(ctx, method, req)
			duration := time.Since(start)

			if err != nil {
				logger.Error("mcp request failed", append(attrs, "duration", duration.String(), "error", err)...)
				return result, fmt.Errorf("%s failed: %w", method, err)
			}

			logger.Info("mcp request completed", append(attrs, "duration", duration.String())...)
			return result, nil
		}
	}
}
