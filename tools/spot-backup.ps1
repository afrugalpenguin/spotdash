<#
.SYNOPSIS
Backs up the partitions of an Echo Spot booted into TWRP, and proves the copies.

.DESCRIPTION
The backup taken before flashing is the only route back to stock, so it is
checked, not just made. With the Spot in TWRP and connected over USB this:

  1. refuses to run unless exactly one device is attached and it is in recovery;
  2. reads the partition map from the device instead of using fixed numbers;
  3. pulls kb, dkb, lk_real, tee1_real, tee2_real, logo, MISC, boot, recovery,
     system, mmcblk0boot0 and mmcblk0boot1 (and, with -FullDisk, all of mmcblk0)
     into a new timestamped folder;
  4. checks each file's size against /proc/partitions and its sha256 against the
     sha256 the device computes for the same partition;
  5. writes manifest.txt (sha256sum -c format) listing the files that verified.

It is read only. It sends the device nothing but listings, reads and pulls, and
never changes anything on it. It exits 0 only when every file matched.

.PARAMETER OutDir
Where to put the backup. Defaults to spot-backup-<yyyyMMdd-HHmmss> in the current
directory. It is turned into a full path first, so a later change of directory
cannot move it. Must not already hold files.

.PARAMETER FullDisk
Also pull the whole eMMC (mmcblk0.img). Large: it checks there is room first.

.PARAMETER VerifyOnly
Re-check an existing backup folder against its manifest. No device is needed.

.PARAMETER Adb
The adb to run. Defaults to the one on PATH.

.EXAMPLE
.\tools\spot-backup.ps1

.EXAMPLE
.\tools\spot-backup.ps1 -VerifyOnly .\spot-backup-20260920-181500

Exit codes: 0 everything matched; 1 a file failed verification; 2 the run could
not start or could not finish (not in recovery, no adb, no device, and so on).

The folder holds data unique to this device. Keep it private: never commit it,
attach it to an issue or send it to anyone.
#>
[CmdletBinding()]
param(
    [string]$OutDir,
    [switch]$FullDisk,
    [string]$VerifyOnly,
    [string]$Adb = 'adb'
)

$ErrorActionPreference = 'Stop'

# Partitions to pull. The first ten are read through the by-name map; the two
# boot partitions are not in it and are addressed directly.
$WantedNames = @('kb', 'dkb', 'lk_real', 'tee1_real', 'tee2_real', 'logo', 'MISC', 'boot', 'recovery', 'system')
$DirectDevices = [ordered]@{
    mmcblk0boot0 = '/dev/block/mmcblk0boot0'
    mmcblk0boot1 = '/dev/block/mmcblk0boot1'
}
$FullDiskDevice = '/dev/block/mmcblk0'

# sha256 of no input at all, used to find out which sha256sum the device has.
$EmptySha256 = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'

function Write-Step([string]$tag, [string]$message, [string]$colour) {
    Write-Host "[$tag] $message" -ForegroundColor $colour
}
function Write-Pass([string]$message) { Write-Step 'PASS' $message 'Green' }
function Write-Fail([string]$message) { Write-Step 'FAIL' $message 'Red' }
function Write-Info([string]$message) { Write-Step 'INFO' $message 'Gray' }

# Stops the run because it cannot go on, with what to do about it.
function Stop-Run([string]$problem, [string]$next) {
    Write-Fail $problem
    if ($next) { Write-Host "       Next: $next" }
    exit 2
}

# ---------------------------------------------------------------------------
# Verifying a folder against its manifest. Needs no device.
# ---------------------------------------------------------------------------

