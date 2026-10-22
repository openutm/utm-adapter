# UTM Adapter

UTM Adapter connects your vehicle's MAVLink telemetry to Flight Blender. Use it with QGroundControl, Mission Planner, or a direct vehicle connection to view live telemetry and send position updates for an existing flight declaration.

The adapter runs on your computer and keeps working in the system tray when you close its window. It receives telemetry passively and sends no vehicle commands.

**Early access:** this preview supports telemetry monitoring and delivery. It does not create flight declarations or provide flight planning. See [current limitations](#current-limitations) before use.

## Get the app

Download a build from a successful run of the [Desktop binaries workflow](https://github.com/openutm/utm-adapter/actions/workflows/desktop.yml). Open the run, scroll to **Artifacts**, and select the download for your computer. Downloading requires a GitHub account. Each artifact contains an application archive and a SHA-256 checksum file.

| Your computer | Artifact | How to open it |
| --- | --- | --- |
| macOS, Apple Silicon or Intel | `utm-adapter-macos-universal` | Extract the archive and open `utm-adapter.app`. You can move it to Applications. |
| Windows, x64 | `utm-adapter-windows-amd64` | Extract the archive and run `utm-adapter.exe`. Microsoft Edge WebView2 Runtime is required. |
| Linux, x64 | `utm-adapter-linux-amd64` | Extract the archive, make `utm-adapter` executable if necessary, and launch it from your desktop session. |

These are preview builds, not signed release installers. The macOS app is ad-hoc signed and is not notarized, so macOS may require approval in **System Settings → Privacy & Security** before opening it.

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

You can monitor incoming telemetry with **Enable telemetry delivery** switched off. Delivery is off by default.

## Connect for the first time

1. Open UTM Adapter and select **Connection settings**.
2. Enter your **Server URL** and choose **Authentication**. For a bearer token, paste the token. For OAuth, enter the **Token URL**, **Client ID**, **Client secret**, and **Scopes** provided by your administrator. The default scopes are `flightblender.read flightblender.write`. Choose **None** only if your server permits unauthenticated requests.
3. Under **Ground station & telemetry**, choose your **Telemetry source**. For a local ground station, keep **UDP** and the default **Listen address**, `127.0.0.1:14551`.
4. Leave telemetry delivery off while setting up. Click **Save & connect**.
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

The plugin forwards received telemetry automatically when Mission Planner starts and follows changes to its primary vehicle connection. Automatic configuration alone never installs it; you must select the plugin explicitly. The adapter does not request administrator access, so installation can fail if the Mission Planner directory is not writable. Use the built-in mirror in that case.

**File installed** confirms installation on disk. Incoming telemetry confirms forwarding is working. Windows runtime validation of the plugin remains pending in this preview.

### Direct MAVLink

Choose **Direct MAVLink** in **Connection settings**, then select a connection type:

| Connection type | What to enter | What the source must do |
| --- | --- | --- |
| UDP | **Listen address**, for example `127.0.0.1:14551` | Send MAVLink telemetry to that address. |
| TCP | **Remote address**, for example `127.0.0.1:5760` | Accept the adapter's connection and stream MAVLink telemetry. |
| Serial | **Device**, for example `/dev/cu.usbmodem…` or `COM3`, and the device's **Baud rate** | Provide telemetry over the serial connection. |

Click **Save & connect**. Direct mode does not change ground station settings. The source must already emit heartbeat and GPS data; the adapter does not request telemetry streams. If a ground station already uses the vehicle's serial port, use its forwarding feature.

**MAVLink system ID** selects a vehicle: `0` displays all received vehicles, while `1`–`255` filters to that system. Sending to Flight Blender requires an explicit, nonzero ID.

## Send telemetry to Flight Blender

1. Confirm your vehicle's live values in **Overview**.
2. In **Connection settings**, set **MAVLink system ID** to the vehicle you want to send.
3. Under **Send telemetry to Flight Blender**, enter the **Flight declaration ID**, **Vehicle serial number**, and **Operator ID**.
4. Switch on **Enable telemetry delivery** and click **Save & connect**.
5. Return to **Overview** and check **Flight Blender telemetry delivery**. Its accepted count increases when Flight Blender accepts updates.

The declaration must cover the current time and be in a state that accepts telemetry: normally Activated, Contingent, or Non-conforming; non-network mode also permits Accepted. Ask your Flight Blender administrator if the declaration is rejected.

Delivery requires a vehicle heartbeat and GPS sample no more than five seconds old, a 3D GPS fix, WGS84 ellipsoid altitude from MAVLink 2, speed, and course. An MSL altitude displayed in the app does not satisfy the WGS84 altitude requirement. Missing ellipsoid altitude, including a reported zero value, blocks delivery.

The adapter sends new GPS samples about once per second. It has no offline queue or historical replay, so interrupted delivery does not backfill missed samples. Switch off **Enable telemetry delivery** and save to stop sending while continuing local monitoring.

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

Tokens and client secrets are stored in macOS Keychain, Windows Credential Manager, or Linux Secret Service. Leave a password field blank to keep its saved value. **Remove saved credentials** deletes saved credentials, disables telemetry delivery, and pauses connections.

Other settings are saved in `OpenUTM/utm-adapter/product.json` under your OS configuration directory. Typical locations are:

| Platform | Configuration file |
| --- | --- |
| macOS | `~/Library/Application Support/OpenUTM/utm-adapter/product.json` |
| Windows | `%AppData%\OpenUTM\utm-adapter\product.json` |
| Linux | `$XDG_CONFIG_HOME/OpenUTM/utm-adapter/product.json`, or `~/.config/OpenUTM/utm-adapter/product.json` when unset |

Use the path shown in **Recent activity** to locate your actual file.

Automatic setup backs up existing files before changing them, beside the original with the suffix `.utm-adapter-backup-<timestamp>`. It does not rewrite unchanged settings. Turning automatic setup off leaves existing forwarding and installed plugins in place.

- **Undo QGroundControl setup:** disable automatic configuration in the adapter, close QGroundControl, restore its settings backup, and remove the adjacent `.utm-adapter-target` file. Reopen QGroundControl. If no backup is available, disable forwarding manually in QGroundControl.
- **Remove the Mission Planner plugin:** close Mission Planner and choose **Ground stations → Remove OpenUTM plugin**. This disables future installation and moves the plugin to a backup that Mission Planner will not load.

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

- Existing flight declarations only; no declaration creation, maps, flight planning, or fleet synchronization.
- MAVLink 1 and 2 are supported for monitoring, using autopilot component 1. Delivery requires MAVLink 2 ellipsoid altitude. UDP input expects one MAVLink frame per datagram.
- Armed state is displayed locally; delivered operational status is **Undeclared**, because armed state alone does not establish whether a vehicle is airborne.
- No automatic updates, login startup, or signed release installers. The optional OpenUTM custom QGroundControl build is planned and has no downloadable release in this preview.
- Real autopilots, serial hardware, live authenticated Flight Blender connections, and Mission Planner still need user trials. QGroundControl forwarding has been exercised on macOS with synthetic telemetry.

For source builds, development setup, architecture, and testing, see the separate [developer guide](DEVELOPING.md).

Licensed under [Apache 2.0](LICENSE).
