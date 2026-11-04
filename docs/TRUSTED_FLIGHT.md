# Aerobridge PRO and Trusted Flight

Trusted Flight is optional and disabled by default. Configure it in **Connection settings → Aerobridge Trusted Flight**. The older bearer-token fleet registry setting is independent. Core telemetry, Flight Blender delivery and local sessions continue working when Trusted Flight is disabled or its configuration cannot be loaded.

## Prerequisites and handshake

Use a bidirectional MAVLink connection to the flight controller, directly or through a router. A receive-only GCS forwarder cannot serve command replies. The adapter targets autopilot component 1 and uses system ID 254. Do not assign that ID to another device on the link.

1. Configure the PRO server, Flight Passport client credentials, `aerobridge.read aerobridge.write` access, organization UUID and operator UUID.
2. Resume connections, choose the vehicle's system ID, and click **Connect and verify vehicle**. The adapter obtains a Passport token, checks `/ping/`, requests `AUTOPILOT_VERSION` and waits for a fresh response and heartbeat. It encodes the 18-byte nonzero UID2 as uppercase hexadecimal, matching the MAVSDK reference. Register that exact `flight_controller_id` in PRO with active aircraft status `1`.
3. Select a registry pilot and click **Assign pilot**. The backend requires `is_active: true` and the configured operator assignment.
4. The **Aerobridge Fleet Management** tab becomes available. Every backend command also checks the prerequisites. Heartbeats older than five seconds, controller changes, token expiry, configuration changes and remote API failures lock it again. Repeat the handshake after a failure. Server health is checked every ten seconds while ready.

Automatic handshake requires both `auto_connect_drone` and `auto_connect_server`. It selects a vehicle only when exactly one fresh system is present; it selects a pilot only when a valid `default_pilot_id` is configured. Automatic retries are spaced thirty seconds apart.

## Configuration and storage

`aerobridge_config.json` lives beside `product.json`; saves are atomic and use owner-only permissions (`0600` on Unix). The secret is stored in this file as required by the design. The frontend receives an empty secret; saving a blank value preserves the existing secret. Access tokens remain in memory. Use HTTPS for remote services. `domain` is retained for configuration compatibility; token requests use `url` plus `token_url` without overriding the HTTP Host header. Redirects are rejected to avoid forwarding credentials to another endpoint.

```json
{
  "aerobridge": {
    "enabled": false,
    "server_url": "https://api.aerobridge.example",
    "timeout_seconds": 30,
    "auto_connect_drone": false,
    "auto_connect_server": false,
    "watch_directory": "/absolute/path/to/outbound",
    "flight_passport": {
      "url": "https://passport.example",
      "domain": "passport.example",
      "token_url": "/oauth/token/",
      "audience": "aerobridge.example",
      "client_id": "",
      "client_secret": ""
    },
    "credentials": {
      "organization_id": "",
      "operator_id": "",
      "default_pilot_id": ""
    }
  }
}
```

Timeout must be 1–300 seconds. Use an absolute watch directory so launch location does not affect discovery. Only regular `.plan` files of at most 8 MiB are imported; symlinks are skipped. Plan JSON must identify itself as a QGroundControl `Plan` and contain mission items. PRO performs its own full plan validation.

`aerobridge_jobs.json` stores reconciliation markers and server-assigned UUIDs. PRO's model IDs are read only in its serializers, so the adapter appends a saved unique marker to plan/operation/credential names and reconciles the server collection before retrying writes. Logs are reconciled by operation and canonical JSON content. Preserve this file when backing up the configuration. If unreadable, Trusted Flight is disabled until the file is restored and the app restarted. The passive connector still starts.

Collected JSON logs are archived under `aerobridge_logs/<server-log-uuid>.json` with owner-only permissions after upload. **Retry signing** uses the uploaded record without downloading the vehicle file again. These files may contain sensitive flight data; include them in your retention policy.

## Plan workflows

For a GCS-created plan, save a QGroundControl `.plan` file into the watch directory. Once the handshake is ready, the watcher requires two unchanged observations at two-second intervals before import. Invalid local plans are reported without preventing later valid plans from importing; remote API failures stop the scan and lock the handshake. It uploads the plan and creates an operation using the verified aircraft and selected pilot. Automatic operations start five minutes after discovery and last one hour. The chosen interval and reconciliation markers are saved before requests. A retry reconciles an existing operation even after its start has passed; a new operation requires a future start.