function Get-Sha256([string]$path) {
    return (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLower()
}

function Test-Backup([string]$folder) {
    $full = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($folder)
    if (-not (Test-Path -LiteralPath $full -PathType Container)) {
        Stop-Run "Backup folder not found: $full" 'Check the path.'
    }
    $manifestPath = Join-Path $full 'manifest.txt'
    if (-not (Test-Path -LiteralPath $manifestPath)) {
        Stop-Run "No manifest.txt in $full" 'This is not a backup made by this script, or the manifest was lost.'
    }

    $lines = @(Get-Content -LiteralPath $manifestPath | ForEach-Object { $_.Trim() })
    $sizes = @{}
    $entries = @()
    $incomplete = @()
    foreach ($line in $lines) {
        if ($line -match '^# size (\d+) (\S+)$') { $sizes[$Matches[2]] = [int64]$Matches[1] }
        elseif ($line -match '^# INCOMPLETE: (.+)$') { $incomplete += $Matches[1] }
        elseif ($line -match '^([0-9a-f]{64})  ([A-Za-z0-9_.-]+\.img)$') {
            $entries += [pscustomobject]@{ Hash = $Matches[1]; Name = $Matches[2] }
        }
    }
    if ($entries.Count -eq 0) {
        Stop-Run "manifest.txt in $full lists no files" 'The backup did not complete.'
    }

    Write-Info "Verifying $($entries.Count) files in $full"
    $bad = 0
    foreach ($entry in $entries) {
        $file = Join-Path $full $entry.Name
        if (-not (Test-Path -LiteralPath $file)) {
            Write-Fail "$($entry.Name): file is missing"
            $bad++
            continue
        }
        $length = (Get-Item -LiteralPath $file).Length
        if ($sizes.ContainsKey($entry.Name) -and $sizes[$entry.Name] -ne $length) {
            Write-Fail "$($entry.Name): size is $length, manifest says $($sizes[$entry.Name])"
            $bad++
            continue
        }
        $hash = Get-Sha256 $file
        if ($hash -ne $entry.Hash) {
            Write-Fail "$($entry.Name): sha256 does not match the manifest"
            $bad++
            continue
        }
        Write-Pass "$($entry.Name): $length bytes, sha256 matches"
    }
    foreach ($name in $incomplete) {
        Write-Fail "$name did not verify when the backup was taken, so it is not in this backup"
        $bad++
    }

    if ($bad -eq 0) {
        Write-Pass "all $($entries.Count) files match"
        return 0
    }
    Write-Fail "$bad problem(s): this backup cannot be relied on"
    return 1
}

if ($VerifyOnly) {
    exit (Test-Backup $VerifyOnly)
}

# ---------------------------------------------------------------------------
# Talking to the device.
# ---------------------------------------------------------------------------

$script:Serial = $null

# Runs adb, with the chosen device once there is one. A native program writing
# to stderr is not an error here, only its exit code is.
function Invoke-Adb([string[]]$AdbArgs) {
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $all = @()
        if ($script:Serial) { $all += @('-s', $script:Serial) }
        $all += $AdbArgs
        $lines = @(& $Adb @all)
        return [pscustomobject]@{ Code = $LASTEXITCODE; Lines = @($lines | ForEach-Object { "$_".TrimEnd() }) }
    }
    finally {
        $ErrorActionPreference = $previous
    }
}

if (-not (Get-Command $Adb -ErrorAction SilentlyContinue)) {
    Stop-Run "adb was not found ($Adb)" 'Install Android platform-tools (https://developer.android.com/tools/releases/platform-tools) and put it on PATH, or pass -Adb <path>.'
}
Write-Pass "adb found: $((Get-Command $Adb).Source)"

# Exactly one device.
$list = Invoke-Adb @('devices')
$devices = @($list.Lines | Where-Object { $_ -match '^(\S+)\s+(\S+)$' -and $_ -notmatch '^List of devices' } |
    ForEach-Object { $null = $_ -match '^(\S+)\s+(\S+)$'; [pscustomobject]@{ Serial = $Matches[1]; State = $Matches[2] } })
if ($devices.Count -eq 0) {
    Stop-Run 'No device is attached.' 'Boot the Spot into TWRP, connect it over USB, and see docs/device.md ("adb shows nothing over USB") if adb still sees nothing.'
}
if ($devices.Count -gt 1) {
    Stop-Run "$($devices.Count) devices are attached." 'Unplug all but the Spot, then run this again. A backup of the wrong device is worse than none.'
}
if ($devices[0].State -in @('unauthorized', 'offline')) {
    Stop-Run "The device is $($devices[0].State)." 'Accept the USB debugging prompt on the Spot, or replug it, then run this again.'
}
$script:Serial = $devices[0].Serial
Write-Pass 'one device attached'

