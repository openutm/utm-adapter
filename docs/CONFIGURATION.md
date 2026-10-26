# Configuration reference

Use **Connection settings → Save & connect** to apply changes. Saving restarts connections and resets live counters, but preserves sessions and queued archives. See the [user guide](../README.md) for installation and the [operations guide](OPERATIONS.md) for workflows and recovery.

## Connection settings

The JSON keys below correspond to `product.json`. Omitted fields inherit the defaults in [config.go](../internal/adapter/config.go). Prefer the UI; quit the application before editing this file manually. Never put credentials in it: credentials read from JSON are discarded.

### Flight Blender and authentication

| UI option | JSON key | Default | Meaning |
| --- | --- | --- | --- |
| Server URL | `baseURL` | `http://127.0.0.1:8000` | Flight Blender base URL, optionally with a path prefix. |
| Authentication | `authMode` | `bearer` | `bearer`, `oauth` (client credentials), or `none`. Use the mode your server accepts. |
| Bearer token | `token` | Empty | Secret supplied through the UI and stored in the OS credential store. |
| Token URL | `tokenURL` | Empty | OAuth token endpoint; required in OAuth mode. |
| Client ID | `clientID` | Empty | Required in OAuth mode. |
| Client secret | `clientSecret` | Empty | OAuth secret supplied through the UI and stored in the OS credential store. |
| Scopes | `scope` | `flightblender.read flightblender.write` | Requested OAuth scopes. A bearer token must already carry the required permissions. |

HTTP(S) URLs must have a host, with no embedded username/password, query, or fragment. Redirects are rejected. Use the server's final URL. HTTPS uses the operating system's trust configuration; there is no custom CA or client-certificate setting in the UI. HTTP is available for local test servers; use HTTPS for remote credentials and flight data.

The connection check verifies reachability and read access. Live delivery and archive uploads need write access too. Session archives require the companion Flight Blender API and `a218_session_records` migration. Archive ownership is based on token issuer and subject: renew credentials using the same service identity to continue accessing that identity's records.

### Sessions, Aerobridge, and maps

| UI option | JSON key | Default | Meaning |
| --- | --- | --- | --- |
| Send session telemetry to Flight Blender | `sessionDelivery` | `false` | Sends fresh positions for active sessions. Does not control local recording or durable archive synchronization. |
| Local GCS map address | `bridgeAddress` | `127.0.0.1:14552` | Local HTTP listener for restrictions. Empty disables it. Must use a loopback IP literal and port, such as `127.0.0.1:14552` or `[::1]:14552`; `localhost` is not accepted. |
| Connect Aerobridge | `aerobridgeEnabled` | `false` | Enables registry choices and flight-log synchronization. |
| Aerobridge URL | `aerobridgeURL` | `http://127.0.0.1:8080` | Required when enabled; same URL rules as Flight Blender. |
| Aerobridge bearer token | `aerobridgeToken` | Empty | Separate credential stored in the OS credential store. The adapter requires a nonempty token when making registry/log requests, including against a development server without authentication enforcement. |

Archive synchronization is automatic while connections run, even when both live-delivery switches are off. **Flight sessions → Synchronize now** can attempt synchronization while connections are paused. There is no separate archive-off setting. To monitor without creating flight records, do not start a session. To record during an outage, leave the server configuration intact; uploads retry when connectivity returns.

The map listener runs only while connections run. Release GCS integrations use the default IPv4 map address. Changing it requires updating the QGC QML endpoint or regenerating the Mission Planner source plugin; the compiled release DLL has fixed default endpoints.

### Ground station and telemetry

| UI option | JSON key | Default | Meaning |
| --- | --- | --- | --- |
| Telemetry source | `source` | `auto` | `auto`, `qgroundcontrol`, `mission-planner`, or `direct`. Controls discovery/setup policy; the transport below determines reception. |
| Set up ground station forwarding automatically | `autoConfigureGCS` | `true` | Allows managed local UDP setup while the GCS is closed. Direct mode skips setup. Does not install the Mission Planner plugin without opt-in. |
| Install/remove OpenUTM plugin in Ground stations | `missionPlannerPlugin` | `false` | Persists explicit opt-in to the managed Mission Planner source plugin. |
| Connection type | `transport` | `udp` | `udp` listener, `tcp` client, or `serial` connection. |
| Listen address / Remote address | `address` | `127.0.0.1:14551` | UDP bind address or TCP server address, in `host:port` form. Ignored for serial. |
| Device | `serialDevice` | Empty | Serial device path or Windows COM port; required for serial. |
| Baud rate | `baud` | `57600` | Match the device; minimum accepted value is 1200. |
| MAVLink system ID | `systemID` | `0` | `0` tracks all aircraft; `1`–`255` filters to one aircraft. Legacy delivery requires a nonzero filter. Sessions require explicit IDs matching this filter, or filter `0`. |

