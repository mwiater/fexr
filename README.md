# fexr

fexr is a Go-based Model Context Protocol (MCP) server for retrieving and exploring current public web content. It exposes seven read-only tools over a JSON-RPC 2.0 HTTP endpoint at `/mcp`.

| Tool | Purpose |
| --- | --- |
| `fetch_url_as_text` | Fetch static HTML and convert it to plain text. |
| `fetch_rss_as_JSON` | Fetch and parse RSS or Atom as structured JSON. |
| `fetch_url_as_JSON` | Fetch and validate a public JSON response. |
| `geocode` | Resolve a place name through Open-Meteo. |
| `weather` | Return current conditions and a five-day forecast. |
| `browse` | Render one JavaScript-enabled page and return text and links. |
| `search_site` | Discover relevant content across several pages on one site. |

fexr does not bypass authentication, CAPTCHAs, paywalls, or other access restrictions.

## Natural Tool Calling

Users can ask questions such as:

> Explore IANA's example-domain documentation and summarize the related information about reserved domains and protocol registries.

They should not need to name `search_site`. The MCP client gives the model the definitions returned by `tools/list`; the model selects a tool and constructs its arguments.

The normal flow is:

1. The user asks a natural-language question.
2. The model recognizes that current external data is needed.
3. It selects a fexr tool from the tool descriptions and schemas.
4. The MCP client sends `tools/call` to fexr.
5. fexr retrieves and normalizes the data.
6. The model interprets the result and answers the user.

For best results, enable automatic tool selection in the MCP host and add an instruction like:

```text
Use fexr automatically when an answer requires current information from a
public website or URL. Do not require the user to name a tool. Prefer browse
for one JavaScript-rendered page, search_site when links may need to be
followed, fetch_url_as_text for static HTML, and the JSON or RSS tools for
matching structured endpoints.
```

## Requirements

- The Go version declared by `go.mod` (currently Go 1.26.1)
- `curl` and `html2text`
- The Playwright driver and Chromium for `browse` and `search_site`

Ubuntu/Debian installation:

```bash
sudo apt update
sudo apt install -y curl html2text
go run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6201.0 install --with-deps chromium
```

The Playwright version must match the `playwright-go` version in `go.mod`.

## Run, Test, and Build

```bash
go run .
go test ./...
goreleaser release --snapshot --clean --skip=publish
```

GoReleaser runs `go test ./...` as a pre-build hook. If the Go test suite fails, the release build stops before producing artifacts.

fexr listens on `:4002`, which binds all available interfaces. Restrict access with firewall rules or a reverse proxy when appropriate.

## MCP Protocol

Initialize:

```bash
curl -sS http://127.0.0.1:4002/mcp \
  -H 'Content-Type: application/json' \
  --data '{
    "jsonrpc":"2.0","id":1,"method":"initialize",
    "params":{"protocolVersion":"2025-03-26","capabilities":{},
    "clientInfo":{"name":"example-client","version":"1.0.0"}}
  }'
```

List tools:

```bash
curl -sS http://127.0.0.1:4002/mcp \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
```

