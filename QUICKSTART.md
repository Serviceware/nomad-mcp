# Quickstart: use nomad-mcp with Claude

`nomad-mcp` is a read-only stdio MCP server. Claude starts it as a subprocess, and the subprocess
connects to Nomad using the standard `NOMAD_*` environment variables. Setup has three steps: pick
how to launch the server, give it a Nomad address and credentials, and register it with Claude.

## 1. Choose how to launch it

**A. Prebuilt binary.** This is the fastest to start and doesn't need Go at runtime.

```bash
go build -o nomad-mcp ./cmd/nomad-mcp    # nomad-mcp.exe on Windows
```

Use the **absolute path** of the binary as the command, e.g. `/usr/local/bin/nomad-mcp`.

**B. Checked-out repo with `go run`.** Every start uses the current source, which is good for
development. It needs Go 1.27.1+.

```bash
go -C /path/to/nomad-mcp run ./cmd/nomad-mcp
```

`-C` makes `go` switch to the repo first, so this works no matter what directory Claude starts it
from. The first start compiles the server and can take a few seconds. If Claude gives up before it
connects, raise the startup timeout with `MCP_TIMEOUT=30000 claude`.

## 2. Configure the Nomad connection

| Variable | Required | Purpose |
|---|---|---|
| `NOMAD_ADDR` | yes | Nomad API address, e.g. `https://nomad.example.com:4646` |
| `NOMAD_TOKEN` | if ACLs are enabled | ACL token; read-only policies are enough |
| `NOMAD_CACERT` | for TLS | CA certificate (PEM) that signed the Nomad server cert |
| `NOMAD_CAPATH` | for TLS | Directory of CA certificates, as an alternative to `NOMAD_CACERT` |
| `NOMAD_CLIENT_CERT` | for mTLS | Client certificate (PEM) |
| `NOMAD_CLIENT_KEY` | for mTLS | Client private key (PEM) |
| `NOMAD_TLS_SERVER_NAME` | optional | Hostname to verify, if it differs from the one in `NOMAD_ADDR` |
| `NOMAD_NAMESPACE` / `NOMAD_REGION` | optional | Defaults; individual tool calls can override them |
| `NOMAD_MCP_LOG_LEVEL` | optional | `debug` / `info` / `warn` / `error`; logs go to stderr |

Use absolute paths for the certificate files. Avoid `NOMAD_SKIP_VERIFY=true`: it turns off TLS
verification entirely.

## 3. Add it to Claude Code

The examples use mTLS. Drop the lines your cluster doesn't need.

**Binary:**

```bash
claude mcp add nomad -s user \
  -e NOMAD_ADDR=https://nomad.example.com:4646 \
  -e NOMAD_CACERT=/path/to/tls/ca.pem \
  -e NOMAD_CLIENT_CERT=/path/to/tls/cert.pem \
  -e NOMAD_CLIENT_KEY=/path/to/tls/key.pem \
  -e NOMAD_TOKEN=your-token \
  -- /usr/local/bin/nomad-mcp
```

**Checked-out repo:**

```bash
claude mcp add nomad -s user \
  -e NOMAD_ADDR=https://nomad.example.com:4646 \
  -e NOMAD_CACERT=/path/to/tls/ca.pem \
  -e NOMAD_CLIENT_CERT=/path/to/tls/cert.pem \
  -e NOMAD_CLIENT_KEY=/path/to/tls/key.pem \
  -- go -C /path/to/nomad-mcp run ./cmd/nomad-mcp
```

- The server name (`nomad`) has to come **before** the `-e` flags, and `--` separates Claude's
  flags from the server command.
- `-s user` makes the server available in every project. Without it, the server is only registered
  for the directory you ran the command in.
- On Windows, `\` line continuation only works in Git Bash. In PowerShell, use a backtick (`` ` ``)
  or write it on one line. Windows paths such as `C:\certs\ca.pem` work as-is.

Check that it connects:

```bash
claude mcp list        # nomad: ... - ✔ Connected
```

Inside Claude Code, `/mcp` shows the server's status, tools, resources and prompts.

### Share it with a team through the repo

If you add the server with `-s project` from inside the checkout, Claude Code writes it to
`.mcp.json` in the repo root. Start `claude` from the repo root so the relative `./cmd/nomad-mcp`
path resolves. Reference secrets as `${VAR}` and don't hardcode them; each person then sets them in
their own shell before starting Claude:

```json
{
  "mcpServers": {
    "nomad": {
      "command": "go",
      "args": ["run", "./cmd/nomad-mcp"],
      "env": {
        "NOMAD_ADDR": "${NOMAD_ADDR}",
        "NOMAD_TOKEN": "${NOMAD_TOKEN:-}",
        "NOMAD_CACERT": "${NOMAD_CACERT:-}",
        "NOMAD_CLIENT_CERT": "${NOMAD_CLIENT_CERT:-}",
        "NOMAD_CLIENT_KEY": "${NOMAD_CLIENT_KEY:-}"
      }
    }
  }
}
```

`${VAR:-}` falls back to an empty value, which is fine for clusters without TLS or ACLs. The first
time someone starts `claude` in the repo, Claude asks them to approve the project server. Until then,
`claude mcp list` shows it as `⏸ Pending approval`.

## Add it to Claude Desktop

Edit `claude_desktop_config.json`, then restart Claude Desktop:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "nomad": {
      "command": "/usr/local/bin/nomad-mcp",
      "env": {
        "NOMAD_ADDR": "https://nomad.example.com:4646",
        "NOMAD_CACERT": "/path/to/tls/ca.pem",
        "NOMAD_CLIENT_CERT": "/path/to/tls/cert.pem",
        "NOMAD_CLIENT_KEY": "/path/to/tls/key.pem",
        "NOMAD_TOKEN": "your-token"
      }
    }
  }
}
```

For the checked-out repo, use `"command": "go"` and
`"args": ["-C", "/path/to/nomad-mcp", "run", "./cmd/nomad-mcp"]`. In JSON, Windows backslashes
must be doubled (`"C:\\certs\\ca.pem"`), or use forward slashes (`"C:/certs/ca.pem"`).

## Try it

Ask Claude something like:

- "What's the status of the Nomad cluster?"
- "Which jobs are failing in namespace `default`?"
- "Why did allocation `<id>` crash?" (uses the `debug_allocation` prompt with a bounded log tail)

## Troubleshooting

- **`✘ Failed to connect`.** Run the exact command from the config in a terminal with the same
  environment variables. The server should start and then wait for input on stdin; press Ctrl+C to
  stop it. Errors appear on stderr. Add `NOMAD_MCP_LOG_LEVEL=debug` for more detail.
- **`x509: certificate signed by unknown authority`.** `NOMAD_CACERT` points at the wrong CA, or
  the path isn't absolute.
- **`remote error: tls: certificate required`.** The cluster requires mTLS. Set
  `NOMAD_CLIENT_CERT` and `NOMAD_CLIENT_KEY`.
- **"Nomad rejected the request due to insufficient ACL permissions."** The token is missing or
  lacks read access to that namespace.
- **The token is stored in plain text.** `claude mcp add -e` saves values in `~/.claude.json`. For
  shared or committed configs, use `${NOMAD_TOKEN}` expansion in `.mcp.json`.