# In recovery, and only in recovery. From Android a partition is in use and can
# change while it is being read, which would make a backup that verifies but is
# not consistent.
$state = Invoke-Adb @('get-state')
if ($state.Code -ne 0 -or ($state.Lines -join '') -ne 'recovery') {
    Stop-Run "The device is not in recovery (it reports '$($state.Lines -join ' ')')." 'Boot the Spot into TWRP, then run this again.'
}
Write-Pass 'device is in recovery (TWRP)'

# Which sha256sum does the device have? Without one the copy cannot be checked
# against anything, so there is no point starting.
$shaCommand = $null
foreach ($candidate in @('sha256sum', 'toybox sha256sum', 'busybox sha256sum')) {
    $probe = Invoke-Adb @('shell', "$candidate /dev/null")
    if ($probe.Code -eq 0 -and (($probe.Lines -join ' ') -match $EmptySha256)) {
        $shaCommand = $candidate
        break
    }
}
if (-not $shaCommand) {
    Stop-Run 'The device has no working sha256sum, so the copies could not be checked against it.' 'Use a TWRP build that includes toybox or busybox with sha256sum.'
}
Write-Pass "device can hash partitions ($shaCommand)"

# The partition map, read from the device.
$byNameDirs = @((Invoke-Adb @('shell', 'ls -d /dev/block/platform/*/by-name')).Lines | Where-Object { $_ -like '/dev/*' })
if ($byNameDirs.Count -eq 0) {
    Stop-Run 'The device has no /dev/block/platform/*/by-name directory.' 'This does not look like the Spot; do not go on.'
}
$byName = @{}
foreach ($dir in $byNameDirs) {
    foreach ($line in (Invoke-Adb @('shell', "ls -l $dir")).Lines) {
        if ($line -match '(\S+) -> (/dev/block/\S+)$') { $byName[$Matches[1].ToLower()] = $Matches[2] }
    }
}

$plan = @()
$missing = @()
foreach ($name in $WantedNames) {
    if ($byName.ContainsKey($name.ToLower())) {
        $plan += [pscustomobject]@{ Name = $name; Device = $byName[$name.ToLower()] }
    }
    else { $missing += $name }
}
foreach ($name in $DirectDevices.Keys) {
    $plan += [pscustomobject]@{ Name = $name; Device = $DirectDevices[$name] }
}
if ($FullDisk) {
    $plan += [pscustomobject]@{ Name = 'mmcblk0'; Device = $FullDiskDevice }
}
if ($missing.Count -gt 0) {
    Stop-Run "Not in the device's partition map: $($missing -join ', ')." 'Nothing was pulled. Do not go on with a backup that is missing partitions.'
}

# Sizes, from the kernel.
$blocks = @{}
foreach ($line in (Invoke-Adb @('shell', 'cat /proc/partitions')).Lines) {
    if ($line -match '^\s*\d+\s+\d+\s+(\d+)\s+(\S+)$') { $blocks[$Matches[2]] = [int64]$Matches[1] }
}
$total = [int64]0
foreach ($item in $plan) {
    $kernelName = ($item.Device -split '/')[-1]
    if (-not $blocks.ContainsKey($kernelName)) {
        Stop-Run "$($item.Name): $kernelName is not listed in /proc/partitions." 'Nothing was pulled.'
    }
    $item | Add-Member -NotePropertyName Bytes -NotePropertyValue ($blocks[$kernelName] * 1024)
    $total += $item.Bytes
}
Write-Pass "partition map read: $($plan.Count) images, $([math]::Round($total / 1MB, 1)) MB in total"

# Where it goes.
if (-not $OutDir) { $OutDir = "spot-backup-$(Get-Date -Format 'yyyyMMdd-HHmmss')" }
$out = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($OutDir)
if ((Test-Path -LiteralPath $out) -and (@(Get-ChildItem -LiteralPath $out -Force).Count -gt 0)) {
    Stop-Run "$out already has files in it." 'Pick a new folder with -OutDir. Mixing two backups is how one gets restored from the wrong one.'
}
$root = [System.IO.Path]::GetPathRoot($out)
if ($root -and -not $root.StartsWith('\\')) {
    $free = (New-Object System.IO.DriveInfo $root).AvailableFreeSpace
    if ($free -lt $total) {
        Stop-Run "Not enough room on $root ($([math]::Round($free / 1MB)) MB free, $([math]::Round($total / 1MB)) MB needed)." 'Free some space or use -OutDir on another drive.'
    }
}
else {
    Write-Info 'Cannot check free space on a network path; make sure there is room.'
}
New-Item -ItemType Directory -Path $out -Force | Out-Null
Write-Pass "backup folder: $out"