Tool calls use this envelope:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {"name": "TOOL_NAME", "arguments": {}}
}
```

## Tool Reference and Examples

The outputs below are abbreviated and representative. Live content and values will change.

### `fetch_url_as_text`

Fetches static HTML with `curl`, converts it with `html2text`, and returns plain text.

Input: `url` (required HTTP/HTTPS URL).

Example prompt:

> Summarize the text on https://example.com.

Steps: the model identifies a static page, calls `fetch_url_as_text`, receives converted text, and summarizes it.

```json
{"name":"fetch_url_as_text","arguments":{"url":"https://example.com"}}
```

Representative output:

```json
{"content":[{"type":"text","text":"Example Domain\n\nThis domain is for use in illustrative examples..."}]}
```

### `fetch_rss_as_JSON`

Fetches and parses RSS or Atom with `gofeed`. It returns normalized data in `structuredContent` and formatted JSON in `content`.

Input: `url` (required RSS/Atom HTTP/HTTPS URL).

Example prompt:

> What are the newest posts in https://hnrss.org/frontpage?

Steps: the model recognizes a feed, calls `fetch_rss_as_JSON`, then presents the newest item titles and links.

```json
{"name":"fetch_rss_as_JSON","arguments":{"url":"https://hnrss.org/frontpage"}}
```

Representative output:

```json
{
  "structuredContent":{"title":"Hacker News: Front Page","items":[{"title":"Example story","link":"https://example.com/story"}]},
  "content":[{"type":"text","text":"{\n  \"title\": \"Hacker News: Front Page\", ...\n}"}]
}
```

### `fetch_url_as_JSON`

Fetches a JSON endpoint, retries HTTP 502/503/504 responses up to three times, validates successful response content types, and parses the JSON.

Input: `url` (required JSON HTTP/HTTPS endpoint).

Example prompt:

> Show me the public profile returned by https://api.github.com/users/octocat.

Steps: the model recognizes a JSON API, calls `fetch_url_as_JSON`, and extracts useful fields from `structuredContent`.

```json
{"name":"fetch_url_as_JSON","arguments":{"url":"https://api.github.com/users/octocat"}}
```

Representative output:

```json
{
  "structuredContent":{"login":"octocat","html_url":"https://github.com/octocat","type":"User"},
  "content":[{"type":"text","text":"{\n  \"login\": \"octocat\", ...\n}"}]
}
```

### `geocode`

Looks up a place through Open-Meteo and returns up to ten candidates.

Input: `locationname` (required non-empty place name).

Example prompt:

> What are the coordinates of Portland, Oregon?

Steps: the model calls `geocode`, chooses the Oregon candidate, and reports its coordinates.

```json
{"name":"geocode","arguments":{"locationname":"Portland, Oregon"}}
```

Representative output:

```json
{
  "structuredContent":{"results":[{"name":"Portland","latitude":45.52345,"longitude":-122.67621,"admin1":"Oregon","country":"United States"}]},
  "content":[{"type":"text","text":"{\n  \"results\": [...]\n}"}]
}
```

### `weather`

Returns current conditions and a five-day Open-Meteo forecast. Temperatures use Fahrenheit, wind speed mph, precipitation inches, and the timezone is currently fixed to `America/Los_Angeles`.

Inputs: `latitude` and `longitude` (both required numbers).

Example prompt:

> What is the weather at latitude 45.52345 and longitude -122.67621?

Steps: the model calls `weather` directly because coordinates are provided, then translates the returned values and weather codes into a forecast.

```json
{"name":"weather","arguments":{"latitude":45.52345,"longitude":-122.67621}}
```

Representative output:

```json
{
  "structuredContent":{
    "latitude":45.52,"longitude":-122.68,
    "current":{"temperature_2m":65.2,"relative_humidity_2m":58,"wind_speed_10m":4.1,"weather_code":1},
    "daily":{"time":["2026-09-01"],"temperature_2m_max":[78.4],"temperature_2m_min":[55.1]}
  },
  "content":[{"type":"text","text":"{\n  \"current\": {...}, \"daily\": {...}\n}"}]
}
```

For “What is the weather in Portland?”, the model can call `geocode` first, then pass the selected coordinates to `weather`.

### `browse`

Loads one page in an isolated headless Chromium context, executes JavaScript, and returns visible text and links.

Inputs:

- `url` — required HTTP/HTTPS URL.
- `wait_for_selector` — optional CSS selector; defaults to `body`.
- `timeout_seconds` — optional integer from 1–60; defaults to 30.

Example prompt:

> What heading is currently rendered on https://example.com?

Steps: the model selects `browse` for rendered content; Chromium loads the page; fexr returns its final URL, title, status, selected text, visible links, and truncation state.

```json
{"name":"browse","arguments":{"url":"https://example.com","wait_for_selector":"h1","timeout_seconds":30}}
```

Representative output:

```json
{
  "structuredContent":{
    "url":"https://example.com/","title":"Example Domain","status":200,
    "text":"Example Domain","links":[{"text":"More information...","url":"https://www.iana.org/help/example-domains"}],
    "truncated":false
  },
  "content":[{"type":"text","text":"Example Domain"}]
}
```

### `search_site`

Renders a starting page, ranks visible same-host links against a query, and follows the best candidates in one isolated browser context.

Inputs:

- `start_url` — required HTTP/HTTPS starting page.
- `query` — required natural-language search goal.
- `max_pages` — optional integer from 1–20; defaults to 5.
- `timeout_seconds` — optional per-page timeout from 1–60; defaults to 30.

The crawler removes fragments, deduplicates URLs, stays on the starting hostname, and skips common authentication, account, checkout, download, and non-HTML asset paths. Individual page failures are skipped.

Example prompt:

> Explore IANA's example-domain documentation and summarize the related information about reserved domains and protocol registries.

Steps: the model recognizes that the answer spans linked pages; calls `search_site`; fexr renders the example-domain page and follows query-ranked same-host links such as Reserved Domains and Protocols; then the model summarizes the pages with source URLs.

```json
{
  "name":"search_site",
  "arguments":{
    "start_url":"https://www.iana.org/help/example-domains",
    "query":"example domains reserved domains protocol registries",
    "max_pages":5,
    "timeout_seconds":30
  }
}
```

Representative output:

```json
{
  "structuredContent":{
    "query":"example domains reserved domains protocol registries","start_url":"https://www.iana.org/help/example-domains",
    "pages_visited":5,
    "results":[
      {"title":"Example Domains","url":"https://www.iana.org/help/example-domains","status":200,"text":"Example Domains ... Further Reading ..."},
      {"title":"IANA-managed Reserved Domains","url":"https://www.iana.org/domains/reserved","status":200,"text":"Certain domains are set aside ..."},
      {"title":"Protocol Registries","url":"https://www.iana.org/protocols","status":200,"text":"Protocol parameter registries ..."}
    ],
    "truncated":false
  },
  "content":[{"type":"text","text":"{\n  \"query\": \"example domains reserved domains protocol registries\", ...\n}"}]
}
```

## Limits and Errors

Incoming bodies are limited to 4 MiB. Tool text output is limited to approximately 200 KiB and may end with `[output truncated]`. Structured tools also include `structuredContent`.

Malformed protocol requests return JSON-RPC-style codes such as `-32700`, `-32600`, and `-32601`. Tool validation and execution failures use `-32000` with a descriptive message.

## Linux fexr Service Setup Guide

This procedure installs fexr as a hardened systemd service on Ubuntu or another systemd-based Linux distribution.

### 1. Install dependencies

Install the Go version required by `go.mod`, then:

```bash
sudo apt update
sudo apt install -y curl html2text
```

### 2. Create the service account and directories

```bash
sudo useradd --system --home-dir /opt/fexr --shell /usr/sbin/nologin fexr
sudo install -d -o fexr -g fexr /opt/fexr
sudo install -d -o fexr -g fexr /var/cache/fexr
```

Skip `useradd` if the account already exists.

### 3. Build and install fexr

```bash
goreleaser release --snapshot --clean --skip=publish
find dist -type f -name fexr -print
sudo install -o root -g root -m 0755 dist/fexr_linux_amd64_v1/fexr /opt/fexr/fexr
```

Adjust the `dist` path for the host architecture and actual GoReleaser layout.

### 4. Install Playwright in the service cache

The hardened service cannot write under `/opt/fexr` or a normal home cache. Install the exact version from `go.mod` into `/var/cache/fexr`:

```bash
GO_BINARY="$(command -v go)"

