# freebuff-proxy

`freebuff-proxy` is a small Go service that exposes a local OpenAI- and Anthropic-compatible HTTP surface in front of the Freebuff/Codebuff session infrastructure. It lets you sign in to a Freebuff account via CLI, securely use the credential stored in `credentials.json`, keep the session alive, and serve compatible endpoints at `/v1/models`, `/v1/chat/completions`, `/v1/messages`, and `/v1/messages/count_tokens`.

Chat endpoints accept OpenAI or Anthropic-shaped requests, prepare the Freebuff session, start an agent run via `/api/v1/agent-runs` (like the Codebuff CLI flow), send the request to the verified upstream chat route with `codebuff_metadata`, and convert the response into `chat.completion`, OpenAI SSE chunk, or Anthropic Messages/SSE format according to the client contract.

> 🇹🇷 Türkçe doküman için bkz. [README.TR.md](README.TR.md)

## Requirements

- Go 1.25+
- A Freebuff/Codebuff account
- Docker CLI and a running daemon (only if you want to build a container image)

The HTTP layer runs on Fiber v3. On startup the project root `.env` file is loaded automatically via `github.com/joho/godotenv`; process env values take precedence over `.env` entries.

## Build from source

```bash
go test ./...
go vet ./...
go build -o bin/freebuff-proxy ./cmd/freebuff-proxy
```

Makefile targets:

```bash
make test
make vet
make build
make run
make run-bin
make doctor
make smoke-openai
make smoke-anthropic
make docker
```

`make build` produces the binary at `bin/freebuff-proxy`. `make run-bin` builds then runs `./bin/freebuff-proxy serve`. `make doctor` inspects the process on port `1455` and stale root binary artifacts. `make smoke-openai` and `make smoke-anthropic` send sample requests to the running local proxy. To install directly:

```bash
go install ./cmd/freebuff-proxy
```

## Environment variables

| Variable | Default | Description |
| --- | --- | --- |
| `FREEBUFF_PROXY_ADDR` | `127.0.0.1:1455` | HTTP listen address. Use `0.0.0.0:1455` to expose the port inside a container. |
| `FREEBUFF_API_BASE_URL` | `https://www.codebuff.com` | Freebuff/Codebuff API base URL for login, logout, and session endpoints. |
| `FREEBUFF_MODEL` | `deepseek/deepseek-v4-pro` | Model name listed in `/v1/models` and used by sample requests. |
| `FREEBUFF_PROXY_API_KEY` | *(empty)* | When set, the proxy requires `Authorization: Bearer <value>` or `x-api-key: <value>` on every endpoint. Use as `api_key` for OpenAI-compatible clients or `ANTHROPIC_AUTH_TOKEN` / `ANTHROPIC_API_KEY` on the Anthropic/Claude Code side. |
| `FREEBUFF_CREDENTIALS_PATH` | `$HOME/.config/manicode/credentials.json` | Path to the credential file written after login and read during `serve`. |

Example `.env`:

```dotenv
FREEBUFF_PROXY_ADDR=127.0.0.1:1455
FREEBUFF_API_BASE_URL=https://www.codebuff.com
FREEBUFF_MODEL=deepseek/deepseek-v4-pro
FREEBUFF_PROXY_API_KEY=local-proxy-key
FREEBUFF_CREDENTIALS_PATH=$HOME/.config/manicode/credentials.json
```

Do not commit real production secrets to `.env`; prefer a secret manager or runtime env in CI/container environments.

## Quick start

1. Build the binary:

   ```bash
   make build
   ```

2. Export local settings (or write them to `.env`):

   ```bash
   export FREEBUFF_PROXY_ADDR=127.0.0.1:1455
   export FREEBUFF_API_BASE_URL=https://www.codebuff.com
   export FREEBUFF_MODEL=deepseek/deepseek-v4-pro
   export FREEBUFF_PROXY_API_KEY=local-proxy-key
   export FREEBUFF_CREDENTIALS_PATH="$HOME/.config/manicode/credentials.json"
   ```

3. Sign in:

   ```bash
   ./bin/freebuff-proxy login
   ```

   The command prints a login link. Open it in your browser, authenticate, and the CLI writes a credential file to `FREEBUFF_CREDENTIALS_PATH` once verification completes.

4. Start the proxy:

   ```bash
   ./bin/freebuff-proxy serve
   ```

   In another terminal, run quick checks:

   ```bash
   make smoke-openai
   make smoke-anthropic
   ```

