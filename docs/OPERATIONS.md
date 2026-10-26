# Operating UTM Adapter

Start with the [installation and connection guide](../README.md). The [configuration reference](CONFIGURATION.md) explains every option. This guide covers choosing a workflow, checking results, and recovering records.

## Choose a workflow

| Goal | Required actions | Result |
| --- | --- | --- |
| Monitor telemetry | Connect a source; leave both delivery switches off; do not start a session. | Live display; no session archive. Server checks and restriction refreshes still run. |
| Record a flight | Start a session with an immutable assignment. | Durable GPS records and start/end audit events. Archive synchronization runs automatically while connections run. |
| Send live positions for sessions | Enable **Send session telemetry to Flight Blender** and start assigned sessions. | Fresh positions go to the live API; recorded history independently goes to the archive API. |
| Send live positions without a session | Enable legacy **Enable telemetry delivery**, with explicit system/declaration/vehicle/operator IDs. | Single-aircraft live updates; no historical queue or session report. |
| Attach records to Aerobridge | Enable Aerobridge before starting the session; assign an operation UUID. | Session batches also become operation flight logs. |
| Display restrictions in a GCS | Run the adapter bridge and install the custom QGC or Mission Planner integration. | Cached Flight Blender restriction boundaries appear on the native map. Stock QGC forwarding alone does not add overlays. |

An active session suppresses legacy live delivery for its system. Turning off session live delivery does not stop recording or archive uploads. Pausing connections stops reception, periodic synchronization, and the map listener; **Synchronize now** remains an explicit way to attempt uploads while paused.

## Before recording

1. Obtain an existing declaration and the server permissions for the intended workflow. For archives, have the administrator deploy the companion Flight Blender session API and apply `alembic upgrade head`, including `a218_session_records`.
2. Verify **Overview** shows incoming vehicle telemetry and the intended MAVLink system IDs. Each aircraft must have a distinct system ID; the adapter cannot distinguish aircraft sharing one ID.
3. Use system filter `0` for concurrent aircraft. Confirm the correct declaration, aircraft identifier, operator and pilot details for each assignment. If Aerobridge is enabled, select each operation UUID before starting.
4. Check writable local storage and available disk space. Recording and queues have no automatic retention or size limit.
5. Start a session for each aircraft before the flight. Verify its sample count increases. Starting a session does not require a currently connected aircraft, so a successful start alone does not prove recording is receiving data.
6. If live delivery is required, enable the appropriate switch and check accepted updates. A recent heartbeat and 3D GPS fix, speed, course, and nonzero MAVLink 2 ellipsoid altitude are required. A live vehicle card or MSL altitude alone is insufficient.

The adapter records received `GPS_RAW_INT` messages during active sessions, including samples with missing fix/position values. Timestamps reflect reception on the adapter computer, so keep its clock correct. It does not infer takeoff, landing, or flight completion from armed state.

## After recording

1. End each session explicitly. Pausing, closing the window, quitting, and restarting do not end sessions.
2. Use **Synchronize now** and inspect the session's Flight Blender and Aerobridge statuses, along with Records/Aerobridge status messages. The success toast means the attempt completed; individual services can still be unavailable or queues pending.
3. Retry until the required destinations show synchronized. A live accepted count confirms live API acceptance only; it does not confirm archive synchronization.
4. Select **Export** for an ended session. The app reports a JSON file path. Reports contain assignment, GPS samples, observed declaration history, and session audit events. Protect pilot details and positions when sharing them.
5. Back up the data directory after quitting. Exports are independent snapshots; a later export may include additional observed declaration history or updated delivery metadata.

The UI shows the latest 100 sessions. Older records remain in SQLite and are still considered by synchronization, but there is no full-history browser or bulk export UI. Export reports while sessions are accessible and retain protected backups.

## Outages, restarts, and server changes

Recording continues during server outages while telemetry and local storage remain available. Exact pending archive batches and separate destination cursors are persisted before upload. Sessions survive a restart; reconnecting resumes recording into active assignments. Samples missed while the app is stopped, paused, or disconnected cannot be reconstructed.

Synchronization runs in background passes, with up to 200 samples per session per destination in each pass. Each pass is followed by a ten-second wait; HTTP requests, backlog, and time budgets can extend catch-up time. Large queues may require many passes. Ending a session also produces metadata that must synchronize.

Sessions retain their original Flight Blender URL and, if enabled when created, Aerobridge URL. Changing servers does not reroute existing records. Select the original URL again to resume those deliveries, and use the original Flight Blender service identity. Enabling Aerobridge later does not attach earlier sessions to it; disabling it retains pending work. Avoid running cloned datasets concurrently: Aerobridge reconciliation assumes one adapter owns a session.

