# nomad-mcp — Improvement TODO

Review snapshot of the read-only Nomad MCP server. Build, `go vet`, `gofmt -l`, and
`go test ./...` are all currently clean — the items below are improvements, not breakages.

## Medium priority

- [ ] **`withContext` is a no-op** — `internal/nomad/client.go:255`
  `func withContext(query *api.QueryOptions) *api.QueryOptions { return query }` does nothing.
  Context is actually threaded by callers via `.WithContext(ctx)` in the tools layer. Remove the
  dead indirection (or make it meaningful).

- [ ] **`Leader`/`Peers`/`Regions` are not cancellable** — `internal/nomad/client.go:87-100`
  These three calls accept and thread no context, breaking the "always thread context" invariant
  in CLAUDE.md. Plumb a `context.Context` through them.

- [ ] **Error-message sanitization is inconsistent across surfaces** — resources/prompts return
  raw Go errors straight from the Nomad API (e.g. `internal/tools/resources.go:185-194`), while
  tools sanitize via `failResult` + `userFacingError`. Raw API errors can leak addresses / ACL
  details on the resource and prompt paths. Align the error contract across tools, resources, prompts.

- [ ] **`GetAllocationLogs` error propagation is best-effort** — `internal/nomad/client.go:207-241`
  After the `for frame := range frames` loop drains, a late error delivered on `errCh` is dropped
  by the non-blocking `select`/`default`. Bounded-tail behavior is correct, but errors can be silently lost.

## Test coverage gaps

- [ ] `internal/tools`, `internal/logging`, and `cmd/` have no tests. (`internal/nomad` now has
  `logtail_test.go`.)
- [ ] **`GetAllocationLogs` is never tested end to end** — stdout/stderr defaulting, the byte
  budget, and the `Truncated` flag still go untested; the fake bypasses the method entirely.
  Line trimming is covered by `trimLogTail` / `lastLines` unit tests.
- [ ] **Error paths untested** — no test drives a `Facade` method returning an error to verify
  `failResult` / `userFacingError` (ACL-denial sanitization, `IsError` flag).
- [ ] **Resource URI routing still thinly covered** — the alloc status/checks/logs paths and a
  percent-encoded job ID are now read, but `ResourceNotFoundError` for malformed URIs and several
  of the 10 templates remain untested.
- [ ] **Helpers untested** — `parsePromptTailLines`, `normalizeLogStream`, `metadataSummary`,
  `defaultTaskName`, the `deref*` helpers.
- [ ] Tests assert `must.Positive(t, len(...))` ("something came back") rather than exact tool/
  resource/prompt counts — a silently dropped registration would not fail.

## Documentation

- [ ] **No godoc comments anywhere** — no package docs and no exported-symbol docs on `Facade`,
  `Client`, `New`, `Run`, `Register`, `RegisterResources`, `RegisterPrompts`, `logging.New`,
  `AllocationLogTail`. CLAUDE.md is rich but the source itself is undocumented.
- [ ] README: clarify logs are intentionally resource/prompt-only (there is no `get_allocation_logs` tool).
- [ ] README: add a Testing / Development section (`go test`, single-test run, `gofmt`).
- [ ] README: add a security note that `NOMAD_SKIP_VERIFY` disables TLS verification (currently
  listed neutrally at README line ~72).
- [ ] Document that an invalid `NOMAD_MCP_LOG_LEVEL` silently falls back to `info`
  (`internal/logging/logger.go:21-23`).
- [ ] No `LICENSE` file present.

## Tooling / CI

- [ ] **No CI** — add a workflow running `go build`, `go vet`, `gofmt -l`, `go test`, and a linter.
- [ ] **No linter config** — add `golangci-lint` / `staticcheck` (would likely flag the duplicate
  helpers and the `count` type mismatch).
- [ ] **No Makefile / task runner** — codify the canonical commands from CLAUDE.md.
- [ ] **Version is hardcoded** — `serverVersion = "0.1.0"` (`internal/server/server.go:16`) is not
  wired to git tags via `-ldflags -X`.
- [ ] `.gitignore` ignores `coverage.out` but there is no coverage target to produce it.

## Code cleanup (low priority)

- [x] **Duplicate identical helpers** — `formatSubmitTime` and `formatUnixNanos` were byte-for-byte
  identical. Consolidated onto `formatUnixNanos`.
- [ ] **`userFacingError` "not found" branch is a no-op** — returns `message` unchanged, identical
  to `default` (`internal/tools/helpers.go:192-195`).
- [ ] **Leftover compile-anchor** — `var _ = api.AllNamespacesNamespace` (`internal/tools/cluster.go:177`)
  is a no-op; replace with a real comment or remove.
- [ ] **Redundant `query.Params` init in `list_nodes`** (`internal/tools/cluster.go:91-94`) — no other
  list tool does this; make it consistent.

## Dependencies / build

- [ ] **`nomad/api` pinned to a pseudo-version** (`v0.0.0-20260317185003-...`), not a tagged release —
  a moving target with no semver guarantees. Pin to a tagged Nomad API release if available.
- [ ] **`go 1.27.1` full-patch pin** in `go.mod` may cause friction for contributors on slightly
  older patch toolchains; consider `go 1.27`. Note the official `golang` images set
  `GOTOOLCHAIN=local`, so the Dockerfile base image has to match the pin exactly.
- [ ] Dockerfile is solid (distroless static, multistage, `-trimpath -ldflags='-s -w'`); optionally
  add image labels / a version build-arg.

## Follow-ups from the 2026-10-05 review pass

- [ ] **`Out` is `map[string]any` for every tool**, so each advertised `outputSchema` is a generic
  object. Typed output structs per tool would give clients a real schema to validate and plan
  against. This is a large, mechanical change across `cluster.go` / `jobs.go` / `allocations.go`
  and was deliberately deferred.

### Resolved in that pass

- [x] Dockerfile base image was `golang:1.26.1` against a `go 1.27.1` `go.mod` — with
  `GOTOOLCHAIN=local` in the official images the build could not succeed.
- [x] `allocationStatusResource`, `allocationChecksResource`, the allocation logs resource, the
  `get_allocation_checks` tool, and the `debug_allocation` prompt all queried namespace-scoped
  endpoints under the default namespace. All now re-scope via `queryForAllocation`.
- [x] Job IDs containing `/` (periodic and dispatched children) could not be addressed: prompts
  built URIs without escaping, and `resourcePathSegments` split the already-decoded path.
- [x] `debug_allocation` dereferenced a possibly-nil `AllocationLogTail`.
- [x] Tool `Content` held only the summary line, so clients ignoring `structuredContent` got no
  data. Now `structuredResult` appends the serialized payload.
- [x] No tool advertised `ToolAnnotations`; `addTool` now stamps read-only/idempotent/open-world.
- [x] The log tail was byte-bounded but not line-bounded; `trimLogTail` drops the leading partial
  line and trims to the applied line count.
- [x] Empty `job_id` / `node_id` / `deployment_id` / `allocation_id` were forwarded to Nomad.
- [x] The check-listing and job-summary payloads were duplicated between a tool and a resource.
