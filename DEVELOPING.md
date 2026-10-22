# Developing UTM Adapter

This guide covers building, testing, and changing UTM Adapter. For installation and everyday use, see the [user guide](README.md).

The desktop preview implements the initial MAVLink-to-Flight Blender connection work described in [flight-blender#218](https://github.com/openutm/flight-blender/issues/218). It uses Go, Wails v3, and Svelte 5. A separate Python prototype is outside this repository.

## Prerequisites

- Go 1.26 or newer, as declared in [go.mod](go.mod).
- Node.js 24 and npm, matching CI. Use the committed `frontend/package-lock.json`.
- Wails CLI **v3.0.0-beta.34**, matching the Go module and frontend runtime.
- Native desktop build tools and libraries for your target platform. Run `wails3 doctor` to inspect your setup.

Install the pinned CLI and check the host:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.34
wails3 doctor
```

Ensure Go's binary installation directory is on `PATH`.

On Ubuntu 24.04, install native build dependencies:

```sh
sudo apt-get install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev libx11-dev
```

Desktop runs need a graphical session and tray support. Credential persistence requires macOS Keychain, Windows Credential Manager, or Linux Secret Service. Backend tests construct services without accessing the OS credential store and do not require a live Flight Blender server or vehicle.

## Build and run

Run commands from the repository root unless stated otherwise.

```sh
wails3 dev
```

The development configuration in [build/config.yml](build/config.yml) builds the Go application, starts the Vite frontend, and runs the desktop app. Closing its window hides it; use **Quit UTM Adapter** in the tray to exit.

Build a desktop package on the host platform:

```sh
wails3 package
```

The build tasks install locked frontend dependencies, generate TypeScript bindings from the Go services, build the frontend, and embed its output in the application. Do not edit generated files in `frontend/bindings/` or `frontend/dist/`.

| Target | Command | Output |
| --- | --- | --- |
| macOS, host architecture | `wails3 package` | `bin/utm-adapter.app` |
| macOS, Apple Silicon and Intel | `wails3 task darwin:package:universal` | `bin/utm-adapter.app` with a universal executable |
| Windows, native host | `wails3 build` | `bin/utm-adapter.exe` |
| Linux, native host | `wails3 build` | `bin/utm-adapter` |

macOS packaging ad-hoc signs and verifies the bundle. This is not Developer ID signing or notarization. Platform-specific tasks live under `build/darwin/`, `build/windows/`, and `build/linux/`; the root [Taskfile.yml](Taskfile.yml) dispatches to them. Docker and server-mode task scaffolding also exists, but the supported user workflow documented here is the desktop app.

The Go entry point embeds `frontend/dist`, so a raw `go build` from a fresh checkout is insufficient. Use the Wails build tasks to prepare the frontend first.

## Isolate local settings

| Environment variable | Behavior |
| --- | --- |
| `UTM_ADAPTER_CONFIG` | Selects a separate configuration file instead of the OS default. Use an absolute path for a stable credential-store identity. |
| `UTM_ADAPTER_START_HIDDEN=1` | Starts the window hidden in the tray. |
| `WAILS_VITE_PORT` | Overrides the frontend port when using `wails3 task dev`; the task default is `9245`. |

For example, on macOS or Linux:

```sh
UTM_ADAPTER_CONFIG="$PWD/product.json" wails3 dev
```

The root `product.json` is ignored by Git. Configuration uses an atomic temporary-file replacement and excludes tokens and client secrets. Secrets are stored under credential service `org.openutm.adapter`, keyed by the configuration path. Choosing another path also chooses another credential-store entry.

A second application launch opens the existing instance, even when a different config path is supplied. Quit the first instance before switching configurations. The application starts connections on launch using its saved configuration; for local testing, leave delivery disabled and select **Direct MAVLink** to avoid automatic ground station changes.

## Test without a vehicle

1. Start the adapter with the local test configuration.
2. Select **Direct MAVLink**, UDP, listen address `127.0.0.1:14551`, and MAVLink system ID `0` or `1`.
3. Keep **Enable telemetry delivery** off and click **Save & connect**.
4. Start the synthetic sender:

```sh
go run ./cmd/telemetry-demo
```

The sender emits disarmed MAVLink 2 heartbeat, GPS, and global-position messages for system 1, component 1, once per second. Stop it with Ctrl+C. The UI should show position, altitude, speed, and a 3D fix; after stopping the sender, freshness should expire.

To exercise QGroundControl forwarding, configure QGC to forward to the adapter on port 14551, then send the demo to QGC instead:

```sh
go run ./cmd/telemetry-demo --address 127.0.0.1:14550
```

Synthetic data can satisfy the delivery guards. Keep delivery off for UI-only tests; use a dedicated test server and declaration when testing publishing.

## Automated checks

These commands match the checks in the [Tests workflow](.github/workflows/tests.yml):

```sh
go test -race ./internal/...
go vet ./internal/...
npm --prefix frontend ci
npm --prefix frontend run check
```

For a focused test while changing an integration:

```sh
go test ./internal/adapter -run TestUDPToFlightBlender -v
```

The backend suite covers UDP-to-HTTP delivery, direct TCP reception, GPS/altitude guards, credential persistence behavior, authentication failures, redirects, QGC settings preservation, local forwarding targets, and Mission Planner plugin opt-in and removal. Frontend checks validate Svelte and TypeScript; they are not UI interaction tests.

Before changing a native integration, also exercise it on the relevant OS. Compilation and mocked tests do not establish that a ground station or hardware connection works at runtime.

## Code layout and data flow

| Location | Responsibility |
| --- | --- |
| [main.go](main.go) | Wails application, service registration, embedded frontend, single-instance behavior, window and tray lifecycle. |
| [internal/adapter/config.go](internal/adapter/config.go) | Configuration defaults, validation, loading, and secret-free persistence. |
| [internal/adapter/service.go](internal/adapter/service.go) | Connection lifecycle, credential storage, MAVLink consumption, vehicle state, snapshots, periodic checks, and publishing loop. |
| [internal/adapter/blender.go](internal/adapter/blender.go) | Flight Blender HTTP client, bearer/OAuth authentication, access checks, and telemetry payloads. |
| [internal/adapter/discovery.go](internal/adapter/discovery.go) | Ground station process detection and known installation/settings locations. |
| [internal/adapter/gcsconfigure.go](internal/adapter/gcsconfigure.go) | QGC forwarding configuration, backups, and generated Mission Planner C# plugin installation/removal. |
| [frontend/src/App.svelte](frontend/src/App.svelte) | Overview, ground station options, connection settings, and service calls. |
| [cmd/telemetry-demo/main.go](cmd/telemetry-demo/main.go) | Synthetic local UDP sender. |
| `build/` and `.github/workflows/` | Platform build tasks, application assets, CI checks, and binary artifacts. |

The Wails service owns a gomavlib node and a background loop. Incoming frames update vehicle state keyed by MAVLink system ID. The frontend polls `GetSnapshot()` once per second; snapshots contain status and credential-presence flags, never secret values. `ApplyConfig()` validates and saves settings and restarts connections. `Start()` and `Stop()` control connections; `ClearCredentials()` pauses them and disables publishing.

Ground station discovery, setup, server health, and API read checks run roughly every ten loop iterations. Publishing is attempted on a one-second ticker. HTTP calls and discovery can delay that cadence, so these are approximate intervals.

## MAVLink and delivery contract

- The receiver uses the ArduPilotMega dialect and accepts MAVLink 1/2. It ignores GCS heartbeats and only tracks autopilot component 1. System ID `0` disables filtering for monitoring; publishing requires a system ID from `1` to `255`.
- UDP expects one frame per datagram; TCP and serial handle streams. Heartbeat transmission and stream requests are disabled. No vehicle commands are sent.
- `HEARTBEAT` supplies armed state and heartbeat freshness. `GPS_RAW_INT` supplies the delivery position, GPS freshness, fix, speed, course, and MAVLink 2 ellipsoid altitude. `GLOBAL_POSITION_INT` supplies displayed MSL/relative altitude and a position fallback before GPS arrives.
- Delivery requires heartbeat and GPS no more than five seconds old, a new GPS sample after the last successful send, and populated position, WGS84 altitude, speed, and course. GPS fix types 3–8 are accepted. Unknown speed/course sentinels and missing or zero ellipsoid altitude block delivery; MSL is never substituted for WGS84 altitude.
- Delivery sends one observation with one current state for the configured declaration, vehicle serial, and operator. Operational status is `Undeclared`; unknown accuracy and unavailable vertical-speed/pressure-altitude values use the sentinels in `Blender.Publish`. Changing these mappings requires checking the receiving Flight Blender contract.
- A successful send advances the last-sent sample and accepted count. A failed send can be retried while the sample remains fresh. There is no durable queue or historical replay. Vehicle state and counters reset when connections restart.

The HTTP client uses these endpoints relative to the configured base URL:

| Request | Purpose |
| --- | --- |
| `GET /ping` | Unauthenticated reachability check. |
| `GET /flight_declaration_ops/flight_declaration?page_size=1` | Authenticated read access check when no declaration is configured. |
| `GET /flight_declaration_ops/flight_declaration/{UUID}` | Authenticated read access check for the selected declaration. |
| `PUT /flight_stream/set_telemetry` | Telemetry delivery; successful read access alone does not prove write permission. |

Requests have a six-second timeout, reject redirects, and limit response reads to 1 MiB. OAuth uses client credentials with HTTP Basic client authentication and caches access tokens until 30 seconds before expiry. A missing expiry defaults to 300 seconds; HTTP 401 clears the cached token for a subsequent retry.

## Ground station changes

Automatic setup only configures local UDP forwarding, skips direct mode, and waits until the relevant GCS is closed before writing. Discovery uses known paths and process names; custom installations may require manual setup.

QGC configuration updates `forwardMavlink=true` and `forwardMavlinkHostName` under `[General]` in `QGroundControl.ini`. The adjacent `.utm-adapter-target` file records an adapter-managed destination so future updates can preserve unrelated forwarding settings.

Mission Planner plugin installation requires explicit opt-in. The generated `plugins/OpenUTMAdapter.cs` forwards received packets to the adapter and follows the primary connection. Files without the managed-plugin marker are preserved. Removal disables the opt-in and renames the managed plugin to an unloaded backup.

Changed existing files are backed up with `.utm-adapter-backup-<timestamp>`. Disabling automatic configuration does not undo earlier changes. See the [user guide's rollback instructions](README.md#settings-credentials-and-undoing-setup).

## CI and preview distribution

[Tests](.github/workflows/tests.yml) runs on pull requests, pushes to `main`, and manual dispatch. It checks Go tests with the race detector, Go vet, and Svelte/TypeScript on Ubuntu 24.04 and Windows 2025. It does not publish binaries.

[Desktop binaries](.github/workflows/desktop.yml) runs on tag pushes and manual dispatch. Branch pushes and pull requests do not automatically build binaries. To request a build for a branch:

```sh
gh workflow run desktop.yml --ref <source-branch>
```

GitHub exposes manual dispatch once the workflow is present on the default branch. The binary workflow tests the backend, builds the app, checks the frontend, and uploads macOS universal, Windows amd64, and Linux amd64 archives with SHA-256 checksums. Artifacts are retained for 30 days; they are not published release installers.

Linux CI installs native dependencies through [build/ci/install-linux-deps.sh](build/ci/install-linux-deps.sh), using Ubuntu's official HTTPS repositories. Package downloads are cached, indexes refresh each run, and cache keys rotate weekly or when the script changes.

## Validation boundaries

The existing preview notes record successful QGroundControl 5.1.4 automatic setup and forwarding on macOS with synthetic MAVLink, direct UDP, TCP integration tests, an HTTP Flight Blender test server, and checks against Flight Blender's telemetry dataclass parser. These are separate from live operational validation.

Live authenticated Flight Blender delivery, real autopilots, serial hardware, and Mission Planner runtime integration still need trials. The Mission Planner plugin has been checked against a local source API but not validated at runtime on Windows. Record the OS, GCS version, transport, and receiving Flight Blender version when extending that coverage.

Declaration creation, mapping/planning, Aerobridge/fleet synchronization, release signing and installers, auto-updates, login startup, and the custom OpenUTM QGC build remain outside the implemented preview.
