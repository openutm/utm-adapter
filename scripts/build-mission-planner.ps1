param(
    [Parameter(Mandatory=$true)][string]$MissionPlannerDirectory,
    [Parameter(Mandatory=$true)][string]$OutputDirectory
)
$ErrorActionPreference = 'Stop'
$output = [IO.Path]::GetFullPath($OutputDirectory)
$plugins = Join-Path $output 'plugins'
$sources = Join-Path $output 'source'
New-Item -ItemType Directory -Force $plugins, $sources | Out-Null
$source = Join-Path $sources 'OpenUTMAdapter.cs'
$binary = Join-Path $plugins 'OpenUTMAdapter.dll'
go run ./cmd/mission-planner-plugin -out $source
if ($LASTEXITCODE -ne 0) { throw 'Plugin generation failed' }
if (-not (Test-Path -LiteralPath $source)) { throw "Generated source missing: $source" }
$framework = Get-ChildItem "${env:ProgramFiles(x86)}/Reference Assemblies/Microsoft/Framework/.NETFramework" -Directory |
    Where-Object { Test-Path (Join-Path $_.FullName 'Facades/netstandard.dll') } |
    Sort-Object Name -Descending | Select-Object -First 1
if (-not $framework) { throw '.NET Framework reference assemblies with netstandard facade are required' }
$compiler = Join-Path $env:WINDIR 'Microsoft.NET/Framework64/v4.0.30319/csc.exe'
$arguments = @('/nologo', '/noconfig', '/nostdlib+', '/target:library', '/langversion:5', '/codepage:65001', "/out:$binary")
foreach ($name in @('MissionPlanner.exe', 'MissionPlanner.ArduPilot.dll', 'MissionPlanner.Comms.dll', 'MissionPlanner.Utilities.dll', 'MissionPlanner.Controls.dll', 'Interfaces.dll', 'MAVLink.dll', 'GMap.NET.Core.dll', 'GMap.NET.Drawing.dll', 'GMap.NET.WindowsForms.dll', 'Newtonsoft.Json.dll')) {
    $arguments += "/reference:$(Join-Path $MissionPlannerDirectory $name)"
}
foreach ($name in @('mscorlib.dll', 'System.dll', 'System.Core.dll', 'System.Drawing.dll', 'System.Windows.Forms.dll', 'System.Xml.dll', 'Facades/netstandard.dll')) {
    $arguments += "/reference:$(Join-Path $framework.FullName $name)"
}
$arguments += $source
& $compiler @arguments
if ($LASTEXITCODE -ne 0) { throw 'Plugin compilation failed' }
Copy-Item integrations/mission-planner/README.md (Join-Path $output 'README.md')
