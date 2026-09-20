<#
.SYNOPSIS
Sets up an Echo Spot over USB so the dash appears, in one command.

.DESCRIPTION
Run it with the Spot connected by USB and USB debugging on. It checks everything
that needs no device first, then goes through these steps and prints [PASS],
[SKIP] (already done) or [FAIL] for each. It stops at the first failure and says
what to do about it. Running it again changes nothing that is already right.

  1. adb root
  2. install or upgrade the shell APK
  3. start the shell once, so it creates its folder
  4. deliver the agent address and token as a provisioning file
  5. make the shell the home activity
  6. allow WRITE_SETTINGS for brightness control
  7. set the device timezone from this PC
  8. set brightness to manual at 255
  9. add a Windows Firewall rule for the agent port, limited to the Spot
 10. restart the shell and check that the panel loads

The token is never printed or logged. A transcript is written to
provision-<timestamp>.log next to this script.

.PARAMETER Apk
The shell APK. Defaults to shell\spotdash-shell-*.apk in the release zip, or the
debug build in a checkout.

.PARAMETER Exe
spotdash.exe, used to find the config (-config-path) and named in the firewall
rule. Defaults to the one next to tools\ or under agent\.

.PARAMETER Config
A config.json to read the token and port from. Defaults to the one the agent uses.

.PARAMETER AgentHost
This PC's address as the Spot sees it. Defaults to the address on the Spot's
subnet.

.PARAMETER Timezone
An IANA timezone name such as Europe/London. Defaults to a mapping of this PC's
timezone.

.PARAMETER SkipFirewall
Skip step 9, for a machine where the rule already exists or is managed elsewhere.

.NOTES
Needs adb (Android platform-tools) on PATH, or pass -Adb. Step 9 needs an elevated
PowerShell. Exit code 0 means the dash loaded, 1 means a step failed.
#>
[CmdletBinding()]
param(
    [string]$Apk,
    [string]$Exe,
    [string]$Config,
    [string]$AgentHost,
    [string]$Timezone,
    [string]$WindowsTimezone,
    [switch]$SkipFirewall,
    [string]$Adb = 'adb',
    [string]$LogDir,
    [int]$AdbTimeoutSec = 15,
    [int]$InstallTimeoutSec = 120,
    [int]$PanelWaitSec = 60
)

$ErrorActionPreference = 'Stop'

$Package = 'dev.spotdash.shell'
$Activity = "$Package/.PanelActivity"
$FilesDir = "/data/media/0/Android/data/$Package/files"
$ProvisionFile = "$FilesDir/provision.json"
$FirewallRule = 'spotdash agent (panel)'
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