5. Sign out when done:

   ```bash
   ./bin/freebuff-proxy logout
   ```

   Logout tears down the upstream session and removes the local credential file. To stop the proxy process only, press `Ctrl-C`.

## Login flow

1. Start login:
   ```bash
   freebuff-proxy login
   ```
2. The command prints a Freebuff login URL.
3. Open the URL in a browser and authenticate.
4. The CLI polls the status endpoint until verification completes.
5. On success a credential is written to `$HOME/.config/manicode/credentials.json` by default.
6. The file is created atomically with `0600` permissions.

Custom credential path:

```bash
FREEBUFF_CREDENTIALS_PATH=/secure/path/credentials.json freebuff-proxy login
```

Sign out:

```bash
freebuff-proxy logout
```

Logout calls the upstream logout endpoint with stored metadata and removes the local credential file on success.

## Serve command

Start the local service:

```bash
make build
./bin/freebuff-proxy serve
```

Or with a single Makefile target:

```bash
make run-bin
```

Run from source during development:

```bash
make run
```

The default address is `127.0.0.1:1455` so the service is only reachable from localhost. To use a different address or require an API key:

```bash
FREEBUFF_PROXY_ADDR=0.0.0.0:1455 \
FREEBUFF_PROXY_API_KEY=local-proxy-key \
./bin/freebuff-proxy serve
```

If a stale `./freebuff-proxy` binary exists in the repo root it may be executed instead of the current `bin/freebuff-proxy`, causing errors like `upstream_chat_route_not_verified`. Diagnose with:

```bash
make doctor
strings ./freebuff-proxy | grep -F upstream_chat_route_not_verified
strings ./bin/freebuff-proxy | grep -F upstream_chat_route_not_verified
```

Health and model endpoints:

```bash
curl http://127.0.0.1:1455/healthz
curl -H 'Authorization: Bearer ***' http://127.0.0.1:1455/v1/models
```

The chat endpoint accepts OpenAI-shaped requests, prepares the Freebuff session, starts an agent run (like the Codebuff CLI flow), and forwards the request to the upstream route with `run_id`, `client_id`, `cost_mode`, and `freebuff_instance_id` metadata fields. Short model aliases like `deepseek-v4-pro` and `deepseek-v4-flash` are resolved to their canonical `deepseek/...` form in the upstream request:

```bash
curl -X POST http://127.0.0.1:1455/v1/chat/completions \
  -H 'Authorization: Bearer ***' \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek/deepseek-v4-pro","messages":[{"role":"user","content":"Hello"}]}'
```

A successful call returns `object: "chat.completion"` with the assistant reply in `choices[0].message.content`:

```json
{
  "object": "chat.completion",
  "model": "deepseek/deepseek-v4-pro",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello!"
      },
      "finish_reason": "stop"
    }
  ]
}
```

Streaming:

```bash
curl -N -X POST http://127.0.0.1:1455/v1/chat/completions \
  -H 'Authorization: Bearer ***' \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek/deepseek-v4-pro","stream":true,"messages":[{"role":"user","content":"Hello"}]}'
```

Stream responses are delivered as `Content-Type: text/event-stream` with `data: {...}` chunks terminated by `data: [DONE]`.

Smoke targets for quick verification:

```bash
make smoke-openai
make smoke-anthropic

PROXY_URL=http://127.0.0.1:1455 \
PROXY_API_KEY=local-proxy-key \
SMOKE_MODEL=deepseek/deepseek-v4-pro \
make smoke-anthropic
```

## OpenAI-compatible client example

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:1455/v1",
    api_key="local-proxy-key",
)

response = client.chat.completions.create(
    model="deepseek/deepseek-v4-pro",
    messages=[{"role": "user", "content": "Hello"}],
)

print(response.choices[0].message.content)
```

## Codex and OpenAI-compatible usage

For OpenAI Chat Completions compatible clients set `base_url` to `http://127.0.0.1:1455/v1` and `api_key` to your `FREEBUFF_PROXY_API_KEY` value.

```bash
export OPENAI_BASE_URL=http://127.0.0.1:1455/v1
export OPENAI_API_KEY=local-proxy-key
```

Chat Completions clients can then use `model=deepseek/deepseek-v4-pro` through the proxy.

> **Note:** The current OpenAI Codex CLI may use the Responses API (`/v1/responses`). This proxy does not yet serve that endpoint. If your Codex version supports Chat Completions or a custom provider, the `OPENAI_BASE_URL` / `OPENAI_API_KEY` settings above should work; Responses API support needs to be added separately.

