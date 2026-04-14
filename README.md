# nomad-mcp

`nomad-mcp` is a read-only MCP server for HashiCorp Nomad built with the official Go MCP SDK and the official Nomad API client.

> **[!NOTE]** 
> This project was developed via agentic / vibe coding

## Scope

The initial implementation targets `stdio` transport and read-only inspection workflows.

Supported tools:

- `get_cluster_status`
- `list_regions`
- `list_namespaces`
- `list_nodes`
- `get_node`
- `list_jobs`
- `get_job`
- `get_job_scale_status`
- `get_job_evaluations`
- `get_job_allocations`
- `get_job_services`
- `list_deployments`
- `get_deployment`
- `list_allocations`
- `get_allocation`
- `get_allocation_checks`

`list_jobs` now includes each job's `meta` map and a stable `meta_summary` string so clients can answer discovery questions from a single tool call.

Supported resources:

- `nomad://cluster/summary`
- `nomad://jobs/{namespace}/{job_id}/summary`
- `nomad://jobs/{namespace}/{job_id}/spec`
- `nomad://jobs/{namespace}/{job_id}/evaluations`
- `nomad://allocs/{allocation_id}/status`
- `nomad://allocs/{allocation_id}/checks`
- `nomad://allocs/{allocation_id}/logs/{task_name}/{stream}`
- `nomad://nodes/{node_id}/status`
- `nomad://nodes/{node_id}/allocations`
- `nomad://deployments/{deployment_id}`
- `nomad://evaluations/{evaluation_id}`

Supported prompts:

- `debug_allocation`
- `explain_evaluation`
- `diagnose_deployment`
- `investigate_node`
- `explain_job`

## Configuration

The server uses Nomad's standard environment variables through `github.com/hashicorp/nomad/api.DefaultConfig()`.

Operational logs are emitted with Go's `log/slog` package to `stderr`, so they do not interfere with the MCP `stdio` protocol on `stdout`. Set `NOMAD_MCP_LOG_LEVEL` to `debug`, `info`, `warn`, or `error` to adjust verbosity.

Common variables:

- `NOMAD_ADDR`
- `NOMAD_TOKEN`
- `NOMAD_NAMESPACE`
- `NOMAD_REGION`
- `NOMAD_CACERT`
- `NOMAD_CAPATH`
- `NOMAD_CLIENT_CERT`
- `NOMAD_CLIENT_KEY`
- `NOMAD_TLS_SERVER_NAME`
- `NOMAD_SKIP_VERIFY`

Per-tool inputs can override `namespace`, `region`, pagination, filtering, and stale-read behavior where the underlying Nomad endpoint supports those options.

For discovery-style questions, prefer `list_jobs` first. The `filter` input accepts native Nomad filter expressions, for example:

- `Name matches "financial"`
- `Meta.department == "financial"`

That makes questions like "what financial jobs are there" answerable without post-processing MCP output with shell tools.

## Run

Build the server:

```bash
go build ./cmd/nomad-mcp
```

Run it directly over stdio:

```bash
./nomad-mcp
```

## Docker

Build the image:

```bash
docker build -t nomad-mcp .
```

Run it over stdio:

```bash
docker run --rm -i \
	-e NOMAD_ADDR=https://nomad.example.com \
	-e NOMAD_TOKEN=... \
	nomad-mcp
```

The container uses the same `NOMAD_*` environment variables as the local binary.

## Notes

- This MVP does not perform writes.
- This MVP does not expose event streams or blocking-query watch semantics.
- Allocation logs are exposed only as bounded static tails through resources and prompts. Follow mode and filesystem browsing are still excluded.