# Windows timezone id to IANA name: the common part of the CLDR mapping. Anything
# else needs -Timezone.
$TimezoneMap = @{
    'UTC' = 'Etc/UTC'; 'GMT Standard Time' = 'Europe/London'; 'Greenwich Standard Time' = 'Atlantic/Reykjavik'
    'W. Europe Standard Time' = 'Europe/Berlin'; 'Central Europe Standard Time' = 'Europe/Budapest'
    'Romance Standard Time' = 'Europe/Paris'; 'Central European Standard Time' = 'Europe/Warsaw'
    'E. Europe Standard Time' = 'Europe/Chisinau'; 'GTB Standard Time' = 'Europe/Bucharest'
    'FLE Standard Time' = 'Europe/Kiev'; 'Russian Standard Time' = 'Europe/Moscow'; 'Turkey Standard Time' = 'Europe/Istanbul'
    'Eastern Standard Time' = 'America/New_York'; 'Central Standard Time' = 'America/Chicago'
    'Mountain Standard Time' = 'America/Denver'; 'US Mountain Standard Time' = 'America/Phoenix'
    'Pacific Standard Time' = 'America/Los_Angeles'; 'Alaskan Standard Time' = 'America/Anchorage'
    'Hawaiian Standard Time' = 'Pacific/Honolulu'; 'Atlantic Standard Time' = 'America/Halifax'
    'Newfoundland Standard Time' = 'America/St_Johns'; 'Canada Central Standard Time' = 'America/Regina'
    'Central America Standard Time' = 'America/Guatemala'; 'Mexico Standard Time' = 'America/Mexico_City'
    'SA Pacific Standard Time' = 'America/Bogota'; 'E. South America Standard Time' = 'America/Sao_Paulo'
    'Argentina Standard Time' = 'America/Buenos_Aires'; 'AUS Eastern Standard Time' = 'Australia/Sydney'
    'E. Australia Standard Time' = 'Australia/Brisbane'; 'AUS Central Standard Time' = 'Australia/Darwin'
    'Cen. Australia Standard Time' = 'Australia/Adelaide'; 'W. Australia Standard Time' = 'Australia/Perth'
    'Tasmania Standard Time' = 'Australia/Hobart'; 'New Zealand Standard Time' = 'Pacific/Auckland'
    'Tokyo Standard Time' = 'Asia/Tokyo'; 'Korea Standard Time' = 'Asia/Seoul'; 'China Standard Time' = 'Asia/Shanghai'
    'Taipei Standard Time' = 'Asia/Taipei'; 'Singapore Standard Time' = 'Asia/Singapore'; 'India Standard Time' = 'Asia/Kolkata'
    'Arabian Standard Time' = 'Asia/Dubai'; 'Israel Standard Time' = 'Asia/Jerusalem'; 'Egypt Standard Time' = 'Africa/Cairo'
    'South Africa Standard Time' = 'Africa/Johannesburg'; 'W. Central Africa Standard Time' = 'Africa/Lagos'
    'E. Africa Standard Time' = 'Africa/Nairobi'; 'Pakistan Standard Time' = 'Asia/Karachi'
    'SE Asia Standard Time' = 'Asia/Bangkok'; 'Morocco Standard Time' = 'Africa/Casablanca'
}

# ---------------------------------------------------------------------------
# Output. Every line also goes to the transcript, with the token scrubbed.
# ---------------------------------------------------------------------------

if (-not $LogDir) { $LogDir = $scriptDir }
$script:LogFile = Join-Path $LogDir ("provision-{0}.log" -f (Get-Date -Format 'yyyyMMdd-HHmmss'))
$script:Secret = $null
$script:Serial = $null
$script:PayloadSent = $false

function Scrub([string]$text) {
    if ($script:Secret) { return $text.Replace($script:Secret, '<token>') }
    return $text
}

function Say([string]$text, [string]$colour = 'Gray') {
    $clean = Scrub $text
    Write-Host $clean -ForegroundColor $colour
    try { Add-Content -Path $script:LogFile -Value $clean -ErrorAction Stop } catch { }
}
function Pass([string]$step, [string]$text) { Say "[PASS] ${step}: $text" 'Green' }
function Skip([string]$step, [string]$text) { Say "[SKIP] ${step}: $text" 'Cyan' }
function Info([string]$text) { Say "[INFO] $text" 'Gray' }

# Removes the payload from the device if one may be there, then stops the run.
function Stop-Step([string]$step, [string]$problem, [string]$next) {
    Say "[FAIL] ${step}: $problem" 'Red'
    if ($next) { Say "       Next: $next" 'Yellow' }
    if ($script:PayloadSent -and $script:Serial) {
        $null = Invoke-Adb @('shell', "rm -f $ProvisionFile") 5
        Say '       The provisioning file was removed from the device.' 'Gray'
    }
    Say "       Transcript: $($script:LogFile)" 'Gray'
    exit 1
}

# ---------------------------------------------------------------------------
# Running adb, with a deadline on every call so a vanished device cannot hang.
# ---------------------------------------------------------------------------

function ConvertTo-Argument([string]$value) {
    if ($value -eq '') { return '""' }
    if ($value -notmatch '[\s"]') { return $value }
    return '"' + $value.Replace('"', '\"') + '"'
}

