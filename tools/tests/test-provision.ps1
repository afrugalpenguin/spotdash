# Tests for tools\provision.ps1 against fake-spot-adb.cmd, a stateful stand-in
# for adb and an Echo Spot.
#
#   powershell -NoProfile -ExecutionPolicy Bypass -File tools\tests\test-provision.ps1
#
# Exits 0 when every check passes and 1 otherwise. No real device or firewall is
# touched: every run that could reach the firewall step passes -SkipFirewall,
# and the one test that does not is skipped when this session is elevated.

$ErrorActionPreference = 'Stop'

$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$script = (Resolve-Path (Join-Path $here '..\provision.ps1')).Path
$fakeAdb = Join-Path $here 'fake-spot-adb.cmd'
$root = Join-Path ([System.IO.Path]::GetTempPath()) ("provision-test-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root | Out-Null

$script:failures = 0
$script:checks = 0
$token = 'TestTokenAbc123Def456Ghi789Jkl012'

function Check([bool]$condition, [string]$message) {
    $script:checks++
    if ($condition) { Write-Host "  ok   $message" }
    else { $script:failures++; Write-Host "  FAIL $message" -ForegroundColor Red }
}

# A case is a folder with the fake device state, an APK, a config and a temp
# directory for the child process, so nothing leaks between cases.
function New-Case([string]$name, [string]$wlan = '203.0.113.55/24') {
    $dir = Join-Path $root $name
    New-Item -ItemType Directory -Path $dir | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $dir 'temp') | Out-Null
    New-Item -ItemType Directory -Path (Join-Path $dir 'logs') | Out-Null
    [System.IO.File]::WriteAllBytes((Join-Path $dir 'shell.apk'), [byte[]](1..200))
    $config = [ordered]@{ listen = '0.0.0.0:8765'; token = $token; sources = @{} }
    $config | ConvertTo-Json | Set-Content -Path (Join-Path $dir 'config.json') -Encoding ASCII
    $state = [ordered]@{
        calls = 0; root = $false; rootable = $true; installedHash = $null
        home = 'com.android.launcher3/.Launcher'; appop = 'default'; tz = 'GMT'
        brightMode = '1'; brightness = '100'; wlan = $wlan
        filesDir = $false; running = $false; pending = $null
        configuredUrl = $null; configuredToken = $null
    }
    $state | ConvertTo-Json | Set-Content -Path (Join-Path $dir 'state.json') -Encoding ASCII
    return $dir
}

function Get-State([string]$case) { return (Get-Content -Raw (Join-Path $case 'state.json') | ConvertFrom-Json) }
function Set-State([string]$case, $state) { $state | ConvertTo-Json | Set-Content -Path (Join-Path $case 'state.json') -Encoding ASCII }
function Get-AdbLog([string]$case) {
    $log = Join-Path $case 'adb.log'
    if (Test-Path $log) { return @(Get-Content $log) }
    return @()
}
function Touched([string]$case) {
    return @(Get-AdbLog $case | Where-Object { $_ -match '\b(install|push|root)\b' }).Count -gt 0
}

# The parameters most cases share, with any of them replaced or (with $null)
# dropped.
function Get-Params([string]$case, [hashtable]$override = @{}) {
    $p = [ordered]@{
        Apk = (Join-Path $case 'shell.apk'); Config = (Join-Path $case 'config.json')
        LogDir = (Join-Path $case 'logs'); Timezone = 'Europe/London'; AgentHost = '192.0.2.10'
    }
    foreach ($key in $override.Keys) { $p[$key] = $override[$key] }
    return $p
}

