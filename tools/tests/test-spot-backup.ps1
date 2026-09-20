# Tests for tools\spot-backup.ps1, run against fake-adb.cmd instead of a real
# device.
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File tools\tests\test-spot-backup.ps1
#
# Exits 0 when every check passes and 1 otherwise. Nothing here touches a real
# device: the fake adb answers only what the script is allowed to ask, so a
# command that changes a device makes a test fail.

$ErrorActionPreference = 'Stop'

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$script = (Resolve-Path (Join-Path $here '..\spot-backup.ps1')).Path
$fakeAdb = Join-Path $here 'fake-adb.cmd'
$root = Join-Path ([System.IO.Path]::GetTempPath()) ("spot-backup-test-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root | Out-Null

$script:failures = 0
$script:checks = 0

function Check([bool]$condition, [string]$message) {
    $script:checks++
    if ($condition) {
        Write-Host "  ok   $message"
    }
    else {
        $script:failures++
        Write-Host "  FAIL $message" -ForegroundColor Red
    }
}

$wanted = @('kb', 'dkb', 'lk_real', 'tee1_real', 'tee2_real', 'logo', 'MISC', 'boot', 'recovery', 'system', 'mmcblk0boot0', 'mmcblk0boot1')

# A fake device: one small file per block device, with contents that differ so a
# mixed-up pull would show.
function New-FakeDevice([string]$dir) {
    New-Item -ItemType Directory -Path $dir | Out-Null
    $rng = New-Object System.Random 42
    $devices = 1..11 | ForEach-Object { "mmcblk0p$_" }
    $devices += 'mmcblk0boot0', 'mmcblk0boot1', 'mmcblk0'
    $n = 0
    foreach ($device in $devices) {
        $n++
        $bytes = New-Object byte[] (1024 * (($n % 4) + 1))
        $rng.NextBytes($bytes)
        [System.IO.File]::WriteAllBytes((Join-Path $dir "$device.bin"), $bytes)
    }
}

# Runs spot-backup.ps1 with the fake adb and returns the exit code and output.
# Environment switches are set for the child only.
function Invoke-Backup([hashtable]$environment, [string[]]$arguments, [string]$workDir) {
    $stdout = Join-Path $root ("out-" + [guid]::NewGuid().ToString('N') + ".txt")
    $stderr = Join-Path $root ("err-" + [guid]::NewGuid().ToString('N') + ".txt")
    $saved = @{}
    foreach ($key in $environment.Keys) {
        $saved[$key] = [Environment]::GetEnvironmentVariable($key)
        [Environment]::SetEnvironmentVariable($key, $environment[$key])
    }
    try {
        $argList = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "`"$script`"", '-Adb', "`"$fakeAdb`"") + $arguments
        $process = Start-Process -FilePath 'powershell.exe' -ArgumentList $argList `
            -WorkingDirectory $workDir -Wait -PassThru -NoNewWindow `
            -RedirectStandardOutput $stdout -RedirectStandardError $stderr
        return [pscustomobject]@{
            Code = $process.ExitCode
            Text = ((Get-Content -Raw $stdout) + (Get-Content -Raw $stderr))
        }
    }
    finally {
        foreach ($key in $saved.Keys) {
            [Environment]::SetEnvironmentVariable($key, $saved[$key])
        }
    }
}

function New-Case([string]$name) {
    $dir = Join-Path $root $name
    New-Item -ItemType Directory -Path $dir | Out-Null
    New-FakeDevice (Join-Path $dir 'device')
    return $dir
}

function Env-For([string]$case, [hashtable]$extra) {
    $environment = @{
        FAKE_ADB_DIR = (Join-Path $case 'device')
        FAKE_ADB_LOG = (Join-Path $case 'adb.log')
        FAKE_ADB_MODE = ''
        FAKE_ADB_STATE = ''
        FAKE_ADB_MISSING = ''
        FAKE_ADB_LIE_HASH = ''
        FAKE_ADB_BAD_PULL = ''
        FAKE_ADB_NO_SHA = ''
        FAKE_ADB_BLOCKS = ''
    }
    if ($extra) { foreach ($key in $extra.Keys) { $environment[$key] = $extra[$key] } }
    return $environment
}

function Adb-Log([string]$case) {
    $log = Join-Path $case 'adb.log'
    if (Test-Path $log) { return @(Get-Content $log) }
    return @()
}

try {
    Write-Host "happy path: everything pulled and verified"
    $case = New-Case 'happy'
    $out = Join-Path $case 'backup'
    $r = Invoke-Backup (Env-For $case @{}) @('-OutDir', "`"$out`"") $case
    Check ($r.Code -eq 0) "exits 0 (got $($r.Code))"
    foreach ($name in $wanted) {
        Check (Test-Path (Join-Path $out "$name.img")) "$name.img exists"
    }
    Check (Test-Path (Join-Path $out 'manifest.txt')) 'manifest.txt exists'
    Check (([regex]::Matches($r.Text, '\[PASS\]')).Count -ge $wanted.Count) 'one PASS line per partition'
    Check ($r.Text -match 'all \d+ files match') 'ends with an all-match summary'
    Check ($r.Text -match 'second location') 'tells the user to copy the folder elsewhere'
    Check (-not (Test-Path (Join-Path $out 'userdata.img'))) 'pulls only the listed partitions'

    Write-Host "manifest: sha256sum format, one line per file, sizes recorded"
    $manifest = Get-Content (Join-Path $out 'manifest.txt')
    $hashLines = @($manifest | Where-Object { $_ -match '^[0-9a-f]{64}  \S+\.img$' })
    Check ($hashLines.Count -eq $wanted.Count) "$($wanted.Count) hash lines in sha256sum -c format (got $($hashLines.Count))"
    Check (@($manifest | Where-Object { $_ -match '^# size \d+ \S+\.img$' }).Count -eq $wanted.Count) 'a size comment per file'
    $bootHash = (Get-FileHash -Algorithm SHA256 (Join-Path $out 'boot.img')).Hash.ToLower()
    Check ($manifest -contains "$bootHash  boot.img") 'the boot.img line carries the real hash'

    Write-Host "read only: only the commands it is allowed to send"
    $log = Adb-Log $case
    $allowed = '^(-s \S+ )?(devices|get-state|pull|shell (ls -d|ls -l|cat /proc/partitions|sha256sum|toybox sha256sum|busybox sha256sum|getprop))'
    $bad = @($log | Where-Object { $_ -notmatch $allowed })
    Check ($bad.Count -eq 0) "no command outside the read-only set ($($bad -join ' | '))"
    Check (@($log | Where-Object { $_ -match '\bpush\b|\brm\b|\bdd\b|\bmount\b|\breboot\b|\bformat\b|\bwipe\b' }).Count -eq 0) 'nothing that writes, wipes or reboots'
    Check (-not (Select-String -Path $script -Pattern 'adb push', 'dd of=', ' rm ' -SimpleMatch -Quiet)) 'the script text contains no push, dd of= or rm'

    Write-Host "verify only: passes on a good folder, names the damaged file"
    $r = Invoke-Backup @{} @('-VerifyOnly', "`"$out`"") $case
    Check ($r.Code -eq 0) "verify exits 0 on an intact folder (got $($r.Code))"
    Add-Content -Path (Join-Path $out 'boot.img') -Value 'x' -NoNewline
    $r = Invoke-Backup @{} @('-VerifyOnly', "`"$out`"") $case
    Check ($r.Code -eq 1) "verify exits 1 after corrupting a file (got $($r.Code))"
    Check ($r.Text -match '\[FAIL\] boot\.img') 'and reports boot.img as the failure'
    Check (-not ($r.Text -match '\[FAIL\] recovery\.img')) 'without blaming the intact files'
    Remove-Item (Join-Path $out 'boot.img')
    $r = Invoke-Backup @{} @('-VerifyOnly', "`"$out`"") $case
    Check (($r.Code -eq 1) -and ($r.Text -match '\[FAIL\] boot\.img')) 'a missing file is a failure too'
    $r = Invoke-Backup @{} @('-VerifyOnly', "`"$(Join-Path $case 'nope')`"") $case
    Check ($r.Code -ne 0) 'verifying a folder that does not exist fails'

    Write-Host "run from another directory: files go only to the reported folder"
    $case = New-Case 'cwd'
    $elsewhere = Join-Path $case 'somewhere-else'
    New-Item -ItemType Directory -Path $elsewhere | Out-Null
    $r = Invoke-Backup (Env-For $case @{}) @() $elsewhere
    Check ($r.Code -eq 0) "default output folder works (got $($r.Code))"
    $created = @(Get-ChildItem $elsewhere)
    Check (($created.Count -eq 1) -and $created[0].PSIsContainer -and ($created[0].Name -like 'spot-backup-*')) 'exactly one spot-backup-<timestamp> folder appears in the working directory'
    Check ((@(Get-ChildItem $created[0].FullName -Filter '*.img').Count) -eq $wanted.Count) 'and every image is inside it'
    Check ($r.Text.Contains($created[0].FullName)) 'the output names the folder by its full path'

    Write-Host "not in recovery: refuses before doing anything"
    $case = New-Case 'android'
    $out = Join-Path $case 'backup'
    $r = Invoke-Backup (Env-For $case @{ FAKE_ADB_STATE = 'device' }) @('-OutDir', "`"$out`"") $case
    Check ($r.Code -ne 0) "exits non-zero (got $($r.Code))"
    Check ($r.Text -match 'TWRP') 'says the device must be in TWRP'
    Check (-not (Test-Path $out)) 'creates no folder'
    Check (@(Adb-Log $case | Where-Object { $_ -match '\bpull\b|\bshell\b' }).Count -eq 0) 'sent no pull or shell command'

    Write-Host "no device, two devices, unauthorised device"
    foreach ($mode in 'none', 'two', 'unauthorized') {
        $case = New-Case "mode-$mode"
        $r = Invoke-Backup (Env-For $case @{ FAKE_ADB_MODE = $mode }) @('-OutDir', "`"$(Join-Path $case 'backup')`"") $case
        Check ($r.Code -ne 0) "mode '$mode' exits non-zero (got $($r.Code))"
        Check (-not (Test-Path (Join-Path $case 'backup'))) "mode '$mode' creates no folder"
        Check (@(Adb-Log $case | Where-Object { $_ -match '\bpull\b' }).Count -eq 0) "mode '$mode' pulls nothing"
    }

    Write-Host "a partition missing from the by-name map: stop before pulling anything"
    $case = New-Case 'missing'
    $out = Join-Path $case 'backup'
    $r = Invoke-Backup (Env-For $case @{ FAKE_ADB_MISSING = 'lk_real' }) @('-OutDir', "`"$out`"") $case
    Check ($r.Code -ne 0) "exits non-zero (got $($r.Code))"
    Check ($r.Text -match 'lk_real') 'names the missing partition'
    Check (@(Adb-Log $case | Where-Object { $_ -match '\bpull\b' }).Count -eq 0) 'pulled nothing'

    Write-Host "a copy that differs from the device is caught"
    $case = New-Case 'badpull'
    $out = Join-Path $case 'backup'
    $r = Invoke-Backup (Env-For $case @{ FAKE_ADB_BAD_PULL = 'mmcblk0p8' }) @('-OutDir', "`"$out`"") $case
    Check ($r.Code -eq 1) "a pulled file with the wrong size exits 1 (got $($r.Code))"
    Check ($r.Text -match '\[FAIL\] boot') 'and it is boot that is reported'
    Check (-not ($r.Text -match 'all \d+ files match')) 'no all-match summary'

    $case = New-Case 'lie'
    $out = Join-Path $case 'backup'
    $r = Invoke-Backup (Env-For $case @{ FAKE_ADB_LIE_HASH = 'mmcblk0p9' }) @('-OutDir', "`"$out`"") $case
    Check ($r.Code -eq 1) "a hash that differs from the device exits 1 (got $($r.Code))"
    Check ($r.Text -match '\[FAIL\] recovery') 'and it is recovery that is reported'

    # Same bytes, same hash, but the kernel says the partition is bigger than
    # what came back: only the size check can see this one.
    $case = New-Case 'size'
    $out = Join-Path $case 'backup'
    $r = Invoke-Backup (Env-For $case @{ FAKE_ADB_BLOCKS = 'mmcblk0p10' }) @('-OutDir', "`"$out`"") $case
    Check ($r.Code -eq 1) "a copy the wrong size for its partition exits 1 (got $($r.Code))"
    Check ($r.Text -match '\[FAIL\] system: the copy is') 'and it is system that is reported, as a size problem'

    Write-Host "no sha256sum on the device: refuses rather than trusting the copy"
    $case = New-Case 'nosha'
    $out = Join-Path $case 'backup'
    $r = Invoke-Backup (Env-For $case @{ FAKE_ADB_NO_SHA = '1' }) @('-OutDir', "`"$out`"") $case
    Check ($r.Code -ne 0) "exits non-zero (got $($r.Code))"
    Check ($r.Text -match 'sha256') 'says why'
    Check (@(Adb-Log $case | Where-Object { $_ -match '\bpull\b' }).Count -eq 0) 'pulled nothing'

    Write-Host "an output folder that already has files: refuses to mix"
    $case = New-Case 'occupied'
    $out = Join-Path $case 'backup'
    New-Item -ItemType Directory -Path $out | Out-Null
    Set-Content -Path (Join-Path $out 'old.txt') -Value 'earlier backup'
    $r = Invoke-Backup (Env-For $case @{}) @('-OutDir', "`"$out`"") $case
    Check ($r.Code -ne 0) "exits non-zero (got $($r.Code))"
    Check (@(Get-ChildItem $out).Count -eq 1) 'leaves the folder as it was'

    Write-Host "full disk image on request"
    $case = New-Case 'full'
    $out = Join-Path $case 'backup'
    $r = Invoke-Backup (Env-For $case @{}) @('-OutDir', "`"$out`"", '-FullDisk') $case
    Check ($r.Code -eq 0) "exits 0 (got $($r.Code))"
    Check (Test-Path (Join-Path $out 'mmcblk0.img')) 'mmcblk0.img exists'
    Check (@((Get-Content (Join-Path $out 'manifest.txt')) -match '  mmcblk0\.img$').Count -eq 1) 'and is in the manifest'
}
finally {
    Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
}

Write-Host ""
if ($script:failures -eq 0) {
    Write-Host "all $($script:checks) checks passed"
    exit 0
}
Write-Host "$($script:failures) of $($script:checks) checks FAILED" -ForegroundColor Red
exit 1