function Invoke-Process([string]$file, [string[]]$arguments, [int]$timeoutSec) {
    $info = New-Object System.Diagnostics.ProcessStartInfo
    $info.FileName = $file
    $info.Arguments = ($arguments | ForEach-Object { ConvertTo-Argument $_ }) -join ' '
    $info.UseShellExecute = $false
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $info.CreateNoWindow = $true
    $process = [System.Diagnostics.Process]::Start($info)
    $out = $process.StandardOutput.ReadToEndAsync()
    $err = $process.StandardError.ReadToEndAsync()
    if (-not $process.WaitForExit($timeoutSec * 1000)) {
        # /T takes the whole tree: a .cmd shim starts a child of its own.
        $null = Start-Process -FilePath 'taskkill' -ArgumentList "/F /T /PID $($process.Id)" -NoNewWindow -Wait
        return [pscustomobject]@{ Code = -1; Lines = @(); Error = "timed out after $timeoutSec s"; TimedOut = $true }
    }
    $process.WaitForExit()
    $lines = @($out.Result -split "`r?`n" | Where-Object { $_ -ne '' })
    return [pscustomobject]@{ Code = $process.ExitCode; Lines = $lines; Error = $err.Result.Trim(); TimedOut = $false }
}

function Invoke-Adb([string[]]$AdbArgs, [int]$TimeoutSec = $AdbTimeoutSec) {
    $all = @()
    if ($script:Serial) { $all += @('-s', $script:Serial) }
    $all += $AdbArgs
    return Invoke-Process $script:AdbPath $all $TimeoutSec
}

# Text for a failed call: what adb said, or that it timed out.
function Describe([object]$result) {
    if ($result.TimedOut) { return $result.Error }
    $said = (@($result.Error) + $result.Lines | Where-Object { $_ }) -join ' '
    if ($said) { return $said }
    return "exit code $($result.Code)"
}

# A shell command that has to succeed for the step to mean anything.
function Invoke-Shell([string]$step, [string]$command, [int]$TimeoutSec = $AdbTimeoutSec) {
    $result = Invoke-Adb @('shell', $command) $TimeoutSec
    if ($result.Code -ne 0) {
        Stop-Step $step "'$command' failed: $(Describe $result)" 'Check the USB cable and that the Spot is still connected, then run this again.'
    }
    return $result
}

# ---------------------------------------------------------------------------
# Checks that need no device. Nothing on the Spot changes before these pass.
# ---------------------------------------------------------------------------

Say "spotdash provision, $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" 'White'

$adbCommand = Get-Command $Adb -ErrorAction SilentlyContinue
if (-not $adbCommand) {
    Stop-Step 'adb' "adb was not found ($Adb)." 'Install Android platform-tools (https://developer.android.com/tools/releases/platform-tools) and put it on PATH, or pass -Adb <path>.'
}
$script:AdbPath = $adbCommand.Source
Pass 'adb' $script:AdbPath

$parent = Split-Path -Parent $scriptDir
if (-not $Apk) {
    $found = @(Get-ChildItem -Path (Join-Path $parent 'shell') -Filter 'spotdash-shell-*.apk' -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending)
    if ($found.Count -gt 0) { $Apk = $found[0].FullName }
    else { $Apk = Join-Path $parent 'shell\app\build\outputs\apk\debug\app-debug.apk' }
}
if (-not (Test-Path -LiteralPath $Apk)) {
    Stop-Step 'APK' "No APK at $Apk." 'Pass -Apk <path>, or build one with gradlew assembleDebug in shell\.'
}
$Apk = (Resolve-Path -LiteralPath $Apk).Path
Pass 'APK' $Apk