To choose the schedule yourself, enter a local plan path and start/end times in Fleet Management and click **Import plan and create operation**. For a server-defined plan, select a stored plan and click **Create operation for this aircraft and pilot**. Refresh the lists to see newly created operations.

Select the operation and click **Request permission and load vehicle**. The adapter validates its aircraft, pilot, organization and operator assignments before requesting permission. PRO normally issues new permission only when the start is more than sixty seconds and less than one hour in the future. Denied, pending, malformed or mismatched permission prevents transfer.

Plans and operations have no edit or delete action in this module. A changed local plan creates a new record, so the adapter cannot mutate a plan locked by signed logs. Other clients must enforce the server's `is_editable` policy independently.

## MAVFTP artifact contract and firmware boundary

The adapter transfers these files into `trusted_flight/` on the selected flight controller:

| File | Content |
| --- | --- |
| `plan.json` | Exact `plan_file_json` from the selected server plan |
| `geocage.json` | Granted `geo_cage` array of latitude/longitude objects |
| `permission.json` | Granted permission's token object |
| `manifest.json` | Operation/plan UUIDs and SHA-256 digests of the three artifacts |

A previous manifest is removed before replacing files. Each artifact is downloaded again and checked, then the manifest is written last and checked. MAVFTP replies must match sequence, opcode, session and offset; timeouts retry the same transaction. Transfers abort if the verified identity becomes stale or the vehicle arms. Files are limited to 8 MiB on download.

This file layout is the adapter's explicit integration contract, not a confirmed firmware implementation. The supplied reference scripts use temporary filenames and do not specify a stable firmware-side ingestion protocol. Firmware must validate the permission signature, plan binding, validity interval, digests and complete manifest before consuming artifacts. The adapter does not arm the aircraft or send executable MAVLink mission/fence items. The issue specifies MAVFTP delivery; native mission/fence execution is not claimed by this implementation. A file-transfer success confirms stored bytes, not mission execution or flight authorization by firmware. Native mission/fence upload requires a separately agreed firmware workflow.

## Keys, certificates, components and logs

Generate the key pair using the vehicle's supported firmware procedure. **Register vehicle public key** reads a PEM or DER public key from MAVFTP, validates PKIX or RSA PKCS#1 public-key encoding, normalizes it to PEM `PUBLIC KEY` and registers it as an active RFM public-key credential. Private keys and files containing extra PEM blocks are rejected. A key may live outside `trusted_flight/`; enter the firmware-relative path, such as `APM/self_pubkey.der`. Absolute paths, empty path segments, traversal and backslashes are rejected. **Retrieve signed certificates** lists active X.509 credentials for the verified aircraft. **Verify component** uses PRO's actual `/registry/aircraft-component/verify/{aerobridge_id}` endpoint and requires a matching ID with `status: true`.