sudo env \
  PLAYWRIGHT_DRIVER_PATH=/var/cache/fexr/playwright-driver \
  PLAYWRIGHT_BROWSERS_PATH=/var/cache/fexr/playwright-browsers \
  "$GO_BINARY" run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6201.0 install-deps chromium

sudo env \
  PLAYWRIGHT_DRIVER_PATH=/var/cache/fexr/playwright-driver \
  PLAYWRIGHT_BROWSERS_PATH=/var/cache/fexr/playwright-browsers \
  "$GO_BINARY" run github.com/mxschmitt/playwright-go/cmd/playwright@v0.6201.0 install chromium

sudo chown -R fexr:fexr /var/cache/fexr
```

### 5. Create `/etc/systemd/system/fexr.service`

```ini
[Unit]
Description=fexr MCP Server
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=fexr
Group=fexr
WorkingDirectory=/opt/fexr
ExecStart=/opt/fexr/fexr
Restart=on-failure
RestartSec=10

Environment=PLAYWRIGHT_DRIVER_PATH=/var/cache/fexr/playwright-driver
Environment=PLAYWRIGHT_BROWSERS_PATH=/var/cache/fexr/playwright-browsers
CacheDirectory=fexr

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/tmp /var/cache/fexr

StandardOutput=journal
StandardError=journal
SyslogIdentifier=fexr

