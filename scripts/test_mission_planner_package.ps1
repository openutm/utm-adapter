# Run with Windows PowerShell (the .NET Framework runtime used by Mission Planner).
param(
    [Parameter(Mandatory=$true)][string]$Archive,
    [Parameter(Mandatory=$true)][string]$MissionPlannerDirectory
)
$ErrorActionPreference = 'Stop'
$destination = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString())
$upstream = [IO.Path]::GetFullPath($MissionPlannerDirectory)
$resolver = [ResolveEventHandler] {
    param($sender, $eventArgs)
    $name = ([Reflection.AssemblyName]$eventArgs.Name).Name
    foreach ($extension in @('.dll', '.exe')) {
        $path = Join-Path $upstream ($name + $extension)
        if (Test-Path $path) { return [Reflection.Assembly]::LoadFrom($path) }
    }
    return $null
}
try {
    Expand-Archive -LiteralPath $Archive -DestinationPath $destination
    [AppDomain]::CurrentDomain.add_AssemblyResolve($resolver)
    $assembly = [Reflection.Assembly]::LoadFrom((Join-Path $destination 'plugins/OpenUTMAdapter.dll'))
    $plugin = $assembly.CreateInstance('OpenUTM.AdapterForwarder')
    if (-not $plugin) { throw 'Packaged plugin type could not be created' }
    $manifest = Get-Content (Join-Path $destination 'build.json') -Raw | ConvertFrom-Json
    if ($plugin.Version -ne $manifest.adapterVersion) { throw 'Plugin/manifest version mismatch' }
    if (-not $plugin.Init() -or $plugin.loopratehz -ne 1) { throw 'Plugin initialization failed' }
    if (-not $plugin.Exit()) { throw 'Plugin unload failed' }
    Write-Host "Packaged plugin loaded, initialized and unloaded: $($plugin.Version)"
} finally {
    [AppDomain]::CurrentDomain.remove_AssemblyResolve($resolver)
    # Assemblies stay loaded until this process exits; the OS cleans up the temp DLL later.
    Remove-Item $destination -Recurse -Force -ErrorAction SilentlyContinue
}