**Unresolved REQ-PKI-001:** the current Trusted Flight firmware contract is still unavailable. The reference's Python public-key script is TODO. Its [MAVProxy guide](https://github.com/openutm-labs/mavlink-aerobridge/blob/7d16a475059891293a97441a61ad9db30adf7e72/mavproxy/README.md) also links to the historical CubePilot `pr-digitalsky-india` branch. That branch's [KeyManager](https://github.com/CubePilot/ardupilot/blob/c2ccdd0f561d6b8ed63c9c1a043979fa3bd2e381/libraries/AP_Security/KeyManager.cpp) automatically creates an RSA key at secure startup when no valid key exists and exports `HAL_BOARD_STORAGE_DIRECTORY/self_pubkey.der`. This is evidence for DER retrieval support, not a remotely callable key-generation command or proof that the intended vehicle runs that firmware. Provisioning that historical firmware involves PIN setup, secure bootloader flashing and a power cycle; the adapter does not perform firmware provisioning or replace existing vehicle keys. Confirm the target firmware and its supported provisioning/generation procedure, then qualify it on hardware.

After landing and disarming, select the operation and enter the firmware-produced relative raw JSON log path (for example, `trusted_flight/flight_log.json` or the RFM storage directory). **Collect, upload and sign flight log** downloads it, validates that it is a JSON object, posts it to PRO and requests signing. Binary DataFlash/ULog conversion and automatic mission-completion detection are outside the supplied API contract. Repeating the action reconciles the existing raw record; an existing signed receipt avoids duplicate signing. Failures require reconnecting before retrying. Uploaded log IDs appear in the receipt list after refresh.

## Supplied PRO source limitations

The API mapping was checked against `openutm-labs/aerobridge-pro` commit `2162db1b644494e64f0eec1f5fa35f2e8a307551`. These upstream defects prevent a complete live-server permission/signing trial without fixes:

- `gcs_operations/views.py`, `FlightPermissionGenerate.put`: `issue_permission` is called without its required `organization_id` argument.
- `gcs_operations/views.py`, `FlightLogSign.put`: `sign_log` is called without its required `organization` argument.
- `gcs_operations/data_signer.py`, `sign_log`: the SHA-256 digest is already a string, then `.hexdigest()` is called on it again. After fixing that expression, `sign_json` still returns a Django signed string while the caller indexes it as `signed_data['signature']`; that must also be corrected upstream.

These are server failures; the adapter reports them, retains reconciliation state and never fabricates a grant or signature. No changes have been applied to the upstream PRO repository. A deployed server with these defects needs fixes and its configured Passport/signing service before end-to-end qualification.

## Validation

Backend integration tests use a Passport/PRO HTTP fixture and a real gomavlib peer exchanging MAVLink and MAVFTP packets over UDP. They cover inactive pilots, expiry and identity changes, permission denial/pending, packet loss and retry, transfer read-back failure, server-assigned IDs, duplicate prevention, watcher stability, key privacy, component verification, certificate filtering and signing failure/recovery. Frontend tests cover the Fleet Management lock and relock on disconnect. These are simulated protocol tests; no Trusted Flight firmware or live PRO deployment has been qualified.


## Issue #7 acceptance audit

“Implemented” below describes adapter behavior verified with simulated services/vehicles. It does not certify a live PRO server or Trusted Flight firmware. The issue remains incomplete because REQ-PKI-001 lacks a confirmed target firmware and supported generation/provisioning procedure.

| Requirement | Current implementation and evidence | Remaining qualification |
| --- | --- | --- |
| REQ-CFG-001/002 | Separate JSON schema, atomic private saves, disabled module isolation; `TestTrustedConfigSecretAndIsolation` | None for local configuration |
| REQ-WF-001, REQ-PLN-002 | Stable GCS file watching, PRO plan/operation creation and manual artifact loading; watcher and import integration tests | GCS user saves its own plan; live PRO and firmware trial |
| REQ-WF-002, REQ-PLN-001 | Server plan/operation selectors, aircraft/pilot assignment, exact stored plan transfer; `TestTrustedPermissionAndMAVFTP` | Live PRO and firmware ingestion |
| REQ-CON-001–005 | Passport credentials, fresh UID2 request, active aircraft verification, active pilot selection, frontend and backend locks; handshake/expiry/identity tests and UI lock test | Deployed Passport configuration |
| REQ-CON-006 | Drone/server state, one-second UI polling, ten-second server health checks | Display updates wait for a serialized in-flight command to finish |
| REQ-PKI-001 | Vehicle PEM/DER public-key retrieval and PRO registration; private keys rejected and registration reconciled in `TestTrustedPublicKeyAndComponents` | **Blocked: current vehicle firmware and supported key-generation/provisioning procedure are unconfirmed** |
| REQ-PKI-002/003 | Active aircraft certificate retrieval and PRO component verification; `TestTrustedPublicKeyAndComponents` | Live certificates and authentic hardware |
| REQ-PLN-003 | No remote plan mutation API or UI; revisions create a fresh record | Server and other clients enforce their own edit policy |
| REQ-PRM-001/002 | Denied/pending gate, permission/operation binding, cage transfer and read-back; permission and integrity tests | Upstream permission defect and firmware validation |
| REQ-LOG-001/002 | Post-flight disarmed JSON extraction, raw upload, receipt validation and signing retries; import/signing, precision and malformed receipt tests | Firmware-produced raw JSON and corrected PRO signer |

No firmware command has been invented to stand in for REQ-PKI-001. The repository reference `py_mavlink-aerobridge/2-get_public_key.py` explicitly leaves public-key retrieval as TODO. The linked historical CubePilot implementation generates keys during secure startup; applying that provisioning workflow to an unidentified current vehicle is not justified. Completion requires confirmation of the target firmware and its supported generation/provisioning procedure, followed by vehicle/API integration validation.
