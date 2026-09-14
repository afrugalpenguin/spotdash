# Builds and starts the spotdash agent.
#
# Usage:
#   .\run.ps1              build and run
#   .\run.ps1 -SkipBuild   run the existing binary

[CmdletBinding()]
param(
    [switch]$SkipBuild
)

$ErrorActionPreference = 'Stop'
Set-Location -Path $PSScriptRoot

$binary = Join-Path $PSScriptRoot 'spotdash.exe'
$config = Join-Path $PSScriptRoot 'config.json'

if (-not (Test-Path $config)) {
    Write-Error "config.json not found. Copy config.example.json to config.json and set a token."
}

if (-not $SkipBuild) {
    $version = 'dev'
    try {
        $describe = git describe --tags --always --dirty 2>$null
        if ($LASTEXITCODE -eq 0 -and $describe) { $version = $describe }
    } catch {
        # Not a git checkout. 'dev' is fine.
    }

    Write-Host "Building spotdash $version"
    go build -ldflags "-X main.version=$version" -o $binary ./cmd/spotdash
    if ($LASTEXITCODE -ne 0) { Write-Error "build failed" }
}

& $binary