If storage fails, inspect the Records status immediately and resolve disk space or permissions before relying on further recording. Keep the database intact when reporting a problem. Do not clear pending-delivery rows to force a retry; doing so removes the persisted retry contract.

## Native map integrations

QGroundControl users need the OpenUTM custom QGC package for overlays. Install it in place of stock QGC: it uses the same application/settings name. Mission Planner users can install the compiled release plugin ZIP or the adapter-generated source plugin. Follow the [plugin installation guide](../integrations/mission-planner/README.md); install one copy only. The generated source supports custom forwarding and map addresses, while the release DLL uses defaults.

With connections running, these URLs help diagnose the bridge:

```sh
curl --fail http://127.0.0.1:14552/v1/health
curl --fail http://127.0.0.1:14552/v1/restrictions
```

Health confirms the local listener, not the upstream server or telemetry. Restrictions return a GeoJSON FeatureCollection with `fetchedAt` and `stale`. An empty, stale collection can mean no cache has been obtained. Data older than two minutes is stale; failed refreshes retain the last cache. A paused adapter does not serve these URLs, and the GCS integration retains its own last displayed boundaries with a warning.

Overlays fill Polygon/MultiPolygon exteriors, including holes. They do not enforce altitude, validity windows, airspace compliance, or authorization. Read the published restriction details separately. The unauthenticated bridge binds only to loopback and exposes restrictions, not tokens, pilot details, or flight samples; other local processes can read it.

## Backup, restore, upgrade, and removal

Quit the adapter before copying its data directory so SQLite has no active writer. Copy `product.json`, `records.sqlite`, and `exports/` together to protected storage. Back up managed GCS settings separately if needed. Credentials are in the OS credential store and are not included in this copy. Local records are not encrypted or cryptographically tamper-proof.

To restore, quit the adapter, preserve the current directory as a backup, and restore the saved files to the intended configuration directory. Restore normal owner-only access permissions. Start with the same configuration path where possible; re-enter credentials if needed. Verify active sessions and queues before connecting aircraft. Do not combine databases by copying individual rows or run the restored copy alongside the original.

To upgrade, end flights, synchronize, quit, back up, then replace the application using the package for your OS. Check the [compatibility matrix](../COMPATIBILITY.md) and server requirements before starting it. A database with a newer schema version is rejected; retain backups for rollback rather than opening an upgraded database with an older binary. The app does not install updates automatically.

To remove the adapter, first undo managed GCS forwarding/plugins using the [rollback instructions](../README.md#settings-credentials-and-undoing-setup), remove saved credentials if appropriate, quit, and uninstall/delete the app. The in-app plugin removal handles only the managed source file; manually remove a release DLL. Retain or securely remove the separate data directory according to your record-retention needs. Uninstalling the executable does not itself erase flight records.

## Diagnose a problem

| Symptom | Interpretation and next action |
| --- | --- |
| Parse errors but valid telemetry continues | Read the latest diagnostic. Invalid magic often means console text on a MAVLink feed; skipped bytes can mean leftover text or multiple frames in a UDP datagram; checksum errors mean a rejected frame. Use a clean MAVLink output with one frame per UDP datagram. For SITL, use the [MAVProxy procedure](../DEVELOPING.md#test-without-a-vehicle). Reconnect resets diagnostics. |
| Frame count increases but no aircraft appears | Frames may belong to GCS/component IDs that are ignored or a filtered system. Check component 1 and the system filter. |
| Aircraft looks live but cannot deliver | Vehicle freshness and GPS readiness are separate. Check heartbeat/GPS ages, fix, ellipsoid altitude, speed/course, and the selected assignment. |
| Live delivery stopped after starting a session | Active sessions take precedence over legacy mode. Enable session delivery or end the session as appropriate. |
| Archive returns 404 | Check the base URL and companion server API deployment/migration. Existing live telemetry support does not imply archive support. |
| Archive returns 401/403 or disappears for new credentials | Check scopes and the original issuer/subject identity. Read-access success does not prove write access. |
| Synchronize completes but session remains queued | Inspect destination errors, original URL binding, enabled Aerobridge, and backlog; repeat after correcting the cause. |
| Map is empty/stale | Check connections running, bridge address, cached restrictions, server access, and whether the custom GCS integration is installed. Check both bridge URLs above. |
| Cannot open database after downgrade | Restore the matching pre-upgrade backup or use the matching application version. Do not change SQLite's schema-version field manually. |

For other connection/install issues, see [README troubleshooting](../README.md#troubleshooting). When filing an issue, include OS, app/GCS versions, source/transport, relevant statuses, and reproduction steps. Redact tokens, contact details, positions, registry data, and URLs containing private infrastructure names. Do not attach the full database by default.