## Anthropic and Claude Code example

Anthropic Messages-compatible surfaces:

- `POST /v1/messages`
- `POST /v1/messages/count_tokens`
- `GET /v1/models`

`/v1/messages` converts Anthropic messages (string or `type: "text"` content blocks) to OpenAI-compatible chat requests. The response is returned as an Anthropic `message` object (non-stream) or `message_start`, `content_block_delta`, `message_delta`, and `message_stop` SSE events (stream). Tool definitions are best-effort converted to OpenAI function tool format; the current upstream chat service returns text responses so Anthropic `tool_use` response blocks are not produced in this version.

Simple Anthropic curl:

```bash
curl -X POST http://127.0.0.1:1455/v1/messages \
  -H 'x-api-key: local-proxy-key' \
  -H 'anthropic-version: 2023-06-01' \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek/deepseek-v4-pro","max_tokens":256,"messages":[{"role":"user","content":"Hello"}]}'
```

Stream example:

```bash
curl -N -X POST http://127.0.0.1:1455/v1/messages \
  -H 'x-api-key: local-proxy-key' \
  -H 'anthropic-version: 2023-06-01' \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek/deepseek-v4-pro","max_tokens":256,"stream":true,"messages":[{"role":"user","content":"Hello"}]}'
```

Claude Code local gateway setup:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:1455
export ANTHROPIC_AUTH_TOKEN=local-proxy-key
export ANTHROPIC_MODEL=deepseek/deepseek-v4-pro
export ANTHROPIC_SMALL_FAST_MODEL=deepseek/deepseek-v4-flash
export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1
```

Then start Claude Code in the same terminal session:

```bash
claude
```

If the proxy API key is empty `ANTHROPIC_AUTH_TOKEN` is not required; however providing a non-empty local value on the Claude Code side makes client behavior more predictable.

## Downloading release binaries

Pre-built binaries for Linux, macOS, and Windows are published on the [GitHub Releases](https://github.com/ferdiunal/freebuff-proxy/releases) page.

Example download:

```bash
gh release download v0.1.0 \
  --repo ferdiunal/freebuff-proxy \
  --pattern "*darwin-arm64*" \
  --dir dist

tar -xzf dist/freebuff-proxy-darwin-arm64.tar.gz -C dist
./dist/freebuff-proxy-darwin-arm64 serve
```

Release assets contain only the compiled `freebuff-proxy` binary; credential files are never included.

## Docker

Build the image:

```bash
docker build -t freebuff-proxy:local .
```

Or via Makefile:

```bash
make docker
```

Run:

```bash
docker run --rm \
  -p 1455:1455 \
  -e FREEBUFF_PROXY_ADDR=0.0.0.0:1455 \
  -e FREEBUFF_PROXY_API_KEY=local-proxy-key \
  -v "$HOME/.config/manicode:/home/freebuff/.config/manicode:ro" \
  freebuff-proxy:local serve
```

If you need to generate the credential file inside the container, mount the volume as read-write. In production, prefer mounting the credential file read-only.

## Release automation

Pushing a `v*` tag triggers the GitHub Actions release workflow (`.github/workflows/release-proxy.yml`). The workflow runs `go test` and `go vet`, then builds binaries for:

- `linux-amd64`
- `linux-arm64`
- `darwin-amd64`
- `darwin-arm64`
- `windows-amd64`

Each binary is packaged as `.tar.gz` and published as a GitHub Release asset using the default `GITHUB_TOKEN`. No custom secrets are required.

Manual trigger:

```bash
gh workflow run release-proxy.yml -f tag=v0.1.0
```

## Security notes

- Keep `credentials.json` secret; it contains your Freebuff session token.
- Do not store `credentials.json` in the repo root or commit it to git. The safe default path is `$HOME/.config/manicode/credentials.json`.
- If the credential file is accidentally pushed to a public repository, rotate/revoke the token and scrub it from git history.
- The credential file should be stored with `0600` permissions. The application applies this permission on files it creates; verify permissions on externally mounted files.
- Session tokens must not be printed to terminals, logs, error reports, or CI output.
- In externally exposed deployments, set `FREEBUFF_PROXY_API_KEY` and require clients to send `Authorization: Bearer <value>`.
- The proxy API key does not replace the upstream Freebuff token; it only protects the local proxy surface.
- When running in containers, mount the credential volume read-only if possible and restrict access to the necessary user only.
