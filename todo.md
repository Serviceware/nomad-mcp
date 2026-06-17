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

- [ ] Only `internal/server` has tests. `internal/nomad`, `internal/tools`, `internal/logging`,
  and `cmd/` have none.
- [ ] **`GetAllocationLogs` is never tested** — the one method with real logic (line-bound clamping,
  `Truncated` flag, stdout/stderr defaulting, byte budget). The fake bypasses it entirely.
- [ ] **Error paths untested** — no test drives a `Facade` method returning an error to verify
  `failResult` / `userFacingError` (ACL-denial sanitization, `IsError` flag).
- [ ] **Resource URI routing barely covered** — only `nomad://jobs/default/example/summary` is read.
  The dispatch in `readNomadResource` (10 templates, segment-count branches, 4-segment log path)
  and `ResourceNotFoundError` for malformed URIs are untested. A node-status resource test would
  have caught the nil-deref above.
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

- [ ] **Duplicate identical helpers** — `formatSubmitTime` and `formatUnixNanos` are byte-for-byte
  identical (`internal/tools/helpers.go:240-252`). Consolidate.
- [ ] **`userFacingError` "not found" branch is a no-op** — returns `message` unchanged, identical
  to `default` (`internal/tools/helpers.go:192-195`).
- [ ] **Leftover compile-anchor** — `var _ = api.AllNamespacesNamespace` (`internal/tools/cluster.go:177`)
  is a no-op; replace with a real comment or remove.
- [ ] **Redundant `query.Params` init in `list_nodes`** (`internal/tools/cluster.go:91-94`) — no other
  list tool does this; make it consistent.

## Dependencies / build

- [ ] **`nomad/api` pinned to a pseudo-version** (`v0.0.0-20260317185003-...`), not a tagged release —
  a moving target with no semver guarantees. Pin to a tagged Nomad API release if available.
- [ ] **`go 1.26.1` full-patch pin** in `go.mod` may cause friction for contributors on slightly
  older patch toolchains; consider `go 1.26`.
- [ ] Dockerfile is solid (distroless static, multistage, `-trimpath -ldflags='-s -w'`); optionally
  add image labels / a version build-arg.
