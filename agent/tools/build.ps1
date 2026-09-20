# Builds spotdash.exe for everyday use, with no console window.
#
# Usage:
#   .\tools\build.ps1                 build agent\spotdash.exe
#   .\tools\build.ps1 -Output C:\x\spotdash.exe
#
# A default go build makes a console program, which opens a console window on
# every launch. That is wrong for a tray app started at login, so this builds
# for the Windows GUI subsystem instead (-H=windowsgui). Nothing prints to a
# console then; everything, including startup failures, goes to spotdash.log
# next to config.json.
#
# For development use .\run.ps1, which keeps the console.

[CmdletBinding()]
param(
    [string]$Output
)

$ErrorActionPreference = 'Stop'

# This script lives in agent\tools, and the module is one up.
$agentDir = Split-Path -Parent $PSScriptRoot
Set-Location -Path $agentDir

if (-not $Output) { $Output = Join-Path $agentDir 'spotdash.exe' }

$version = 'dev'
try {
    $describe = git describe --tags --always --dirty 2>$null
    if ($LASTEXITCODE -eq 0 -and $describe) { $version = $describe }
} catch {
    # Not a git checkout. 'dev' is fine.
}

Write-Host "Building spotdash $version (no console window)"
go build -ldflags "-H=windowsgui -X main.version=$version" -o $Output ./cmd/spotdash
if ($LASTEXITCODE -ne 0) { Write-Error "build failed" }

Write-Host "Built $Output"
Write-Host "Run it, then tick 'Start with Windows' in its tray menu."