[Install]
WantedBy=multi-user.target
```

The application binds all interfaces on port 4002. Use a firewall, private network, or authenticated reverse proxy if untrusted hosts could reach it.

### 6. Enable and start

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now fexr.service
sudo systemctl status fexr.service
```

Expected status resembles:

```text
● fexr.service - fexr MCP Server
     Loaded: loaded (/etc/systemd/system/fexr.service; enabled)
     Active: active (running)
   Main PID: 12345 (fexr)
```

### 7. Verify Playwright and MCP

```bash
fexr_pid="$(systemctl show fexr --property=MainPID --value)"
sudo tr '\0' '\n' < "/proc/$fexr_pid/environ" | grep '^PLAYWRIGHT_'
```

Expected:

```text
PLAYWRIGHT_DRIVER_PATH=/var/cache/fexr/playwright-driver
PLAYWRIGHT_BROWSERS_PATH=/var/cache/fexr/playwright-browsers
```

Verify the driver:

```bash
sudo -u fexr \
  /var/cache/fexr/playwright-driver/node \
  /var/cache/fexr/playwright-driver/package/cli.js \
  --version
```

For Playwright Go `v0.6201.0`, it should report `Version 1.62.1`.

Verify discovery and Chromium:

```bash
curl -sS http://127.0.0.1:4002/mcp \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'

curl -sS http://127.0.0.1:4002/mcp \
  -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"browse","arguments":{"url":"https://example.com"}}}'
```

### Existing-unit override

For an existing service, run `sudo systemctl edit fexr` and include the required section header:

```ini
[Service]
Environment=PLAYWRIGHT_DRIVER_PATH=/var/cache/fexr/playwright-driver
Environment=PLAYWRIGHT_BROWSERS_PATH=/var/cache/fexr/playwright-browsers
CacheDirectory=fexr
ReadWritePaths=/tmp /var/cache/fexr
```

Then apply it:

```bash
sudo systemctl daemon-reload
sudo systemctl restart fexr
sudo systemctl cat fexr
```

### Manage and update the service

```bash
sudo systemctl start fexr
sudo systemctl stop fexr
sudo systemctl restart fexr
sudo systemctl status fexr
sudo journalctl -u fexr.service -f
```

To update, build first, then replace the binary and restart:

```bash
goreleaser release --snapshot --clean --skip=publish
find dist -type f -name fexr -print
sudo systemctl stop fexr
sudo install -o root -g root -m 0755 dist/fexr_linux_amd64_v1/fexr /opt/fexr/fexr
sudo systemctl start fexr
sudo systemctl status fexr.service
```

Run `sudo systemctl daemon-reload` as well if the unit changed.

### Troubleshooting

```bash
sudo journalctl -u fexr.service --no-pager -n 200
```

- **`/opt/fexr/.cache` is read-only:** the Playwright variables did not reach the process. Inspect `systemctl cat fexr`, reload, restart, and inspect `/proc/$fexr_pid/environ`.
- **Playwright/Chromium missing:** reinstall using the exact version in `go.mod`, then run `sudo chown -R fexr:fexr /var/cache/fexr`.
- **Permission denied:** verify `/opt/fexr/fexr` is executable and the cache is accessible to `fexr`.
- **Port conflict:** inspect it with `sudo ss -ltnp 'sport = :4002'`.
- **Static fetch failure:** verify `curl` and `html2text` exist in the service `PATH`.
- **Remote connection failure:** check firewall and routing. fexr has no built-in authentication or TLS.

## Security Notes

- Deploy fexr only for trusted MCP clients; its tools make outbound requests to supplied URLs.
- The server has no built-in authentication or TLS termination.
- The systemd unit runs as a non-root user and restricts writable paths.
- `browse` uses a fresh isolated browser context per call; `search_site` uses one isolated context per bounded crawl.
- `search_site` stays on the starting hostname, but operators should still block private, loopback, and metadata-service destinations at the network layer.
- Do not expose port 4002 directly to an untrusted network.
