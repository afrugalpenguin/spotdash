# Creates and launches the 480x480 emulator the shell is developed against.
#
# The target hardware is a 1st generation Amazon Echo Spot on LineageOS 18.1:
# Android 11 (API 30), a 480x480 circular display, and about 1 GB of RAM. The
# AVD below matches all three, so layout problems show up here rather than on
# the device.
#
# Usage:
#   .\avd.ps1              create if needed, then launch
#   .\avd.ps1 -Recreate    delete and recreate first
#   .\avd.ps1 -CreateOnly  create without launching

[CmdletBinding()]
param(
    [switch]$Recreate,
    [switch]$CreateOnly
)

$ErrorActionPreference = 'Stop'

$AvdName = 'spotdash_480'
$Package = 'system-images;android-30;default;x86_64'

$sdk = $env:ANDROID_HOME
if (-not $sdk) { $sdk = $env:ANDROID_SDK_ROOT }
if (-not $sdk) { $sdk = Join-Path $env:LOCALAPPDATA 'Android\Sdk' }
if (-not (Test-Path $sdk)) {
    Write-Error "Android SDK not found. Set ANDROID_HOME."
}

$sdkmanager = Join-Path $sdk 'cmdline-tools\latest\bin\sdkmanager.bat'
$avdmanager = Join-Path $sdk 'cmdline-tools\latest\bin\avdmanager.bat'
$emulator = Join-Path $sdk 'emulator\emulator.exe'

foreach ($tool in @($sdkmanager, $avdmanager, $emulator)) {
    if (-not (Test-Path $tool)) { Write-Error "missing $tool. Install the SDK command line tools and the emulator." }
}

# The system image is about a gigabyte, so only fetch it when it is absent.
$imagePath = Join-Path $sdk 'system-images\android-30\default\x86_64'
if (-not (Test-Path $imagePath)) {
    Write-Host "Installing $Package"
    & $sdkmanager --install $Package
    if ($LASTEXITCODE -ne 0) { Write-Error "sdkmanager failed" }
}

$avdHome = if ($env:ANDROID_AVD_HOME) { $env:ANDROID_AVD_HOME } else { Join-Path $env:USERPROFILE '.android\avd' }
$avdDir = Join-Path $avdHome "$AvdName.avd"
$avdIni = Join-Path $avdHome "$AvdName.ini"

if ($Recreate -and (Test-Path $avdDir)) {
    Write-Host "Deleting the existing $AvdName"
    & $avdmanager delete avd -n $AvdName
}

if (-not (Test-Path $avdDir)) {
    Write-Host "Creating $AvdName"
    # avdmanager asks about a hardware profile on stdin and takes "no" for the
    # default. The answer is piped through cmd because a PowerShell pipeline
    # writes UTF-16 with a byte order mark, which avdmanager reads as a stray
    # character and rejects.
    $answer = "echo no | `"$avdmanager`" create avd -n $AvdName -k `"$Package`" --force"
    & cmd.exe /c $answer
    if ($LASTEXITCODE -ne 0) { Write-Error "avdmanager failed" }
}

# The generated config is a generic phone. Rewrite the parts that make it the
# panel: a square 480x480 screen, 1 GB of RAM, and no hardware the device does
# not have. Written after creation because avdmanager has no flags for most of
# these.
$config = Join-Path $avdDir 'config.ini'
$settings = [ordered]@{
    'hw.lcd.width'            = '480'
    'hw.lcd.height'           = '480'
    'hw.lcd.density'          = '240'
    # The real panel is round, and the emulator will mask the corners to match.
    # Content that strays outside the inscribed circle then disappears here
    # exactly as it would on the device.
    'hw.lcd.circular'         = 'yes'
    'hw.ramSize'              = '1024'
    'vm.heapSize'             = '128'
    'hw.keyboard'             = 'yes'
    'hw.mainKeys'             = 'no'
    'hw.dPad'                 = 'no'
    'hw.gpu.enabled'          = 'yes'
    'hw.gpu.mode'             = 'auto'
    'hw.camera.back'          = 'none'
    'hw.camera.front'         = 'none'
    'hw.audioInput'           = 'no'
    'hw.sensors.orientation'  = 'no'
    'hw.sensors.proximity'    = 'no'
    'hw.accelerometer'        = 'no'
    'disk.dataPartition.size' = '2048M'
    'skin.name'               = '480x480'
    'skin.dynamic'            = 'yes'
    'showDeviceFrame'         = 'no'
    'avd.ini.displayname'     = 'spotdash 480x480'
}

$lines = @()
if (Test-Path $config) {
    $lines = Get-Content $config | Where-Object {
        $line = $_
        -not ($settings.Keys | Where-Object { $line -like "$_=*" })
    }
}
foreach ($key in $settings.Keys) { $lines += "$key=$($settings[$key])" }
$lines | Sort-Object | Set-Content $config -Encoding ascii

Write-Host "$AvdName configured at 480x480, API 30, 1024 MB"

if ($CreateOnly) { return }

Write-Host "Launching $AvdName"
# -no-snapshot-load starts clean, which is what you want when verifying a kiosk
# launcher: a restored snapshot can be holding a stale copy of the app.
& $emulator -avd $AvdName -no-snapshot-load -no-boot-anim -gpu auto
