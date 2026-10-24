# UTM Adapter 0.2.0 compatibility

This release implements the adapter workflow in [Flight Blender #218](https://github.com/openutm/flight-blender/issues/218). Planning and declaration creation remain in Flight Spotlight / Flight Blender.

| Component | Integration | Verification |
| --- | --- | --- |
| Flight Blender | Existing telemetry/declaration/geofence APIs plus `/adapter_ops/sessions/{UUID}` | Go client tested against the actual FastAPI/SQLAlchemy application, including real UDP reception, restart, upload acknowledgement, duplicate uploads and exported-report readback. Deploy the companion `a218_session_records` migration before enabling archive synchronization. |
| QGroundControl 5.1.4 | MAVLink forwarding; custom FlyView layer for restriction polygons | CI builds a pinned upstream commit for macOS universal, Windows x64 and Linux x64, tests native restriction rendering, and boots the packaged application before upload. The minimal custom plugin preserves upstream firmware support and UI. Flight trials remain required. |
| Mission Planner 1.3.83 | Generated C# plugin: all connection telemetry and Flight Data restriction overlays | CI compiles the production generator's source against the checksum-pinned official release, then extracts the release ZIP and loads, initializes and unloads the DLL using .NET Framework. Connected-vehicle and Flight Data UI trials remain required. |
| Aerobridge | Optional bearer-authenticated aircraft/pilot/operation registry and flight-log API | HTTP contract tests cover stable batch keys and recovery after a stored upload loses its response. A live Aerobridge deployment remains to be verified. |
| MAVLink 1/2 | Autopilot component 1; distinct system IDs for concurrent aircraft | UDP/TCP tests and two-aircraft session integration test. Live delivery requires fresh MAVLink 2 ellipsoid altitude. Serial hardware remains to be verified. |
| macOS | Wails desktop application | Local native build and UI start/end/export smoke test; CI also builds the universal application. Ad-hoc signing, without notarization. |
| Windows x64 | Portable executable and NSIS installer | Build/test/package CI configured; runtime verification remains pending. |
| Linux x64 | Ubuntu 24.04 archive and AppImage | Build/test/package CI configured; runtime verification remains pending. GTK4, WebKitGTK 6 and a credential-store service are required. |

GCS downloads use the prefix `utm-adapter-gcs-`: QGroundControl ships as a macOS DMG, Windows installer and Linux AppImage; Mission Planner ships as a Windows plugin ZIP with installation instructions and inspection source. Mission Planner itself is not redistributed. Each artifact includes a checksum and a manifest recording the adapter version/commit and exact upstream build inputs in `build/gcs-versions.json`. These QGC builds retain the standard application/settings name, so install them in place of stock QGC.

The integration sources also ship as `utm-adapter-gcs-integrations.tar.gz`. Prepare a new QGC custom directory with `python3 scripts/prepare-qgc.py /path/to/qgroundcontrol`, then build QGC using its supported custom-build process. For custom Mission Planner endpoints, use the adapter's Ground stations screen instead of installing the release DLL. Install only one copy of the plugin (source or DLL). Both map integrations use `http://127.0.0.1:14552/v1/restrictions` by default; adjust the QML endpoint if the local bridge address changes.

Archives and installers include SHA-256 checksum files. Pushing a matching semantic-version tag, such as `v0.2.0`, triggers desktop and GCS builds and publishes a GitHub Release only after all builds and package checks pass. Uploads go to a draft before publication. The Desktop binaries workflow can be dispatched manually to build all artifacts without creating a release; GCS builds can also be dispatched independently. Releases are not automatically installed. macOS packages use ad-hoc signing without notarization.

Local records contain pilot contact details and flight positions. They are stored alongside `product.json` in `records.sqlite`; exports go in `exports/`. Protect and back up this directory. Start/end audit events and declaration history provide operational records, without cryptographic certification or tamper-proof guarantees.
