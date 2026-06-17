# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`nomad-mcp` is a **read-only** MCP (Model Context Protocol) server for HashiCorp Nomad, built with the official Go MCP SDK (`github.com/modelcontextprotocol/go-sdk`) and the official Nomad API client (`github.com/hashicorp/nomad/api`). It exposes Nomad inspection workflows over `stdio` transport as MCP tools, resources, and prompts. It performs no writes, exposes no event streams / blocking queries, and serves only bounded static log tails (no follow mode or filesystem browsing).

Requires Go 1.26.1.

## Commands

```bash
go build ./cmd/nomad-mcp   # build the server binary
./nomad-mcp                # run over stdio
go test ./...              # run all tests
go test ./internal/server -run TestName   # run a single test
gofmt -w .                 # format
```

The only test package is `internal/server`. The VS Code MCP config (`.vscode/mcp.json`) runs the server via `go run ./cmd/nomad-mcp` as the `nomad-mcp-test` server.

Docker: `docker build -t nomad-mcp .` (multi-stage, distroless static image; entrypoint is the binary over stdio).

## Configuration

Connection is configured entirely through Nomad's standard environment variables via `api.DefaultConfig()` — `NOMAD_ADDR`, `NOMAD_TOKEN`, `NOMAD_NAMESPACE`, `NOMAD_REGION`, and the `NOMAD_*` TLS variables. There is no separate config file. Set `NOMAD_MCP_LOG_LEVEL` (`debug`/`info`/`warn`/`error`) to control verbosity.

## Architecture

Flow: `cmd/nomad-mcp/main.go` builds the logger and a Nomad client, then calls `server.Run`, which constructs an `mcp.Server`, registers everything, and serves over `mcp.StdioTransport`.

- **`internal/nomad` — the central abstraction.** `Facade` is an interface enumerating every Nomad API call the server makes; `Client` is the real implementation wrapping `*api.Client`. **Everything downstream depends on `Facade`, not the concrete client.** Adding a new Nomad-backed capability almost always starts by adding a method to `Facade` (and to `fakeNomadClient` in the test). `GetAllocationLogs` is the one method with real logic here (bounds log tails to `maxLogTailLines`).
- **`internal/server`** — assembles the server and installs `loggingMiddleware` (a `ReceivingMiddleware` that logs each request's method/tool/duration). `server_test.go` drives the full server against an in-process `fakeNomadClient` implementing `Facade`.
- **`internal/tools`** — all MCP surface area, despite the package name covering tools *and* resources *and* prompts:
  - `register.go` / `cluster.go` / `jobs.go` / `allocations.go` — tools, registered by domain.
  - `resources.go` — registers `nomad://...` resource templates; URIs are parsed and routed in `readNomadResource`.
  - `prompts.go` — registers guided diagnostic prompts that embed resources by URI.
  - `helpers.go` — shared plumbing (see patterns below).
- **`internal/logging`** — `slog` text handler writing to **stderr**. This is deliberate: `stdout` is reserved for the MCP `stdio` protocol, so all operational logging must stay on stderr.

## Conventions and patterns

- **Tool registration** goes through the generic `addTool[In, Out]` helper in `helpers.go`, not `mcp.AddTool` directly. It builds the input JSON schema from the `In` type via reflection (`jsonschema.ForType`) and `ensureObjectProperties` (a workaround that fills in empty `properties` so generated schemas validate). Input structs use `json` + `jsonschema` struct tags; embed `listQueryInput` / `objectQueryInput` to get the standard namespace/region/pagination/stale-read inputs and their `queryOptions()` conversion.
- **Errors are returned to the model, not as Go errors.** Tool handlers return `failResult(err)` (a `*mcp.CallToolResult` with `IsError: true`) and a `nil` error, so middleware sees success. `userFacingError` sanitizes messages (e.g. ACL permission denials). Only return a real Go error for programmer/argument errors.
- Always thread the request context: call `input.queryOptions().WithContext(ctx)`.
- Output is a structured `map[string]any` plus a human-readable summary string built with helpers like `summarizeList`, `metaMap`, `metadataSummary`, and the `deref*` pointer helpers.
- **Read-only is a hard invariant.** Do not add tools/resources that mutate Nomad, stream events, use blocking queries, or follow logs — these are intentionally out of scope.
- For discovery questions, `list_jobs` is the preferred entry point; it accepts native Nomad `filter` expressions (e.g. `Meta.department == "financial"`) and returns each job's `meta` map plus a stable `meta_summary` string.

This project is developed via agentic / vibe coding.
