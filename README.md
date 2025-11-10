# bindicatwo-api

A small Go service and CLI that fetches North Hertfordshire District Council (NHDC) 
bin collection dates, and exposes the results via:

- A terminal-friendly CLI (bindicatwo query)
- A lightweight HTTP server (bindicatwo serve) that returns JSON

This project pairs with the corresponding firmware client here:
- https://github.com/robberwick/bindicatwo-client (see its README for device setup and OTA expectations)


## Contents
- What it does
- Quick start
- Install
- Configuration (file and environment)
- CLI usage
- HTTP server usage
- Authentication (API keys)
- Rate limiting and caching
- OTA firmware endpoints
- Development


## What it does
Given an address or postcode, bindicatwo resolves your specific address on NHDC's 
website, scrapes upcoming bin collections, and outputs a normalized schedule. The HTTP API is suitable for dashboards, microcontrollers, and home automations.


## Quick start
- CLI one-off query:
  - bindicatwo query --search "SG4 9TY" --prefer "Some Street"  # human-readable output
  - bindicatwo query -s "SG4 9TY" -j                           # JSON output

- Run HTTP server on port 8080:
  - bindicatwo serve
  - curl "http://127.0.0.1:8080/schedule/100080795976" -H "X-API-Key: <key>"  # if keys configured
  - curl "http://127.0.0.1:8080/schedule?uprn=100080795976" -H "X-API-Key: <key>"  # backward compatible query param

- Create a config file and set defaults:
  - bindicatwo config init
  - bindicatwo config set search "SG4 9TY"
  - bindicatwo config set prefer "Some Street"


## Install
Requires Go 1.21+.

- go install github.com/robberwick/bindicatwo-api/cmd/bindicatwo@latest

This provides the bindicatwo binary in your GOPATH/bin or GOBIN.


## Configuration
Configuration is handled by Viper with three sources in priority order:
1. Flags
2. Environment variables
3. Config file

Config file default location and name:
- %USERPROFILE%\.config\bindicatwo\config.yaml (Windows)
- $HOME/.config/bindicatwo/config.yaml (Linux/macOS)

You can also pass a custom file with --config path/to/config.yaml.

Environment variable prefix for generic keys is BINDICATWO_. For example, BINDICATWO_SEARCH sets the default search query for CLI usage. The server subcommand also binds a few un-prefixed env vars for typical hosting environments (see below).

To create and manage the config file:
- bindicatwo config init                     # interactive
- bindicatwo config path                     # show path
- bindicatwo config get                      # show values
- bindicatwo config set <key> <value>        # set a value
- bindicatwo config unset <key>              # delete a key
- bindicatwo config api-keys ...             # manage API keys (see Authentication)

Supported config keys:
- search: default address or postcode
- prefer: choose an address containing this substring when multiple matches exist
- json: default output mode for CLI (true/false)
- firmware_enabled: enable /firmware/* endpoints on the HTTP server
- firmware_version: string served at /firmware/version.txt
- firmware_file: path to a firmware binary served at /firmware/bindicatwo_firmware.bin
- api_key: single API key for HTTP auth (optional)
- api_keys: array of API keys for HTTP auth (optional)


## CLI usage
The CLI is provided by the root bindicatwo command with subcommands.

- bindicatwo query [flags]
  - --search, -s: address or postcode
  - --prefer, -p: prefer address containing this text when multiple matches
  - --json,   -j: output JSON instead of human-readable
  - --config: path to config file (overrides default search path)

Environment variables for CLI defaults (via Viper prefix):
- BINDICATWO_SEARCH, BINDICATWO_PREFER, BINDICATWO_JSON

Examples:
- bindicatwo query -s "SG4 9TY"
- bindicatwo query -s "SG4 9TY" -p "Some Street" -j

JSON shape returned by --json is an array of items with normalized fields designed for machines. Relative fields (e.g., daysUntil, isToday, isTomorrow) are computed by the tool before printing.


## HTTP server usage
Start the server:
- bindicatwo serve [flags]

Default listen address is 127.0.0.1:8080. Change with --addr and --port or environment variables.

Endpoints:
- GET /schedule/{uprn}
  - Path parameter:
    - uprn: the Unique Property Reference Number (e.g., 100080795976)
  - Returns: JSON array of upcoming collections. Relative fields are included.
  - Authentication: optional; see Authentication section.
  - Example: GET /schedule/100080795976

- GET / or /schedule (backward compatible)
  - Query parameters:
    - uprn (also accepts u, id)
  - Returns: JSON array of upcoming collections. Relative fields are included.
  - Authentication: optional; see Authentication section.
  - Example: GET /schedule?uprn=100080795976

- GET /healthz
  - Returns 200 OK with a minimal body.

CORS:
- --cors "*" to allow all origins, or pass a comma-separated list of allowed origins.

Server flags and environment variables:
- --addr (ADDR): listen host (default 127.0.0.1)
- --port (PORT): listen port (default 8080; use 0 for an ephemeral port)
- --cache-ttl (CACHE_TTL): cache lifetime for identical queries (default 15m)
- --cors (CORS_ORIGINS): CORS allowed origins: "*" or CSV
- --rate-rps (RATE_RPS): upstream requests per second (default 1.0)
- --rate-burst (RATE_BURST): burst capacity (default 5)
- --firmware-enable (FIRMWARE_ENABLE): enable OTA endpoints
- --firmware-version (FIRMWARE_VERSION): version string for /firmware/version.txt
- --firmware-file (FIRMWARE_FILE): path to binary served by /firmware/bindicatwo_firmware.bin

Note: The CLI-level env vars use the BINDICATWO_ prefix (e.g., BINDICATWO_SEARCH), but the server-specific hosting vars use the unprefixed names in parentheses above for convenience.


## Authentication (API keys)
If any API key is configured, the schedule endpoints are required to provide a valid API key in order to receive a response.
If none are set, auth is disabled.

How to configure:
- In the config file, either:
  - api_key: "your-key"
  - api_keys: ["key1", "key2", ...]
- Or via environment:
  - API_KEYS="key1,key2" (CSV)
  - API_KEY="your-key"

How to manage keys via CLI:
- bindicatwo config api-keys list
- bindicatwo config api-keys add <key>
- bindicatwo config api-keys remove <key>
- bindicatwo config api-keys generate [--length 32]   # prints generated key

How to authenticate requests:
- Header: X-API-Key: <key>
- Or: Authorization: Bearer <key>
- Or: Query string: ?api_key=<key> (or ?apikey=)


## Rate limiting and caching
- The server uses an in-process token bucket to limit upstream requests (configurable via RATE_RPS and RATE_BURST).
- Successful schedules are cached per (search, prefer) key for --cache-ttl to reduce upstream load. Empty upstream results are intentionally not cached.


## OTA firmware endpoints
These are useful for the companion bindicatwo-client project. Disabled by default.

Enable and configure:
- bindicatwo serve --firmware-enable --firmware-version "1.2.3" --firmware-file ./path/to/bindicatwo_firmware.bin
- Or via env: FIRMWARE_ENABLE=1, FIRMWARE_VERSION=1.2.3, FIRMWARE_FILE=/path/to/bindicatwo_firmware.bin

Endpoints when enabled:
- GET /firmware/version.txt -> plain text of the version string
- GET /firmware/bindicatwo_firmware.bin -> application/octet-stream firmware binary (with Content-Disposition for download)


## Development
- Go 1.21+
- Run locally:
  - go run . query -s "SG4 9TY"
  - go run . serve --port 8080
- Tests:
  - go test ./...


## Related
- Firmware/device client: https://github.com/robberwick/bindicatwo-client


## License
MIT