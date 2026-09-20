# A stateful stand-in for adb and an Echo Spot, used only by test-provision.ps1.
#
# It keeps the device in FAKE_STATE (a JSON file) so a second run sees what the
# first left behind. It answers the commands provision.ps1 may send and refuses
# anything else with exit 97. Every call is appended to FAKE_ADB_LOG.
#
# Switches, all optional environment variables:
#   FAKE_ADB_MODE            normal (default) | none | two | unauthorized
#   FAKE_ADB_DROP_AFTER      after this many calls every call fails as if unplugged
#   FAKE_ADB_HANG            a command word (for example push) that never returns
#   FAKE_PAYLOAD_REJECT      set to 1 and the shell refuses the provisioning file
#   FAKE_PANEL_FAIL          set to 1 and the panel load fails with HTTP 401
#   FAKE_ROOT_FLAKY          set to 1 and adb root restarts adbd but exits non-zero, as real adb can

$ErrorActionPreference = 'Stop'

$statePath = $env:FAKE_STATE
$state = Get-Content -Raw $statePath | ConvertFrom-Json
$logcatPath = "$statePath.logcat"
$filesDir = '/data/media/0/Android/data/dev.spotdash.shell/files'
$provisionPath = "$filesDir/provision.json"
$apkPath = '/data/app/dev.spotdash.shell-1/base.apk'
$stamp = '09-20 19:00:00.000  1000  1000'

function Save { $state | ConvertTo-Json | Set-Content -Path $statePath -Encoding ASCII }
function Refuse($what) { [Console]::Error.WriteLine("fake-spot-adb: not a command provision.ps1 may send: $what"); exit 97 }
function Fail($message, $code = 1) { [Console]::Error.WriteLine($message); exit $code }
function LogLine($text) { Add-Content -Path $logcatPath -Value "$stamp I spotdash: $text" }

if ($env:FAKE_ADB_LOG) { Add-Content -Path $env:FAKE_ADB_LOG -Value ($args -join ' ') }

# Every call counts, so a device can vanish part way through a run.
$state.calls = [int]$state.calls + 1
if ($env:FAKE_ADB_DROP_AFTER -and $state.calls -gt [int]$env:FAKE_ADB_DROP_AFTER) {
    Save
    Fail 'error: device offline'
}
Save

$rest = @($args)
if ($rest.Count -ge 2 -and $rest[0] -eq '-s') { $rest = $rest[2..($rest.Count - 1)] }
$command = $rest[0]
if ($env:FAKE_ADB_HANG -and $env:FAKE_ADB_HANG -eq $command) { Start-Sleep -Seconds 120 }

$mode = if ($env:FAKE_ADB_MODE) { $env:FAKE_ADB_MODE } else { 'normal' }