function Invoke-Provision([string]$case, [hashtable]$params, [string[]]$switches = @('-SkipFirewall'), [hashtable]$extraEnv = @{}) {
    $stdout = Join-Path $case ("out-" + [guid]::NewGuid().ToString('N') + ".txt")
    $stderr = Join-Path $case ("err-" + [guid]::NewGuid().ToString('N') + ".txt")
    $environment = @{
        FAKE_STATE = (Join-Path $case 'state.json'); FAKE_ADB_LOG = (Join-Path $case 'adb.log')
        FAKE_ADB_MODE = ''; FAKE_ADB_DROP_AFTER = ''; FAKE_ADB_HANG = ''
        FAKE_PAYLOAD_REJECT = ''; FAKE_PANEL_FAIL = ''; FAKE_ROOT_FLAKY = ''
        TEMP = (Join-Path $case 'temp'); TMP = (Join-Path $case 'temp')
    }
    foreach ($key in $extraEnv.Keys) { $environment[$key] = $extraEnv[$key] }
    $saved = @{}
    foreach ($key in $environment.Keys) {
        $saved[$key] = [Environment]::GetEnvironmentVariable($key)
        [Environment]::SetEnvironmentVariable($key, $environment[$key])
    }
    try {
        $argList = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "`"$script`"", '-Adb', "`"$fakeAdb`"")
        foreach ($key in $params.Keys) {
            if ($null -eq $params[$key]) { continue }
            $argList += @("-$key", "`"$($params[$key])`"")
        }
        $argList += $switches
        $clock = [System.Diagnostics.Stopwatch]::StartNew()
        $process = Start-Process -FilePath 'powershell.exe' -ArgumentList $argList -WorkingDirectory $case `
            -Wait -PassThru -NoNewWindow -RedirectStandardOutput $stdout -RedirectStandardError $stderr
        $clock.Stop()
        return [pscustomobject]@{
            Code = $process.ExitCode; Seconds = $clock.Elapsed.TotalSeconds
            Text = ((Get-Content -Raw $stdout) + (Get-Content -Raw $stderr))
        }
    }
    finally {
        foreach ($key in $saved.Keys) { [Environment]::SetEnvironmentVariable($key, $saved[$key]) }
    }
}

function Count([string]$text, [string]$pattern) { return ([regex]::Matches($text, $pattern)).Count }

try {
    Write-Host "fresh device: every step runs and the dash loads"
    $case = New-Case 'fresh'
    $r = Invoke-Provision $case (Get-Params $case)
    Check ($r.Code -eq 0) "exits 0 (got $($r.Code))"
    $s = Get-State $case
    Check ($s.root -eq $true) 'adb root was run'
    Check ($s.installedHash -eq (Get-FileHash -Algorithm SHA256 (Join-Path $case 'shell.apk')).Hash.ToLower()) 'the APK was installed'
    Check ($s.home -eq 'dev.spotdash.shell/.PanelActivity') 'the shell is the home activity'
    Check ($s.appop -eq 'allow') 'WRITE_SETTINGS is allowed'
    Check ($s.tz -eq 'Europe/London') 'the timezone is set'
    Check (($s.brightMode -eq '0') -and ($s.brightness -eq '255')) 'brightness is manual at 255'
    Check ($s.configuredUrl -eq 'http://192.0.2.10:8765') "the agent URL is the host and the config's port (got $($s.configuredUrl))"
    Check ($s.configuredToken -eq $token) 'the shell received the token from the config'
    Check ($null -eq $s.pending) 'no provisioning file is left on the device'
    Check ($r.Text -match '\[PASS\][^\r\n]*panel loaded') 'the last step reports the panel loaded'
    Check ((Count $r.Text '\[FAIL\]') -eq 0) 'no FAIL line'
    Check (-not $r.Text.Contains($token)) 'the token is not in the output'
    $logs = @(Get-ChildItem (Join-Path $case 'logs') -Filter 'provision-*.log')
    Check ($logs.Count -eq 1) 'one transcript file is written'
    $transcript = if ($logs.Count -eq 1) { Get-Content -Raw $logs[0].FullName } else { '' }
    Check (($transcript -match '\[PASS\]') -and -not $transcript.Contains($token)) 'and it has the steps but not the token'
    Check (@(Get-ChildItem (Join-Path $case 'temp') | Where-Object { $_.Name -match 'provision' }).Count -eq 0) 'no payload file is left in the PC temp folder'
    Check (@(Get-AdbLog $case | Where-Object { $_ -match '^(-s \S+ )?(install|push)' }).Count -eq 2) 'one install and one push were sent'

    Write-Host "second run: idempotent, unchanged, still passes"
    $before = Get-State $case
    $r = Invoke-Provision $case (Get-Params $case)
    Check ($r.Code -eq 0) "exits 0 (got $($r.Code))"
    $after = Get-State $case
    $fields = 'root', 'installedHash', 'home', 'appop', 'tz', 'brightMode', 'brightness', 'configuredUrl', 'configuredToken'
    $changed = @($fields | Where-Object { "$($before.$_)" -ne "$($after.$_)" })
    Check ($changed.Count -eq 0) "no field changed ($($changed -join ', '))"
    foreach ($step in 'adb root', 'install APK', 'home activity', 'WRITE_SETTINGS', 'timezone', 'brightness') {
        Check ($r.Text -match "\[SKIP\] ${step}:") "$step is skipped as already set"
    }
    Check (@(Get-AdbLog $case | Where-Object { $_ -match '\binstall\b' }).Count -eq 1) 'the APK is not installed a second time'

    Write-Host "no device, two devices, unauthorised device"
    $case = New-Case 'none'
    $r = Invoke-Provision $case (Get-Params $case) @('-SkipFirewall') @{ FAKE_ADB_MODE = 'none' }
    Check ($r.Code -ne 0) "no device exits non-zero (got $($r.Code))"
    Check ($r.Text -match 'No data transfer') 'and mentions the No data transfer quirk'
    foreach ($mode in 'two', 'unauthorized') {
        $case = New-Case "mode-$mode"
        $r = Invoke-Provision $case (Get-Params $case) @('-SkipFirewall') @{ FAKE_ADB_MODE = $mode }
        Check ($r.Code -ne 0) "$mode exits non-zero (got $($r.Code))"
        Check (-not (Touched $case)) "$mode changes nothing on the device"
    }

    Write-Host "checks that fail before the device is touched"
    $case = New-Case 'subnet'
    $r = Invoke-Provision $case (Get-Params $case @{ AgentHost = '' })
    Check ($r.Code -ne 0) "a PC not on the device's network exits non-zero (got $($r.Code))"
    Check ($r.Text -match 'different network') 'and says the two are on different networks'
    Check (-not (Touched $case)) 'and the device is untouched'

    $case = New-Case 'noapk'
    $r = Invoke-Provision $case (Get-Params $case @{ Apk = (Join-Path $case 'missing.apk') })
    Check ($r.Code -ne 0) "a missing APK exits non-zero (got $($r.Code))"
    Check (-not (Touched $case)) 'without touching the device'

    $case = New-Case 'notoken'
    '{"listen":"0.0.0.0:8765","sources":{}}' | Set-Content -Path (Join-Path $case 'config.json') -Encoding ASCII
    $r = Invoke-Provision $case (Get-Params $case)
    Check ($r.Code -ne 0) "a config with no token exits non-zero (got $($r.Code))"
    Check (-not (Touched $case)) 'without touching the device'

    $case = New-Case 'tzunmapped'
    $r = Invoke-Provision $case (Get-Params $case @{ Timezone = ''; WindowsTimezone = 'Fake Standard Time' })
    Check ($r.Code -ne 0) "an unmapped Windows timezone exits non-zero (got $($r.Code))"
    Check ($r.Text -match '-Timezone') 'and points at -Timezone'
    $case = New-Case 'tzmapped'
    $r = Invoke-Provision $case (Get-Params $case @{ Timezone = ''; WindowsTimezone = 'GMT Standard Time' })
    Check (($r.Code -eq 0) -and ((Get-State $case).tz -eq 'Europe/London')) 'a known Windows timezone maps to its IANA name'

    Write-Host "steps that fail on the device"
    $case = New-Case 'noroot'
    $state = Get-State $case
    $state.rootable = $false
    Set-State $case $state
    $r = Invoke-Provision $case (Get-Params $case)
    Check ($r.Code -ne 0) "no root exits non-zero (got $($r.Code))"
    Check ($r.Text -match '\[FAIL\][^\r\n]*root') 'and the FAIL line names root'
    Check ($null -eq (Get-State $case).installedHash) 'and nothing is installed'

    $case = New-Case 'flakyroot'
    $r = Invoke-Provision $case (Get-Params $case) @('-SkipFirewall') @{ FAKE_ROOT_FLAKY = '1' }
    Check ($r.Code -eq 0) "adb root that restarts adbd but exits non-zero is waited out (got $($r.Code))"

    $case = New-Case 'rejected'
    $r = Invoke-Provision $case (Get-Params $case) @('-SkipFirewall') @{ FAKE_PAYLOAD_REJECT = '1' }
    Check ($r.Code -ne 0) "a payload the shell refuses exits non-zero (got $($r.Code))"
    Check ($r.Text -match 'provisioning ignored') 'and the reason from the shell is shown'
    Check (-not $r.Text.Contains($token)) 'without printing the token'
    Check ($null -eq (Get-State $case).pending) 'and no provisioning file is left'

    $case = New-Case 'panelfail'
    $r = Invoke-Provision $case (Get-Params $case @{ PanelWaitSec = 5 }) @('-SkipFirewall') @{ FAKE_PANEL_FAIL = '1' }
    Check ($r.Code -ne 0) "a panel that does not load exits non-zero (got $($r.Code))"
    Check ($r.Text -match '\[FAIL\][^\r\n]*HTTP 401') 'and the FAIL line carries the reason'

    Write-Host "unplugged or hung part way through"
    $case = New-Case 'drop'
    $r = Invoke-Provision $case (Get-Params $case) @('-SkipFirewall') @{ FAKE_ADB_DROP_AFTER = '9' }
    Check ($r.Code -ne 0) "exits non-zero (got $($r.Code))"
    Check ($r.Seconds -lt 30) "ends within 30 seconds (took $([math]::Round($r.Seconds, 1)))"
    Check ($r.Text -match '\[FAIL\]') 'with a FAIL line'

    $case = New-Case 'hang'
    $r = Invoke-Provision $case (Get-Params $case @{ AdbTimeoutSec = 3 }) @('-SkipFirewall') @{ FAKE_ADB_HANG = 'push' }
    Check ($r.Code -ne 0) "a command that never returns exits non-zero (got $($r.Code))"
    Check ($r.Seconds -lt 40) "and does not hang (took $([math]::Round($r.Seconds, 1)))"
    Check ($r.Text -match '\[FAIL\][^\r\n]*(push|provision|payload)') 'and the FAIL line names the step'
    Check (@(Get-AdbLog $case | Where-Object { $_ -match 'rm -f' }).Count -ge 1) 'and the payload is removed from the device'

    Write-Host "the PC address is chosen from the device's own subnet"
    $local = @(Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
        Where-Object { $_.IPAddress -notmatch '^(127|169\.254)\.' -and $_.PrefixLength -le 24 -and $_.PrefixLength -ge 8 }) | Select-Object -First 1
    if ($local) {
        $octets = $local.IPAddress.Split('.')
        $other = if ($octets[3] -eq '222') { '221' } else { '222' }
        $wlan = "$($octets[0]).$($octets[1]).$($octets[2]).$other/$($local.PrefixLength)"
        $case = New-Case 'realsubnet' $wlan
        $r = Invoke-Provision $case (Get-Params $case @{ AgentHost = '' })
        Check ($r.Code -eq 0) "exits 0 (got $($r.Code))"
        $url = (Get-State $case).configuredUrl
        $pcAddresses = @(Get-NetIPAddress -AddressFamily IPv4 | ForEach-Object { $_.IPAddress })
        Check ($url -and ($pcAddresses -contains ([uri]$url).Host)) 'the URL host is an address of this PC'
    }
    else { Write-Host "  skip: no suitable local IPv4 address on this machine" }

    Write-Host "firewall: refuses without elevation and says exactly what to run"
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $elevated = (New-Object Security.Principal.WindowsPrincipal $identity).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if ($elevated) { Write-Host "  skip: this session is elevated, and the test must not create a real rule" }
    else {
        $case = New-Case 'firewall'
        $r = Invoke-Provision $case (Get-Params $case) @()
        Check ($r.Code -ne 0) "exits non-zero (got $($r.Code))"
        Check ($r.Text -match 'New-NetFirewallRule') 'the message carries the New-NetFirewallRule command'
        Check ($r.Text -match '-RemoteAddress 203\.0\.113\.55') 'limited to the device address'
        Check ($r.Text -match '-LocalPort 8765') "for the config's port"
        Check ($r.Text -match '-SkipFirewall') 'and mentions -SkipFirewall'
    }

    Write-Host "the config can come from the agent itself"
    $case = New-Case 'exe'
    $fakeExe = Join-Path $case 'fake-agent.cmd'
    "@echo off`r`necho $(Join-Path $case 'config.json')" | Set-Content -Path $fakeExe -Encoding ASCII
    $r = Invoke-Provision $case (Get-Params $case @{ Config = ''; Exe = $fakeExe })
    Check ($r.Code -eq 0) "-config-path output is used when -Config is empty (got $($r.Code))"
}
finally {
    Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
}

Write-Host ""
if ($script:failures -eq 0) { Write-Host "all $($script:checks) checks passed"; exit 0 }
Write-Host "$($script:failures) of $($script:checks) checks FAILED" -ForegroundColor Red
exit 1
