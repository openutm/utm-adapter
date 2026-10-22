# UTM Adapter — desktop preview

Go + Wails v3 + Svelte bridge from MAVLink to Flight Blender. Closing the window keeps the adapter running in the tray. The tray menu reopens, pauses, resumes, or quits the app.

This is the first runnable slice of [flight-blender#218](https://github.com/openutm/flight-blender/issues/218), for UX and connection testing before implementing the rest. The Python prototype remains in its separate checkout.

## Try it

Open `bin/utm-adapter.app` after building. The build provided in this checkout is for Apple Silicon, locally ad-hoc signed, and not notarized for distribution.

1. Open **Connection settings**, enter your Flight Blender URL, and choose bearer token or OAuth client credentials. OAuth needs the token endpoint, client ID, secret and scopes (normally `flightblender.read flightblender.write`). **None** works only when your development server permits unauthenticated requests.
2. Open **Ground stations** to choose how to connect. Automatic configuration sets up QGroundControl forwarding while QGC is closed. Mission Planner can use its built-in mirror or the optional OpenUTM plugin; plugin installation requires an explicit choice. Once forwarding is configured, reopen the GCS and connect your vehicle normally.
3. Click **Save & connect**, then **Overview**. Server reachability, authenticated API access, incoming telemetry and accepted deliveries have separate status indicators.
4. For delivery, enter the vehicle's explicit MAVLink system ID, an existing declaration UUID, vehicle serial and operator ID, then enable **telemetry delivery**. The declaration must cover the current time and be in a state that accepts telemetry: normally Activated, Contingent or Non-conforming; non-network mode also permits Accepted. Read access alone does not verify write permission.

Credentials use the OS credential store: macOS Keychain, Windows Credential Manager or Linux Secret Service. Other settings live in `product.json` under the OS config directory's `OpenUTM/utm-adapter` folder. Blank password fields preserve saved credentials. **Remove saved credentials** clears them and pauses connections. Saving credentials requires a usable OS credential store.

### QGroundControl

Automatic setup finds the standard `QGroundControl.ini` settings file and sets `forwardMavlink=true` and `forwardMavlinkHostName=127.0.0.1:14551` in `[General]`. Open QGC once if no settings file exists yet, then close it. The adapter waits until QGC is closed before writing; open QGC afterward. Existing forwarding to another destination is left unchanged, with an explanation in the overview.

To configure manually, disable automatic setup and use **Application Settings → MAVLink**. Enable forwarding, set the destination, and restart QGC.

### Mission Planner

Choose **Ground stations → Install OpenUTM plugin** to opt in. Automatic configuration alone never installs the plugin. The adapter installs `plugins/OpenUTMAdapter.cs` beside a detected Mission Planner executable while Mission Planner is closed. Mission Planner compiles this source plugin on its next launch. It forwards received MAVLink packets to the local adapter and never writes vehicle commands. It follows changes to the primary connection. The saved built-in mirror list cannot start forwarding automatically, which is why a plugin is used. Installation may fail if the Mission Planner directory requires administrator access; the adapter reports that error without elevating. The plugin has been checked against the local Mission Planner source API; runtime validation on Windows remains outstanding.

For manual setup, disable automatic setup, open **Ctrl+F → Mavlink**, choose **UDP Client**, host `127.0.0.1`, port `14551`, and start forwarding with write-to-vehicle disabled. Labels vary between versions.

### Optional QGroundControl custom build

The **Ground stations** page describes the planned OpenUTM QGroundControl build. No installer or downloadable release is available in this preview. A future custom build will use a separate application identity and settings directory alongside the user's original QGC. Existing QGC forwarding remains usable now.

### Backups and automatic setup

Automatic setup only supports local UDP destinations. Direct MAVLink mode does not modify GCS files. Changed files are backed up beside the original as `.utm-adapter-backup-<timestamp>`; unchanged settings are not rewritten. QGC gets a small `.utm-adapter-target` sidecar so the adapter can update its own destination later while preserving someone else’s forwarding.

To undo QGC setup, disable automatic setup in the adapter, close QGC, restore its backup and remove the sidecar. To undo Mission Planner setup, close Mission Planner and choose **Ground stations → Remove OpenUTM plugin**. This disables future installation and moves the plugin to a backup that Mission Planner will not load. Disabling automatic setup alone leaves previously configured forwarding in place.

### Direct MAVLink

Choose **Direct MAVLink**, then UDP listener, TCP client or serial. Examples: UDP `127.0.0.1:14551`, TCP `127.0.0.1:5760`, or serial `/dev/cu.usbmodem…` / `COM3` at your device's baud rate. Configure the vehicle, router or SITL to emit heartbeat and GPS data. The adapter receives passively; it sends no vehicle commands or stream requests. Use forwarding when your GCS already holds the vehicle's serial port.

### Without a vehicle

Keep **telemetry delivery off**, then run:

```sh
go run ./cmd/telemetry-demo
```

This developer sender emits synthetic, disarmed MAVLink 2 telemetry for vehicle 1 over local UDP. Ctrl+C stops it. To exercise QGC forwarding, send to QGC with `--address 127.0.0.1:14550` while QGC forwards to the adapter on 14551.

## Build and develop

Requires Go 1.26+, Node.js 22.12+ or a supported newer Node release, npm, Wails platform prerequisites, and CLI **v3.0.0-beta.34**:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.34
wails3 doctor
wails3 package
```

Packaging installs locked frontend dependencies, generates TypeScript bindings, builds Svelte and creates the desktop package. On macOS the result is `bin/utm-adapter.app`. Use `wails3 task darwin:package:universal` to include both Apple Silicon and Intel. For a Windows or Linux executable on its native OS, run `wails3 build`; the output is `bin/utm-adapter.exe` or `bin/utm-adapter` respectively.

### Download CI builds

The [Desktop binaries workflow](https://github.com/openutm/utm-adapter/actions/workflows/desktop.yml) builds only on tag pushes (including release tags) or manual runs. Branch pushes and pull requests never build binaries automatically. To build a pull request on demand, manually run **Desktop binaries** against its source branch using the Actions UI or `gh workflow run desktop.yml --ref <source-branch>`. GitHub exposes manual workflows through the UI/CLI once the workflow is on the default branch.

The separate [Tests workflow](https://github.com/openutm/utm-adapter/actions/workflows/tests.yml) runs backend tests with the race detector, Go vet, and Svelte/TypeScript checks on pull requests and pushes to `main`. It does not package desktop binaries or upload artifacts.

Linux CI installs only required native packages from Ubuntu's official HTTPS repositories and caches downloaded packages. APT still refreshes package indexes on every run; the cache is refreshed weekly or when the dependency script changes.

Open a successful binary build run and download its **Artifacts** (GitHub sign-in required). Each contains an archive and SHA-256 checksum:

| Artifact | Contents | Runtime requirements |
| --- | --- | --- |
| `utm-adapter-macos-universal` | Ad-hoc signed `.app` for Apple Silicon and Intel | macOS; preview is not notarized |
| `utm-adapter-windows-amd64` | Portable x64 `.exe` | Windows with Microsoft Edge WebView2 Runtime |
| `utm-adapter-linux-amd64` | x64 executable, built on Ubuntu 24.04 | Compatible Linux with GTK4, WebKitGTK 6.0, a desktop session and a tray host |

On Ubuntu 24.04, install build dependencies with `sudo apt-get install build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev libx11-dev`. Linux credential storage also needs a running Secret Service provider. These artifacts are preview builds, not signed release installers. CI verifies compilation and automated tests; real GCS integration and native runtime trials remain separate checks.

```sh
wails3 dev
go test -race ./internal/...
cd frontend && npm run check
```

`UTM_ADAPTER_CONFIG` selects a separate config file. `UTM_ADAPTER_START_HIDDEN=1` starts in the tray. A second launch opens the existing instance. OS login startup is not configured yet.

## Scope and limits

- Receives MAVLink 1/2 using the ArduPilotMega dialect, autopilot component 1, and optional system-ID filtering. UDP expects one MAVLink frame per datagram, as normal GCS forwarding emits; TCP/serial accept streams.
- Shows position, MSL/relative/WGS84 altitude, speed, GPS fix, freshness and packet counts. GCS discovery uses known locations, standard settings files and processes; custom installations may require manual forwarding setup. Automatic setup never closes or restarts a GCS.
- Sends new GPS samples about once per second through `PUT /flight_stream/set_telemetry`. Requires heartbeat/GPS less than five seconds old, a 3D fix, MAVLink 2 ellipsoid altitude, speed and course. MSL is never substituted for WGS84. Unknown ellipsoid altitude, including a zero value, blocks delivery. Operational status is Undeclared because armed state alone does not establish airborne status.
- Checks `/ping` and read access about every ten seconds; uses bearer auth or cached OAuth client-credentials tokens. Network calls time out and connections retry. No offline queue or historical replay.
- Verified automatic setup and forwarding through the installed QGroundControl 5.1.4 on macOS with synthetic MAVLink; also verified direct UDP, TCP integration tests, an HTTP Flight Blender test server, and Flight Blender's actual telemetry dataclass parser. Live authenticated Flight Blender, real autopilots, serial hardware and Mission Planner still need user trials.
- Existing declarations only. Declaration creation, maps/planning, Aerobridge/fleet sync, signing, auto-updates, login startup and release installers are deferred until feedback on this preview.