if (-not $Exe) {
    foreach ($candidate in @((Join-Path $parent 'spotdash.exe'), (Join-Path $parent 'agent\spotdash.exe'))) {
        if (Test-Path -LiteralPath $candidate) { $Exe = $candidate; break }
    }
}
if (-not $Config) {
    if (-not $Exe -or -not (Test-Path -LiteralPath $Exe)) {
        Stop-Step 'config' 'No config given and spotdash.exe was not found to ask for one.' 'Pass -Config <path to config.json>, or -Exe <path to spotdash.exe>.'
    }
    $asked = Invoke-Process $Exe @('-config-path') 15
    $Config = @($asked.Lines | Where-Object { $_.Trim() })[0]
    if ($asked.Code -ne 0 -or -not $Config) {
        Stop-Step 'config' "spotdash.exe -config-path gave no path ($(Describe $asked))." 'Run spotdash.exe once so it creates a config, or pass -Config.'
    }
}
if (-not (Test-Path -LiteralPath $Config)) {
    Stop-Step 'config' "No config at $Config." 'Run spotdash.exe once so it creates one, then run this again.'
}
try { $settings = Get-Content -Raw -LiteralPath $Config | ConvertFrom-Json }
catch { Stop-Step 'config' "$Config is not valid JSON." 'Fix the file, or point -Config at another.' }
$token = "$($settings.token)".Trim()
if (-not $token) {
    Stop-Step 'config' "$Config has no token." 'Set a token in config.json (see docs/device.md) and reload the agent.'
}
$script:Secret = $token
$port = 8765
if ($settings.listen -match ':(\d+)$') { $port = [int]$Matches[1] }
Pass 'config' "$Config (port $port)"

if (-not $Timezone) {
    if (-not $WindowsTimezone) { $WindowsTimezone = [System.TimeZoneInfo]::Local.Id }
    if (-not $TimezoneMap.ContainsKey($WindowsTimezone)) {
        Stop-Step 'timezone' "No IANA name is known for the Windows timezone '$WindowsTimezone'." 'Pass -Timezone with a name such as Europe/London.'
    }
    $Timezone = $TimezoneMap[$WindowsTimezone]
}
Pass 'timezone' $Timezone

# ---------------------------------------------------------------------------
# The device.
# ---------------------------------------------------------------------------

$list = Invoke-Adb @('devices')
$devices = @($list.Lines | Where-Object { $_ -match '^(\S+)\s+(\S+)$' -and $_ -notmatch '^List of devices' } |
    ForEach-Object { $null = $_ -match '^(\S+)\s+(\S+)$'; [pscustomobject]@{ Serial = $Matches[1]; State = $Matches[2] } })
if ($devices.Count -eq 0) {
    Stop-Step 'device' 'No device is attached.' 'Connect the Spot over USB with USB debugging on. If adb still sees nothing, set Developer options > Default USB configuration to No data transfer, switch USB debugging off and on, and replug (docs/device.md).'
}
if ($devices.Count -gt 1) {
    Stop-Step 'device' "$($devices.Count) devices are attached." 'Unplug all but the Spot, then run this again.'
}
if ($devices[0].State -in @('unauthorized', 'offline')) {
    Stop-Step 'device' "The device is $($devices[0].State)." 'Accept the USB debugging prompt on the Spot, or replug it, then run this again.'
}
$script:Serial = $devices[0].Serial
Pass 'device' 'one device attached'

$wlan = Invoke-Shell 'device address' 'ip -4 -o addr show wlan0'
if (-not (($wlan.Lines -join ' ') -match 'inet (\d+\.\d+\.\d+\.\d+)/(\d+)')) {
    Stop-Step 'device address' 'The Spot has no Wi-Fi address.' 'Join it to Wi-Fi first (docs/device.md, "Join Wi-Fi from the PC").'
}
$deviceIp = $Matches[1]
$prefix = [int]$Matches[2]

function ConvertTo-Number([string]$address) {
    $bytes = ([System.Net.IPAddress]::Parse($address)).GetAddressBytes()
    return [uint64]$bytes[0] * 16777216 + [uint64]$bytes[1] * 65536 + [uint64]$bytes[2] * 256 + [uint64]$bytes[3]
}

if (-not $AgentHost) {
    $mask = if ($prefix -eq 0) { [uint64]0 } else { [uint64](4294967296 - [math]::Pow(2, 32 - $prefix)) }
    $wanted = (ConvertTo-Number $deviceIp) -band $mask
    $onSubnet = @(Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
        Where-Object { $_.IPAddress -notmatch '^(127|169\.254)\.' -and ((ConvertTo-Number $_.IPAddress) -band $mask) -eq $wanted })
    if ($onSubnet.Count -eq 0) {
        Stop-Step 'PC address' "This PC has no address on the Spot's network ($deviceIp/$prefix): they are on different networks." 'Put both on the same Wi-Fi (not a guest network), or pass -AgentHost with the address the Spot can reach.'
    }
    $best = $onSubnet | Sort-Object { (Get-NetIPInterface -InterfaceIndex $_.InterfaceIndex -AddressFamily IPv4 -ErrorAction SilentlyContinue).InterfaceMetric } | Select-Object -First 1
    $AgentHost = $best.IPAddress
}
$agentUrl = "http://${AgentHost}:$port"
Pass 'PC address' "the Spot ($deviceIp) will reach the agent at $agentUrl"

