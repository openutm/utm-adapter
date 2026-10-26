# UTM Adapter

UTM Adapter connects your vehicle's MAVLink telemetry to Flight Blender. Use it with QGroundControl, Mission Planner, or a direct vehicle connection to view live telemetry and send position updates for an existing flight declaration.

The adapter runs on your computer and keeps working in the system tray when you close its window. It receives telemetry passively and sends no vehicle commands.

**Version 0.2.0:** records pilot-assigned flight sessions locally, synchronizes their telemetry and audit records, and supplies restrictions to native GCS map integrations. Planning and declaration creation remain in Flight Spotlight / Flight Blender. See the [compatibility matrix](COMPATIBILITY.md) for tested versions and validation boundaries.

## Documentation

| Guide | Use it for |
| --- | --- |
| This page | Installation, first connection, and everyday controls. |
| [Configuration reference](docs/CONFIGURATION.md) | Every option, default, identifier, credential and environment setting. |
| [Operations and recovery](docs/OPERATIONS.md) | Workflow choices, pre/post-flight checks, offline behavior, maps, backups and troubleshooting. |
| [Developer guide](DEVELOPING.md) | Builds, tests, integration verification and release CI. |
| [Architecture](docs/ARCHITECTURE.md) | Component/sequence/data diagrams, concurrency, persistence, APIs and extension rules. |
| [Compatibility and validation](COMPATIBILITY.md) | Supported package targets, tested paths and remaining runtime trials. |

## Choose what to enable

Monitoring, local session recording, live delivery and archive synchronization are separate workflows. Both live-delivery switches default to off. Starting a session records received GPS samples and queues its archive for automatic synchronization while connections run, even if live delivery is off. Legacy delivery sends one aircraft's fresh positions without creating a durable session archive. See the [workflow comparison](docs/OPERATIONS.md#choose-a-workflow) before enabling delivery.

## Get the app