switch ($command) {
    'devices' {
        'List of devices attached'
        switch ($mode) {
            'none' { }
            'two' { "FAKE0001`tdevice"; "FAKE0002`tdevice" }
            'unauthorized' { "FAKE0001`tunauthorized" }
            default { "FAKE0001`tdevice" }
        }
        ''
        exit 0
    }
    'wait-for-device' { exit 0 }
    'root' {
        if ($state.root) { 'adbd is already running as root'; exit 0 }
        if (-not $state.rootable) { Fail 'adbd cannot run as root in production builds' }
        $state.root = $true
        Save
        'restarting adbd as root'
        if ($env:FAKE_ROOT_FLAKY -eq '1') { Fail 'timeout expired while waiting for device' }
        exit 0
    }
    'install' {
        $apk = $rest[-1]
        if (-not (Test-Path $apk)) { Fail "adb: error: failed to stat $apk" }
        $state.installedHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $apk).Hash.ToLower()
        Save
        'Success'
        exit 0
    }
    'push' {
        $local = $rest[1]
        $remote = $rest[2]
        if (-not $state.root -or -not $state.filesDir) { Fail "adb: error: failed to copy '$local' to '$remote': Permission denied" }
        if ($remote -ne $provisionPath) { Fail "adb: error: failed to copy '$local' to '$remote': No such file or directory" }
        $state.pending = [System.IO.File]::ReadAllText($local)
        Save
        "$local`: 1 file pushed."
        exit 0
    }
    'logcat' {
        $line = ($rest[1..($rest.Count - 1)]) -join ' '
        if ($line -eq '-c') { Set-Content -Path $logcatPath -Value @(); exit 0 }
        if ($line -eq '-d -s spotdash') { if (Test-Path $logcatPath) { Get-Content $logcatPath }; exit 0 }
        Refuse "logcat $line"
    }
    'shell' {
        $line = ($rest[1..($rest.Count - 1)]) -join ' '
        switch -Regex ($line) {
            '^id$' {
                if ($state.root) { 'uid=0(root) gid=0(root)' } else { 'uid=2000(shell) gid=2000(shell)' }
                exit 0
            }
            '^pm path dev\.spotdash\.shell$' {
                if (-not $state.installedHash) { exit 1 }
                "package:$apkPath"
                exit 0
            }
            '^sha256sum (\S+)$' {
                if (-not $state.installedHash) { Fail 'sha256sum: No such file or directory' }
                "$($state.installedHash)  $($Matches[1])"
                exit 0
            }
            '^ip -4 -o addr show wlan0$' {
                "3: wlan0    inet $($state.wlan) brd 255.255.255.255 scope global wlan0"
                exit 0
            }
            '^ls -d (\S+)$' {
                if ($Matches[1] -eq $filesDir -and $state.filesDir) { $filesDir; exit 0 }
                Fail "ls: $($Matches[1]): No such file or directory"
            }
            '^rm -f (\S+)$' {
                if ($Matches[1] -eq $provisionPath) { $state.pending = $null; Save }
                exit 0
            }
            '^am start -n dev\.spotdash\.shell/\.PanelActivity$' {
                if (-not $state.installedHash) { 'Error type 3'; 'Error: Activity class {dev.spotdash.shell/dev.spotdash.shell.PanelActivity} does not exist.'; exit 0 }
                $wasRunning = [bool]$state.running
                $state.running = $true
                $state.filesDir = $true
                $provisioned = $false
                if ($state.pending) {
                    $text = $state.pending
                    $state.pending = $null
                    $payload = $null
                    try { $payload = $text | ConvertFrom-Json } catch { }
                    if ($env:FAKE_PAYLOAD_REJECT -eq '1' -or -not $payload -or -not $payload.agent_url -or -not $payload.token) {
                        LogLine 'provisioning ignored: "token" is missing'
                    }
                    else {
                        $state.configuredUrl = $payload.agent_url
                        $state.configuredToken = $payload.token
                        $provisioned = $true
                        $hostName = ([uri]$payload.agent_url).Host
                        LogLine "provisioning applied for $hostName"
                    }
                }
                if ($state.configuredUrl -and (-not $wasRunning -or $provisioned)) {
                    if ($env:FAKE_PANEL_FAIL -eq '1') { LogLine 'page load failed: HTTP 401 Unauthorized' } else { LogLine 'page loaded' }
                }
                Save
                'Starting: Intent { cmp=dev.spotdash.shell/.PanelActivity }'
                exit 0
            }
            '^am force-stop dev\.spotdash\.shell$' { $state.running = $false; Save; exit 0 }
            '^cmd package resolve-activity --brief -a android\.intent\.action\.MAIN -c android\.intent\.category\.HOME$' {
                'priority=0 preferredOrder=0 match=0x108000 specificIndex=-1 isDefault=true'
                $state.home
                exit 0
            }
            '^cmd package set-home-activity (\S+)$' { $state.home = $Matches[1]; Save; 'Success'; exit 0 }
            '^appops get dev\.spotdash\.shell WRITE_SETTINGS$' { "WRITE_SETTINGS: $($state.appop)"; exit 0 }
            '^appops set dev\.spotdash\.shell WRITE_SETTINGS (\S+)$' { $state.appop = $Matches[1]; Save; exit 0 }
            '^getprop persist\.sys\.timezone$' { $state.tz; exit 0 }
            '^setprop persist\.sys\.timezone (\S+)$' {
                if (-not $state.root) { Fail 'setprop failed' }
                $state.tz = $Matches[1]; Save; exit 0
            }
            '^settings get system (screen_brightness_mode|screen_brightness)$' {
                if ($Matches[1] -eq 'screen_brightness_mode') { "$($state.brightMode)" } else { "$($state.brightness)" }
                exit 0
            }
            '^settings put system (screen_brightness_mode|screen_brightness) (\d+)$' {
                if ($Matches[1] -eq 'screen_brightness_mode') { $state.brightMode = $Matches[2] } else { $state.brightness = $Matches[2] }
                Save
                exit 0
            }
            default { Refuse "shell $line" }
        }
    }
    default { Refuse ($rest -join ' ') }
}