# ---------------------------------------------------------------------------
# The steps.
# ---------------------------------------------------------------------------

# 1. adb root. Real adb restarts adbd and can exit non-zero while it does, so the
# check that counts is `id` afterwards.
$root = Invoke-Adb @('root')
$said = ($root.Lines + $root.Error) -join ' '
if ($said -match 'cannot run as root|production builds') {
    Stop-Step 'adb root' "The device would not run adbd as root ($said)." 'This step needs the LineageOS build from docs/rooting.md. A stock or production build cannot be provisioned this way.'
}
if ($root.TimedOut -or ($root.Code -ne 0 -and $said -notmatch 'restarting adbd')) {
    Stop-Step 'adb root' "adb root failed ($(Describe $root))." 'Replug the Spot, run adb kill-server, and try again.'
}
$wasRoot = $said -match 'already running as root'
if (-not $wasRoot) {
    $null = Invoke-Adb @('wait-for-device') 30
}
$isRoot = $false
for ($try = 0; $try -lt 15 -and -not $isRoot; $try++) {
    $whoami = Invoke-Adb @('shell', 'id')
    if ($whoami.Code -eq 0 -and (($whoami.Lines -join ' ') -match 'uid=0')) { $isRoot = $true } else { Start-Sleep -Seconds 1 }
}
if (-not $isRoot) {
    Stop-Step 'adb root' 'adbd did not come back as root.' 'Replug the Spot, run adb kill-server, and try again.'
}
if ($wasRoot) { Skip 'adb root' 'adbd is already running as root' } else { Pass 'adb root' 'adbd restarted as root' }

# 2. Install, unless the installed APK is byte for byte this one.
$apkHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $Apk).Hash.ToLower()
$installedPath = Invoke-Adb @('shell', "pm path $Package")
$current = $null
if ($installedPath.Code -eq 0 -and (($installedPath.Lines -join ' ') -match 'package:(\S+)')) {
    $sum = Invoke-Adb @('shell', "sha256sum $($Matches[1])")
    if ($sum.Code -eq 0 -and (($sum.Lines -join ' ') -match '\b([0-9a-f]{64})\b')) { $current = $Matches[1] }
}
if ($current -eq $apkHash) {
    Skip 'install APK' 'the installed copy is already this APK'
}
else {
    $installed = Invoke-Adb @('install', '-r', $Apk) $InstallTimeoutSec
    if ($installed.Code -ne 0 -or (($installed.Lines -join ' ') -notmatch 'Success')) {
        Stop-Step 'install APK' "adb install failed: $(Describe $installed)" 'Check the APK is signed for this device and that the Spot has free storage, then run this again.'
    }
    Pass 'install APK' 'installed'
}

# 3. Start the shell so it creates the folder the payload goes in.
$started = Invoke-Adb @('shell', "am start -n $Activity")
if ($started.Code -ne 0 -or (($started.Lines -join ' ') -match 'Error')) {
    Stop-Step 'start shell' "The shell would not start ($(Describe $started))." 'Check that the APK installed, then run this again.'
}
$ready = $false
for ($try = 0; $try -lt 10 -and -not $ready; $try++) {
    $probe = Invoke-Adb @('shell', "ls -d $FilesDir")
    if ($probe.Code -eq 0) { $ready = $true } else { Start-Sleep -Milliseconds 700 }
}
if (-not $ready) {
    Stop-Step 'start shell' "The shell did not create $FilesDir." 'Open the shell on the Spot once, then run this again.'
}
Pass 'start shell' 'running, and its folder exists'