The default UDP listener accepts traffic only from the local computer. Listening on an interface address exposes reception to that network; incoming MAVLink does not have adapter-level sender authentication. Use a trusted telemetry network. Automatic GCS setup accepts only local UDP destinations. TCP and serial sources must already stream telemetry: the adapter sends no heartbeat, stream requests, or vehicle commands.

### Legacy single-declaration live delivery

| UI option | JSON key | Default | Meaning |
| --- | --- | --- | --- |
| Enable telemetry delivery | `publish` | `false` | Enables live delivery for the configured system and declaration. No durable queue. |
| Flight declaration ID | `declarationID` | Empty | Existing declaration UUID. Any nonempty value must have UUID format, even with delivery off. |
| Vehicle serial number | `vehicleID` | Empty | Vehicle identifier expected by Flight Blender's live telemetry API. |
| Operator ID | `operatorID` | `utm-adapter` | Operator identifier expected by the receiving service. |

Enabling this mode requires all identifiers and a nonzero `systemID`. An active session for that aircraft takes precedence: it suppresses legacy delivery even if session live delivery is off. End that session before returning to legacy delivery. Use sessions for concurrent aircraft and durable archives.

### Credential presence fields

`hasToken`, `hasClientSecret`, and `hasAerobridgeToken` are snapshot indicators for the UI, not configuration controls. The backend redacts secrets from snapshots and clears these flags when saving configuration. Leaving a password field blank preserves its saved secret. **Remove saved credentials** deletes all three secrets, pauses connections, and disables legacy delivery and Aerobridge. It leaves the session-delivery setting intact; review it before resuming, especially with authentication set to None.

## Session assignment

Assignments are fixed once recording starts. End the session and create another to change them. Starting a session validates its fields locally; it does not prove the declaration, aircraft, pilot, or operation exists on a server.

| Field | Required | Meaning |
| --- | --- | --- |
| MAVLink system ID | Yes | Aircraft ID `1`–`255`; unique among active sessions and distinguishable in the incoming telemetry. |
| Declaration UUID | Yes | Existing Flight Blender declaration. |
| Aircraft ID | Yes | Assignment's vehicle identifier, also used as the serial number in session live delivery. Use the value expected by Flight Blender; up to 512 characters. |
| Operator ID | Yes | Operator responsible for this flight; up to 512 characters. |
| Pilot ID | No | Optional registry/reference identifier, up to 128 characters. |
| Pilot name | Yes | Pilot's name, up to 512 characters. |
| Pilot licence | Yes | Licence/reference, up to 512 characters. |
| Pilot contact | Yes | Contact details, up to 512 characters. |
| Aerobridge operation UUID | When Aerobridge is enabled | Operation to receive the flight logs. Optional otherwise; must be a UUID when supplied. |

**Refresh choices** offers cached declarations and, when enabled, Aerobridge aircraft, pilots, and operations. Suggestions do not automatically fill or validate all assignment metadata. A registry UUID and a vehicle serial are not necessarily interchangeable; confirm the identifier accepted by your Flight Blender deployment.

## Files and environment

The default directory is `OpenUTM/utm-adapter` under the OS user configuration directory. See [platform paths](../README.md#settings-credentials-and-undoing-setup).

| Location | Contents |
| --- | --- |
| `product.json` | Non-secret connection settings. |
| `records.sqlite` in the same directory | Sessions, GPS samples, delivery cursors, pending payloads, declaration/restriction cache, and audit events. |
| `exports/` in the same directory | JSON reports created explicitly for ended sessions. |
| OS credential store | Flight Blender token, OAuth client secret, Aerobridge token. |

| Environment variable | Meaning |
| --- | --- |
| `UTM_ADAPTER_CONFIG` | Overrides the configuration filename. Use an absolute path. The directory also determines the database and export location. Different filenames in one directory still share one database; use separate directories for isolated datasets. |
| `UTM_ADAPTER_START_HIDDEN=1` | Opens in the tray with the window hidden. Does not install startup-at-login. |
| `WAILS_VITE_PORT` | Development frontend port; task default `9245`. Not a packaged-app connection setting. |

Credential-store service is `org.openutm.adapter`; entries are keyed by configuration path. Moving the configuration can require entering credentials again. A second launch opens the existing application instance, even with a different path: quit before switching datasets.

Example configuration for **local synthetic monitoring**, using an unauthenticated test server:

```json
{
  "baseURL": "http://127.0.0.1:8000",
  "authMode": "none",
  "source": "direct",
  "autoConfigureGCS": false,
  "transport": "udp",
  "address": "127.0.0.1:14551",
  "systemID": 0,
  "publish": false,
  "sessionDelivery": false,
  "aerobridgeEnabled": false,
  "bridgeAddress": "127.0.0.1:14552"
}
```

This example still attempts server checks and restriction refreshes. Save it in a separate test directory and use the [synthetic sender](../DEVELOPING.md#test-without-a-vehicle). Never point synthetic delivery at an operational declaration.