# ---------------------------------------------------------------------------
# Pull and verify, one partition at a time.
# ---------------------------------------------------------------------------

$verified = @()
$failed = @()
foreach ($item in $plan) {
    $file = Join-Path $out "$($item.Name).img"
    Write-Info "$($item.Name): pulling $($item.Bytes) bytes from $($item.Device)"

    $pull = Invoke-Adb @('pull', $item.Device, $file)
    if ($pull.Code -ne 0 -or -not (Test-Path -LiteralPath $file)) {
        Write-Fail "$($item.Name): the pull failed ($($pull.Lines -join ' '))"
        $failed += $item.Name
        continue
    }

    $length = (Get-Item -LiteralPath $file).Length
    if ($length -ne $item.Bytes) {
        Write-Fail "$($item.Name): the copy is $length bytes but the partition is $($item.Bytes)"
        $failed += $item.Name
        continue
    }

    $remote = Invoke-Adb @('shell', "$shaCommand $($item.Device)")
    $remoteHash = $null
    if ($remote.Code -eq 0 -and (($remote.Lines -join ' ') -match '\b([0-9a-fA-F]{64})\b')) { $remoteHash = $Matches[1].ToLower() }
    if (-not $remoteHash) {
        Write-Fail "$($item.Name): the device did not return a sha256 ($($remote.Lines -join ' '))"
        $failed += $item.Name
        continue
    }

    $localHash = Get-Sha256 $file
    if ($localHash -ne $remoteHash) {
        Write-Fail "$($item.Name): sha256 on the PC does not match the device"
        $failed += $item.Name
        continue
    }

    Write-Pass "$($item.Name): $length bytes, size and sha256 match the device"
    $verified += [pscustomobject]@{ Name = "$($item.Name).img"; Bytes = $length; Hash = $localHash }
}

# The manifest lists only what verified, so it can never vouch for a bad copy. A
# partial backup says so in the file, and verifying it later fails.
$manifest = @(
    '# spotdash spot-backup manifest. Check with: sha256sum -c manifest.txt',
    "# created $((Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ'))",
    "# device $(((Invoke-Adb @('shell', 'getprop ro.product.device')).Lines -join ' ').Trim())"
)
foreach ($entry in $verified) { $manifest += "# size $($entry.Bytes) $($entry.Name)" }
foreach ($name in $failed) { $manifest += "# INCOMPLETE: $name" }
foreach ($entry in $verified) { $manifest += "$($entry.Hash)  $($entry.Name)" }
# One newline style, whatever the platform default: sha256sum reads a stray
# carriage return as part of the file name.
[System.IO.File]::WriteAllText((Join-Path $out 'manifest.txt'), (($manifest -join "`n") + "`n"), (New-Object System.Text.ASCIIEncoding))

Write-Host ''
if ($failed.Count -eq 0) {
    Write-Pass "all $($verified.Count) files match. Manifest: $(Join-Path $out 'manifest.txt')"
    Write-Host ''
    Write-Host 'Now copy this whole folder to a second location (another drive, or offline) before'
    Write-Host 'you change anything on the device. It is the only way back to stock.'
    Write-Host 'It holds data unique to this device: do not share it, attach it to an issue or commit it.'
    Write-Host "To check it again later: .\tools\spot-backup.ps1 -VerifyOnly `"$out`""
    exit 0
}

Write-Fail "$($failed.Count) of $($plan.Count) images did not verify: $($failed -join ', ')"
Write-Host 'Do not flash anything. Run this again into a new folder; if it keeps failing,'
Write-Host 'try another USB cable or port. The manifest lists only the images that did verify.'
exit 1