# 4. Deliver the address and token. The file holds the token, so it exists on
# the PC only for the length of the push.
$null = Invoke-Adb @('logcat', '-c')
$payloadFile = Join-Path ([System.IO.Path]::GetTempPath()) ("provision-{0}.json" -f [guid]::NewGuid().ToString('N'))
$json = ([ordered]@{ version = 1; agent_url = $agentUrl; token = $token } | ConvertTo-Json -Compress)
[System.IO.File]::WriteAllText($payloadFile, $json, (New-Object System.Text.UTF8Encoding($false)))
$script:PayloadSent = $true
try { $pushed = Invoke-Adb @('push', $payloadFile, $ProvisionFile) }
finally { Remove-Item -LiteralPath $payloadFile -Force -ErrorAction SilentlyContinue }
if ($pushed.Code -ne 0) {
    Stop-Step 'deliver payload' "adb push failed: $(Describe $pushed)" 'Check the Spot is still connected and adb is running as root, then run this again.'
}
$null = Invoke-Adb @('shell', "am start -n $Activity")
$applied = $false
$deadline = (Get-Date).AddSeconds(15)
while (-not $applied -and (Get-Date) -lt $deadline) {
    $log = Invoke-Adb @('logcat', '-d', '-s', 'spotdash')
    if ($log.Code -ne 0) { Stop-Step 'deliver payload' "Could not read the shell log: $(Describe $log)" 'Check the USB cable, then run this again.' }
    $text = $log.Lines -join "`n"
    if ($text -match 'provisioning ignored: ([^\n]*)') {
        Stop-Step 'deliver payload' "The shell refused the settings: provisioning ignored: $($Matches[1])" 'The agent address or token in the config is not acceptable to the shell; fix it and run this again.'
    }
    if ($text -match 'provisioning applied for ([^\s]+)') { $applied = $true; $appliedHost = $Matches[1] }
    else { Start-Sleep -Milliseconds 700 }
}
if (-not $applied) {
    Stop-Step 'deliver payload' 'The shell did not report applying the settings.' 'Run adb logcat -d -s spotdash to see why, then run this again.'
}
$script:PayloadSent = $false
Pass 'deliver payload' "applied for $appliedHost"

# 5. Home activity
$homeResult = Invoke-Shell 'home activity' 'cmd package resolve-activity --brief -a android.intent.action.MAIN -c android.intent.category.HOME'
if (($homeResult.Lines | Select-Object -Last 1) -eq $Activity) {
    Skip 'home activity' 'the shell is already the home activity'
}
else {
    $null = Invoke-Shell 'home activity' "cmd package set-home-activity $Activity"
    Pass 'home activity' 'the shell is now the home activity'
}

# 6. WRITE_SETTINGS
$appop = Invoke-Shell 'WRITE_SETTINGS' "appops get $Package WRITE_SETTINGS"
if (($appop.Lines -join ' ') -match 'WRITE_SETTINGS: allow') {
    Skip 'WRITE_SETTINGS' 'already allowed'
}
else {
    $null = Invoke-Shell 'WRITE_SETTINGS' "appops set $Package WRITE_SETTINGS allow"
    Pass 'WRITE_SETTINGS' 'allowed'
}

# 7. Timezone
$currentZone = ((Invoke-Shell 'timezone' 'getprop persist.sys.timezone').Lines -join '').Trim()
if ($currentZone -eq $Timezone) {
    Skip 'timezone' "already $Timezone"
}
else {
    $null = Invoke-Shell 'timezone' "setprop persist.sys.timezone $Timezone"
    Pass 'timezone' "set to $Timezone"
}

# 8. Brightness
$mode = ((Invoke-Shell 'brightness' 'settings get system screen_brightness_mode').Lines -join '').Trim()
$level = ((Invoke-Shell 'brightness' 'settings get system screen_brightness').Lines -join '').Trim()
if ($mode -eq '0' -and $level -eq '255') {
    Skip 'brightness' 'already manual at 255'
}
else {
    $null = Invoke-Shell 'brightness' 'settings put system screen_brightness_mode 0'
    $null = Invoke-Shell 'brightness' 'settings put system screen_brightness 255'
    Pass 'brightness' 'manual at 255'
}

