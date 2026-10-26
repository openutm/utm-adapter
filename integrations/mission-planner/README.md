# Mission Planner plugin

This package targets Mission Planner 1.3.83 on Windows x64 and UTM Adapter 0.2.0.
Close Mission Planner, extract this archive into its installation directory, then
restart it. The `plugins/OpenUTMAdapter.dll` forwards telemetry from all connected
vehicles to UDP `127.0.0.1:14551` and displays restrictions from
`http://127.0.0.1:14552/v1/restrictions` in Flight Data.

Install either the DLL or the C# source, never both. The `source/` directory is
included for inspection and rebuilding; it is deliberately outside `plugins/`.
Remove an older `plugins/OpenUTMAdapter.cs` before installing this DLL. For custom
endpoints, remove the DLL and use the adapter's Ground stations screen to install
the generated source plugin. Remove the installed plugin to uninstall.

Mission Planner itself and its dependencies are not included. The build manifest
records the exact upstream archive and adapter commit used for compilation.

## Verify the installation

1. Start UTM Adapter with UDP listen address `127.0.0.1:14551`, local map address
   `127.0.0.1:14552`, and a system filter matching your aircraft (`0` for all).
2. Start Mission Planner and connect aircraft normally. Check the adapter's
   Overview for incoming frames and vehicle telemetry. The plugin follows all
   connected vehicle links; each aircraft needs a distinct MAVLink system ID.
3. With the adapter connected to Flight Blender, open Flight Data and check the
   restriction overlay. The feed is available at the URL above. Missing/stale
   data retains the last boundaries with a warning; polygon holes are filled.
   Check published altitude/time limits separately.

No vehicle commands are sent. Forwarding alone does not enable Flight Blender
delivery or create a recording; configure those workflows in the adapter.

## Update, customize, or remove

Close Mission Planner before changing plugin files. Back up the existing plugin,
replace it with the new release DLL, and check that no old source/DLL copy remains
in `plugins/`. Restart and repeat the checks above. Do not copy the inspection
source into `plugins/` alongside the DLL.

For custom endpoints, remove `plugins/OpenUTMAdapter.dll`, save the desired UDP
and map addresses in the adapter, and use **Ground stations → Install OpenUTM
plugin** to generate `plugins/OpenUTMAdapter.cs`. The adapter's Remove button
handles its managed source file only; remove a release DLL manually while
Mission Planner is closed.

If no telemetry arrives, confirm matching ports, connected vehicles and the
system filter, and remove duplicate plugin copies. A built-in mirror is an
alternative for telemetry, but does not add the restriction overlay. If the map
is stale, confirm the adapter is running connections and that
`http://127.0.0.1:14552/v1/restrictions` returns the expected collection.

CI verifies compilation and packaged DLL loading, initialization and unload.
Connected-vehicle forwarding and interactive map trials remain required on the
target Windows installation.
