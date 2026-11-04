# Flight Blender #218 acceptance

This tracks implementation and acceptance for [Flight Blender #218](https://github.com/openutm/flight-blender/issues/218), following the review dated 2026-11-02. Automated acceptance and field/release acceptance are separate: passing a fixture or DOM test does not establish connected-aircraft or installer behavior.

| Requirement | Implementation and repeatable evidence | Outstanding acceptance |
| --- | --- | --- |
| FR-01: QGC and Mission Planner | Built-in integrations; forwarding preservation tests, native QGC restriction tests, packaged QGC boot/feed checks and Windows plugin compile/load checks. | Connected-aircraft forwarding and interactive maps in both supported GCS versions. |
| FR-02: extensibility | `GCSIntegration` registry drives discovery, config validation, setup dispatch and the UI source selector. `TestThirdGCSIntegration` adds a third GCS, backs up its forwarding settings, receives real UDP telemetry, records and exports a session. Unknown IDs cannot fall through to Mission Planner setup. | None for the compiled-in extension contract. Binary-loaded third-party plugins are not required. |
| FR-03: declarations | Existing server declarations are cached locally with observed revision history and included in compliance reports; immutable session assignments and archives synchronize to Flight Blender. `TestDocumentHistoryAndExport`, `TestFlightBlenderWorkflow`. | The repository's current scope delegates declaration creation to Flight Spotlight / Flight Blender. Confirm this interpretation of FR-03 before closing the issue; local declaration creation/upload is not implemented. Offline cache captures observed revisions, not server changes made between refreshes. |
| FR-04–06: records, telemetry, pilot | Durable SQLite sessions, start/end audit, GPS/altitude/speed, required pilot name/licence/contact, JSON exports and acknowledged archive batches. Race-enabled backend suite and actual-server contract. | Physical telemetry/serial and production identity-provider checks. |
| FR-07: GCS restrictions | Local GeoJSON bridge and native overlays; QGC tests exercise Polygon/MultiPolygon rendering, cache preservation and stale indication. | Interactive GCS trials. Exterior boundaries include holes conservatively; operators must check published altitude and time limits. |
| FR-08–09: optional fleet | Aircraft/pilot/operation queries, immutable assignments and operation-specific logs. `TestPopulatedFleetSelectionAndLogRouting` covers populated, paginated registries, two assignments, bearer rejection, disabled mode and separate log destinations. Existing restart integration covers ambiguous upload recovery. | Production Aerobridge authentication and deployment. |
| FR-10–11: lightweight status UI | Overview displays Flight Blender access, optional Aerobridge status, telemetry freshness, vehicle values and parser diagnostics. Vitest renders the real Svelte component and exercises outages, retry, registered source selection, settings, populated fleet/declaration choices, session start/end and export. | Native window/tray acceptance on Windows/Linux. Configuration and recording controls remain operational utilities; planning, flight authorization and detailed pre-flight UI stay external. Product acceptance of that scope remains required. |
| NFR-01–02: distribution | Desktop/GCS package workflows and checksum verification; existing native package boot checks. No developer toolchain is needed to use packaged artifacts. | First official release; install/upgrade/tray/credential-store trials on supported target OSes, including native Linux AppImage. Linux runtime dependencies remain documented in the user guide. |
| NFR-03–04: config/version | Structured secret-free `product.json`, OS credential store, semantic version alignment and tag checks. Backend configuration tests and `scripts/check-version.mjs`. | None for implementation. |
| NFR-05: automated releases | Matching version tags build packages, verify checksums, upload to a draft and publish only after successful jobs. Compatibility matrix and pinned GCS inputs are committed. | First successful tag-triggered publication. A manual build cannot establish publication permissions. |

## Verification on 2026-11-04

Local Apple Silicon checks passed:

- `go test -race ./...` and `go vet ./...`.
- `npm --prefix frontend test` (five Svelte DOM interaction tests with a mocked Wails service boundary), `npm --prefix frontend run check` (application and test types), and `npm --prefix frontend run build`.
- `FLIGHT_BLENDER_ROOT=../flight-blender scripts/test-flight-blender.sh` (both actual-application contract tests, isolated temporary SQLite/server with development JWT verification bypass).
- Native `qmltestrunner` restriction rendering: three Qt test cases passed on Qt 6.11.2; `qmllint` passed. CI remains pinned to Qt 6.8.3.
- Python packaging helper suite (four tests), semantic version check and workflow lint.

The GitHub Releases list was empty when checked on this date. Windows compilation/package checks and earlier CI results are recorded in [COMPATIBILITY.md](../COMPATIBILITY.md); they have not been rerun locally on Windows for this change. CI now runs the UI interaction suite on both Linux and Windows.

## Remaining field and publication procedure

1. Build the reviewed revision using the full manual **Desktop binaries** workflow (`packages=all`). Keep the adapter commit, manifests, checksum result and workflow URL with the acceptance record.
2. On Windows, macOS and native Ubuntu 24.04, install/extract the packaged application using only documented runtime prerequisites. Start with a fresh config, save and remove credentials, verify pause/resume and tray/quit, restart and confirm sessions survive. Upgrade over the preceding package using the same config directory; verify records, settings and credentials remain accessible. Record OS, architecture, package checksum and outcome.
3. For QGC 5.1.4 and Mission Planner 1.3.83, close the GCS, configure forwarding, reopen and connect a test vehicle. Verify fresh GPS/altitude/speed in the adapter, session recording, and the expected native restriction polygon at its coordinates. Pan/zoom, refresh, disconnect the restriction feed and confirm cached/stale behavior. End, synchronize and export the session. Verify unrelated GCS settings and forwarding destinations were preserved.
4. In a dedicated deployed Flight Blender/Aerobridge environment, verify the actual OAuth/bearer flow and permissions, populated registry choices, two aircraft/pilots/operations, log routing and restart/retry. Keep credentials out of acceptance evidence.
5. Resolve the FR-03 and FR-10 scope decisions above. Align release versions and review compatibility limits. Push the approved matching version tag to run publication; verify all expected desktop/GCS artifacts, provenance and checksums on the published release, then download and launch those exact artifacts. Record the release and tag workflow URLs before marking NFR-01/NFR-05 accepted.

Do not mark the whole ticket complete solely from this local validation record.