# 9. Firewall: inbound TCP on the agent port, from the Spot only.
if ($SkipFirewall) {
    Skip 'firewall' 'skipped as asked'
}
else {
    $rule = @{ DisplayName = $FirewallRule; Direction = 'Inbound'; Action = 'Allow'; Protocol = 'TCP'; LocalPort = $port; RemoteAddress = $deviceIp; Profile = 'Private' }
    $ruleCommand = "New-NetFirewallRule -DisplayName '$FirewallRule' -Direction Inbound -Action Allow -Protocol TCP -LocalPort $port -RemoteAddress $deviceIp -Profile Private"
    if ($Exe -and (Test-Path -LiteralPath $Exe)) {
        $rule.Program = (Resolve-Path -LiteralPath $Exe).Path
        $ruleCommand += " -Program '$($rule.Program)'"
    }
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $elevated = (New-Object Security.Principal.WindowsPrincipal $identity).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $elevated) {
        Stop-Step 'firewall' 'Adding the rule needs an elevated PowerShell.' "Run this in an elevated PowerShell, or run the command below there once and pass -SkipFirewall here:`n       $ruleCommand"
    }
    $existing = Get-NetFirewallRule -DisplayName $FirewallRule -ErrorAction SilentlyContinue
    $same = $false
    if ($existing) {
        $ports = @($existing | Get-NetFirewallPortFilter | ForEach-Object { $_.LocalPort })
        $remotes = @($existing | Get-NetFirewallAddressFilter | ForEach-Object { $_.RemoteAddress })
        $same = ($ports -contains "$port") -and ($remotes -contains $deviceIp) -and ($existing.Enabled -eq 'True')
    }
    if ($same) {
        Skip 'firewall' "the rule already allows $deviceIp on port $port"
    }
    else {
        if ($existing) { Remove-NetFirewallRule -DisplayName $FirewallRule }
        New-NetFirewallRule @rule | Out-Null
        Pass 'firewall' "allowed $deviceIp to reach port $port"
    }
    $owner = Get-NetIPAddress -IPAddress $AgentHost -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($owner) {
        $netProfile = Get-NetConnectionProfile -InterfaceIndex $owner.InterfaceIndex -ErrorAction SilentlyContinue
        if ($netProfile -and $netProfile.NetworkCategory -ne 'Private') {
            Info "This PC's network is $($netProfile.NetworkCategory), and the rule applies to Private networks only. Set the network to Private if the panel does not load."
        }
    }
}

# 10. Restart the shell and wait for the panel.
$null = Invoke-Adb @('shell', "am force-stop $Package")
$null = Invoke-Adb @('logcat', '-c')
$restart = Invoke-Adb @('shell', "am start -n $Activity")
if ($restart.Code -ne 0) { Stop-Step 'panel' "The shell would not restart ($(Describe $restart))." 'Run this again.' }
$loaded = $false
$deadline = (Get-Date).AddSeconds($PanelWaitSec)
while (-not $loaded -and (Get-Date) -lt $deadline) {
    $log = Invoke-Adb @('logcat', '-d', '-s', 'spotdash')
    if ($log.Code -ne 0) { Stop-Step 'panel' "Could not read the shell log: $(Describe $log)" 'Check the USB cable, then run this again.' }
    $text = $log.Lines -join "`n"
    if ($text -match 'page load failed: ([^\n]*)') {
        Stop-Step 'panel' "page load failed: $($Matches[1])" "Check the agent is running (its tray icon), that Windows Firewall allows $deviceIp to port $port, and that the token matches."
    }
    if ($text -match 'page loaded') { $loaded = $true } else { Start-Sleep -Milliseconds 700 }
}
if (-not $loaded) {
    Stop-Step 'panel' "The panel did not load within $PanelWaitSec s." 'Check the agent is running and reachable from the Spot, then run this again.'
}
Pass 'panel' 'panel loaded on the Echo Spot: the dash is on screen'
Say "Transcript: $($script:LogFile)" 'Gray'
exit 0
