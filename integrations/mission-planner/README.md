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