Download tagged builds from [GitHub Releases](https://github.com/openutm/utm-adapter/releases), or a preview build from a successful run of the [Desktop binaries workflow](https://github.com/openutm/utm-adapter/actions/workflows/desktop.yml). Open the run, scroll to **Artifacts**, and select the download for your computer. Downloading requires a GitHub account. Each artifact contains an application archive and a SHA-256 checksum file.

| Your computer | Artifact | How to open it |
| --- | --- | --- |
| macOS, Apple Silicon or Intel | `utm-adapter-macos-universal` | Extract the archive and open `utm-adapter.app`. You can move it to Applications. |
| Windows, x64 | `utm-adapter-windows-amd64` | Extract the archive and run `utm-adapter.exe`. Microsoft Edge WebView2 Runtime is required. |
| Linux, x64 | `utm-adapter-linux-amd64` | Extract the archive, make `utm-adapter` executable if necessary, and launch it from your desktop session. |

Windows releases include an NSIS installer with WebView2 bootstrapping; Linux releases include an AppImage. Builds are not publisher-signed. The macOS app is ad-hoc signed and is not notarized, so macOS may require approval in **System Settings → Privacy & Security** before opening it.

The Linux executable is built on Ubuntu 24.04 and requires compatible GTK4 and WebKitGTK 6.0 libraries, a desktop session, a tray host, and a running Secret Service provider for credential storage. On Ubuntu 24.04, install the runtime libraries with:

```sh
sudo apt-get install libgtk-4-1 libwebkitgtk-6.0-4
```

If no artifact is available, ask your project administrator for a preview build.

## Before you connect

Have the following ready:

- A Flight Blender server URL and either a bearer token or OAuth client credentials, supplied by your service administrator.
- QGroundControl or Mission Planner connected to your vehicle, or a direct MAVLink source.
- To send telemetry: an existing Flight Blender declaration UUID, the vehicle serial number, the operator ID, and the vehicle's MAVLink system ID.

For monitoring only, leave both **Enable telemetry delivery** and **Send session telemetry to Flight Blender** off, and do not start a session. Both switches are off by default.

## Connect for the first time

1. Open UTM Adapter and select **Connection settings**.
2. Enter your **Server URL** and choose **Authentication**. For a bearer token, paste the token. For OAuth, enter the **Token URL**, **Client ID**, **Client secret**, and **Scopes** provided by your administrator. The default scopes are `flightblender.read flightblender.write`. Choose **None** only if your server permits unauthenticated requests.
3. Under **Ground station & telemetry**, choose your **Telemetry source**. For a local ground station, keep **UDP** and the default **Listen address**, `127.0.0.1:14551`.
4. Leave both live-delivery switches off while setting up. Click **Save & connect**.
5. Follow the instructions below for [QGroundControl](#qgroundcontrol), [Mission Planner](#mission-planner), or [Direct MAVLink](#direct-mavlink).
6. Open **Overview**. Confirm that Flight Blender is reachable, API access is verified, and your vehicle appears under **Vehicle telemetry**.

Saving settings starts or restarts the connections. A successful API access check verifies read access; sending telemetry also requires write access.

## Connect your telemetry source

### QGroundControl

Automatic setup is enabled by default and configures forwarding to the adapter on the same computer.

1. Open QGroundControl once so it creates its settings, then close it.
2. In UTM Adapter, select **QGroundControl forwarding** or **Automatic · discover ground station**, with **Set up ground station forwarding automatically** enabled. Click **Save & connect**.
3. Check **Overview** or **Ground stations** for a message that forwarding is configured. Discovery and setup can take several seconds.
4. Reopen QGroundControl and connect your vehicle normally.

The adapter waits until QGroundControl is closed before changing its settings. It preserves an existing forwarding destination set by another application or by you. If setup reports a conflicting destination, configure forwarding manually.

For manual setup, select **Use manual configuration** in **Ground stations**, then open QGroundControl's **Application Settings → MAVLink**. Enable forwarding to `127.0.0.1:14551` (or your adapter's listen address) and restart QGroundControl.

### Mission Planner

You can use Mission Planner's built-in MAVLink mirror or install the optional OpenUTM forwarding plugin.

**Built-in mirror:** in Mission Planner, open **Ctrl+F → Mavlink**, choose **UDP Client**, enter host `127.0.0.1` and port `14551`, and start forwarding with write-to-vehicle disabled. Labels vary between versions. Match the host and port to the adapter's listen address if you changed it. The saved mirror list alone does not start forwarding automatically on the next launch.

**Optional plugin:**

1. In UTM Adapter's **Connection settings**, choose **Mission Planner forwarding** or **Automatic · discover ground station**, and select **UDP**. Click **Save & connect**.
2. Close Mission Planner.
3. In **Ground stations**, choose **Install OpenUTM plugin** and wait for **File installed**.
4. Reopen Mission Planner and connect your vehicle. Confirm incoming telemetry in **Overview**.

The plugin forwards received telemetry automatically when Mission Planner starts and follows changes to all vehicle connections and displays restriction polygons on the Flight Data map. Automatic configuration alone never installs it; you must select the plugin explicitly. The adapter does not request administrator access, so installation can fail if the Mission Planner directory is not writable. Use the built-in mirror in that case.

**File installed** confirms installation on disk. Incoming telemetry confirms forwarding is working. Windows CI verifies packaged DLL loading, initialization and unload. Connected-vehicle forwarding and interactive map trials remain pending.

### Direct MAVLink

Choose **Direct MAVLink** in **Connection settings**, then select a connection type:

| Connection type | What to enter | What the source must do |
| --- | --- | --- |
| UDP | **Listen address**, for example `127.0.0.1:14551` | Send MAVLink telemetry to that address. |
| TCP | **Remote address**, for example `127.0.0.1:5760` | Accept the adapter's connection and stream MAVLink telemetry. |
| Serial | **Device**, for example `/dev/cu.usbmodem…` or `COM3`, and the device's **Baud rate** | Provide telemetry over the serial connection. |

Click **Save & connect**. Direct mode does not change ground station settings. The source must already emit heartbeat and GPS data; the adapter does not request telemetry streams. If a ground station already uses the vehicle's serial port, use its forwarding feature.

**MAVLink system ID** selects a vehicle: `0` displays all received vehicles, while `1`–`255` filters to that system. Each flight session requires an explicit, nonzero ID; use filter `0` to record multiple aircraft.

## Record and synchronize flight sessions

Deploy the companion Flight Blender adapter API and run `alembic upgrade head` on the server first. The session archive requires `/adapter_ops/sessions/{UUID}` and the `a218_session_records` migration. Existing live telemetry APIs remain supported.

1. Connect your telemetry source and select **Flight sessions**.
2. Enter the aircraft's MAVLink system ID, declaration UUID, vehicle serial, operator, and pilot name, licence and contact details. Pilot ID is optional. Select system filter `0` in settings when receiving multiple aircraft.
3. Start the session before flight. Each aircraft has one active session; its assignment stays fixed until the session ends.
4. Enable **Send session telemetry** in settings to send fresh positions for each active session. Archive synchronization runs independently and retains failed deliveries for retry.
5. End the session and select **Export** for a JSON report containing pilot/aircraft details, GPS positions, altitudes, speed, declaration history and start/end audit events. The app displays the export's path.

Sessions and delivery cursors survive restarts. A paused receiver records no new samples. After a crash, the existing session remains active until you end it; receiving telemetry resumes into that assignment. Session records upload in batches of at most 200 samples, with duplicate-safe Flight Blender retries. Historical samples go to the archive API rather than being replayed as live positions. The adapter caches declarations from the configured Flight Blender server, including observed changes to historical declarations. See [operations and recovery](docs/OPERATIONS.md) for offline behavior, synchronization status, multi-aircraft checks and backups.

For Aerobridge, enable the optional registry in settings and provide its URL and bearer token. **Refresh choices** loads aircraft, pilots and operations. Enter the matching operation UUID when starting a session; the adapter attaches the session batch to that operation's flight log. Disabling Aerobridge keeps its pending records locally. Pending deliveries remain associated with their original server URL and resume when that server is selected again.

The Mission Planner plugin shows Flight Blender restriction boundaries directly on its map. For QGC map overlays, install the OpenUTM QGC build from the release's `utm-adapter-gcs-qgroundcontrol-*` downloads. CI also provides a compiled Mission Planner plugin ZIP; follow its README and install only one plugin copy (DLL or generated source). Source-build instructions are in [DEVELOPING.md](DEVELOPING.md). Both use the read-only loopback feed at `http://127.0.0.1:14552/v1/restrictions`, without credentials or pilot details. Stale/unavailable feeds retain the cached boundaries and show a warning. Check published altitude and validity limits; the overlay fills polygon exteriors, including any holes.

## Send telemetry to Flight Blender

1. Confirm your vehicle's live values in **Overview**.
2. In **Connection settings**, set **MAVLink system ID** to the vehicle you want to send.
3. Under **Send telemetry to Flight Blender**, enter the **Flight declaration ID**, **Vehicle serial number**, and **Operator ID**.
4. Switch on **Enable telemetry delivery** and click **Save & connect**.
5. Return to **Overview** and check **Flight Blender telemetry delivery**. Its accepted count increases when Flight Blender accepts updates.

The declaration must cover the current time and be in a state that accepts telemetry: normally Activated, Contingent, or Non-conforming; non-network mode also permits Accepted. Ask your Flight Blender administrator if the declaration is rejected.

Delivery requires a vehicle heartbeat and GPS sample no more than five seconds old, a 3D GPS fix, WGS84 ellipsoid altitude from MAVLink 2, speed, and course. An MSL altitude displayed in the app does not satisfy the WGS84 altitude requirement. Missing ellipsoid altitude, including a reported zero value, blocks delivery.

This legacy single-declaration mode sends fresh GPS samples about once per second. Use Flight sessions for durable recording and archive synchronization; legacy mode alone has no offline queue. Switch off **Enable telemetry delivery** and save to stop sending while continuing local monitoring.

## Understand the overview

| Indicator | What it tells you |
| --- | --- |
| Ground station | Whether a supported ground station was found or is running, and whether forwarding needs attention. |
| Flight Blender | Server reachability and authenticated API read access. A reachable server does not by itself confirm valid credentials. |
| Telemetry stream | Whether recent MAVLink frames are arriving and parsing. This does not by itself confirm GPS data is ready for delivery. |
| Vehicle telemetry | Position, MSL and WGS84 altitude, relative altitude, speed, GPS fix, and sample age. Missing values appear as `—`. |
| Flight Blender telemetry delivery | Why delivery is waiting or failing, or confirmation that telemetry was accepted. The accepted count resets when connections restart. |

Expand **Recent activity** for connection and setup messages and the location of your configuration file.

## Run in the background

Closing the window keeps the adapter running in the system tray. Use the tray menu to reopen it, **Pause connections**, **Resume connections**, or **Quit UTM Adapter**. You can also pause and resume from **Overview**.

Launching the app again opens the existing instance. The app does not configure automatic startup at login.

## Settings, credentials, and undoing setup

Tokens and client secrets are stored in macOS Keychain, Windows Credential Manager, or Linux Secret Service. Leave a password field blank to keep its saved value. **Remove saved credentials** deletes all saved secrets, disables legacy telemetry delivery and Aerobridge, and pauses connections. The session-delivery setting remains unchanged; review it before resuming. See the [credential reference](docs/CONFIGURATION.md#credential-presence-fields).

Other settings are saved in `OpenUTM/utm-adapter/product.json` under your OS configuration directory. Typical locations are:

| Platform | Configuration file |
| --- | --- |
| macOS | `~/Library/Application Support/OpenUTM/utm-adapter/product.json` |
| Windows | `%AppData%\OpenUTM\utm-adapter\product.json` |
| Linux | `$XDG_CONFIG_HOME/OpenUTM/utm-adapter/product.json`, or `~/.config/OpenUTM/utm-adapter/product.json` when unset |

Use the path shown in **Recent activity** to locate your actual file. `records.sqlite` beside it contains flight sessions, cached declarations and delivery queues. Reports are written in the adjacent `exports/` directory. These contain personal and flight data; protect them and back them up with your configuration.

Automatic setup backs up existing files before changing them, beside the original with the suffix `.utm-adapter-backup-<timestamp>`. It does not rewrite unchanged settings. Turning automatic setup off leaves existing forwarding and installed plugins in place.

- **Undo QGroundControl setup:** disable automatic configuration in the adapter, close QGroundControl, restore its settings backup, and remove the adjacent `.utm-adapter-target` file. Reopen QGroundControl. If no backup is available, disable forwarding manually in QGroundControl.
- **Remove the Mission Planner plugin:** close Mission Planner and choose **Ground stations → Remove OpenUTM plugin**. This disables future installation and moves the managed source plugin to a backup that Mission Planner will not load. A compiled release DLL must be removed manually; see its [installation guide](integrations/mission-planner/README.md).

## Troubleshooting

| Problem | What to check |
| --- | --- |
| Flight Blender is unreachable | Confirm the server URL, server availability, and network connection. Enter the server base URL without embedded credentials or query parameters. |
| Server reachable, but API access fails | Check the selected authentication mode, token or OAuth credentials, and `flightblender.read` permission. HTTP 401 indicates a credential problem; HTTP 403 indicates a permission problem. |
| API access works, but delivery fails | Check write permission, declaration UUID, current declaration time window and state, vehicle serial, and operator ID. HTTP 400 may indicate invalid declaration or vehicle details. |
| No live vehicle | Check the forwarding destination against the adapter's listen address, confirm the vehicle is connected, and check the system ID filter. For automatic setup, close the GCS, wait for setup, then reopen it. |
| Telemetry arrives, but delivery waits for GPS | Confirm a recent heartbeat, 3D fix, MAVLink 2 WGS84 altitude, speed, and course. MSL altitude alone is insufficient. |
| Listen address is already in use | Stop the other listener or choose a different UDP port and update forwarding to match. |
| Ground station is not found | Open it once, then close it. Custom installation paths may need manual forwarding. |
| Cannot save credentials | Unlock or enable your OS credential store. On Linux, check that a Secret Service provider is running in your desktop session. |
| Plugin installation fails | Check that Mission Planner is closed and its installation directory is writable, or use the built-in mirror. |

If you need help, [open an issue](https://github.com/openutm/utm-adapter/issues) with your operating system, telemetry source, relevant status message, and steps to reproduce. Remove credentials and sensitive flight details before sharing screenshots or configuration.

## Current limitations

- Existing flight declarations only; declaration creation and flight planning remain in external tools. Native GCS maps show restriction boundaries through the supplied integrations.
- MAVLink 1 and 2 are supported for monitoring, using autopilot component 1. Delivery requires MAVLink 2 ellipsoid altitude. UDP input expects one MAVLink frame per datagram.
- Armed state is displayed locally; delivered operational status is **Undeclared**, because armed state alone does not establish whether a vehicle is airborne.
- No automatic updates, login startup, or publisher signing. QGC packages and the Mission Planner plugin are built alongside adapter releases; see [COMPATIBILITY.md](COMPATIBILITY.md) for platforms and verification coverage.
- Physical autopilots, serial hardware, production authentication/deployments and interactive Mission Planner vehicle/map use still need trials. Local tests exercise the actual Flight Blender and Aerobridge applications; virtual serial and SITL/parser tests do not establish hardware compatibility. See [COMPATIBILITY.md](COMPATIBILITY.md).

For source builds, development setup, architecture, and testing, see the separate [developer guide](DEVELOPING.md).

Licensed under [Apache 2.0](LICENSE).

See the [shared Go/DJI session archive contract](docs/SESSION_ARCHIVE.md) for schema version 2, completion receipts, compatibility and the cross-client integration harness.
