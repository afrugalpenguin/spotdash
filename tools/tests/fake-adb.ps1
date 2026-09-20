# A stand-in for adb, used only by test-spot-backup.ps1.
#
# It answers exactly the commands spot-backup.ps1 is allowed to send and refuses
# everything else with a non-zero exit, so a script that starts sending a
# command that changes the device fails its tests rather than passing quietly.
# Every call is appended to FAKE_ADB_LOG, so a test can also assert what was
# never called.
#
# The device is a directory (FAKE_ADB_DIR) of small files named after the block
# device they stand in for, such as mmcblk0p3.bin. Behaviour switches, all
# optional environment variables:
#   FAKE_ADB_MODE     normal (default) | none | two | unauthorized
#   FAKE_ADB_STATE    recovery (default) | device
#   FAKE_ADB_MISSING  a partition name to leave out of the by-name map
#   FAKE_ADB_LIE_HASH a block device name whose on-device sha256 comes back wrong
#   FAKE_ADB_BAD_PULL a block device name whose pulled copy gets an extra byte
#   FAKE_ADB_NO_SHA   set to 1 for a device with no sha256sum at all
#   FAKE_ADB_BLOCKS   a block device name whose /proc/partitions size is one block too big

$ErrorActionPreference = 'Stop'

$dataDir = $env:FAKE_ADB_DIR
$mode = if ($env:FAKE_ADB_MODE) { $env:FAKE_ADB_MODE } else { 'normal' }
$state = if ($env:FAKE_ADB_STATE) { $env:FAKE_ADB_STATE } else { 'recovery' }

if ($env:FAKE_ADB_LOG) {
    Add-Content -Path $env:FAKE_ADB_LOG -Value ($args -join ' ')
}

# Drop "-s <serial>", which adb takes before the command.
$rest = @($args)
if ($rest.Count -ge 2 -and $rest[0] -eq '-s') {
    $rest = $rest[2..($rest.Count - 1)]
}

function Refuse($what) {
    [Console]::Error.WriteLine("fake-adb: not a command the backup script may send: $what")
    exit 97
}

# name -> block device, in the order a real by-name directory lists them.
$byName = [ordered]@{
    kb = 'mmcblk0p1'; dkb = 'mmcblk0p2'; lk_real = 'mmcblk0p3'
    tee1_real = 'mmcblk0p4'; tee2_real = 'mmcblk0p5'; logo = 'mmcblk0p6'
    MISC = 'mmcblk0p7'; boot = 'mmcblk0p8'; recovery = 'mmcblk0p9'
    system = 'mmcblk0p10'; userdata = 'mmcblk0p11'
}
if ($env:FAKE_ADB_MISSING) { $byName.Remove($env:FAKE_ADB_MISSING) }

function Blocks($device) {
    $file = Join-Path $dataDir "$device.bin"
    if (Test-Path $file) {
        $count = [int]((Get-Item $file).Length / 1024)
        if ($env:FAKE_ADB_BLOCKS -eq $device) { $count++ }
        return $count
    }
    return 0
}

function DeviceFor($path) { return ($path -split '/')[-1] }

$command = $rest[0]
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
    'get-state' {
        $state
        exit 0
    }
    'pull' {
        $remote = $rest[1]
        $local = $rest[2]
        $device = DeviceFor $remote
        $source = Join-Path $dataDir "$device.bin"
        if (-not (Test-Path $source)) {
            [Console]::Error.WriteLine("adb: error: remote object '$remote' does not exist")
            exit 1
        }
        Copy-Item -LiteralPath $source -Destination $local -Force
        if ($env:FAKE_ADB_BAD_PULL -eq $device) {
            $stream = [System.IO.File]::Open($local, 'Append')
            $stream.WriteByte(0x58)
            $stream.Close()
        }
        "$remote`: 1 file pulled."
        exit 0
    }
    'shell' {
        $line = ($rest[1..($rest.Count - 1)]) -join ' '
        switch -Regex ($line) {
            '^ls -d /dev/block/platform/\*/by-name$' { '/dev/block/platform/soc/by-name'; exit 0 }
            '^ls -l /dev/block/platform/soc/by-name/?$' {
                foreach ($name in $byName.Keys) {
                    "lrwxrwxrwx 1 root root 20 1970-01-01 00:00 $name -> /dev/block/$($byName[$name])"
                }
                exit 0
            }
            '^cat /proc/partitions$' {
                'major minor  #blocks  name'
                ''
                "   179        0    $(Blocks 'mmcblk0') mmcblk0"
                foreach ($device in @($byName.Values) + 'mmcblk0boot0', 'mmcblk0boot1') {
                    "   179        1       $(Blocks $device) $device"
                }
                exit 0
            }
            '^(toybox |busybox )?sha256sum (\S+)$' {
                if ($env:FAKE_ADB_NO_SHA -eq '1') {
                    [Console]::Error.WriteLine('sha256sum: not found')
                    exit 127
                }
                $target = $Matches[2]
                if ($target -eq '/dev/null') {
                    'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  /dev/null'
                    exit 0
                }
                $device = DeviceFor $target
                $file = Join-Path $dataDir "$device.bin"
                if (-not (Test-Path $file)) {
                    [Console]::Error.WriteLine("sha256sum: $target`: No such file or directory")
                    exit 1
                }
                $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $file).Hash.ToLower()
                if ($env:FAKE_ADB_LIE_HASH -eq $device) { $hash = ('0' * 64) }
                "$hash  $target"
                exit 0
            }
            '^getprop ro\.product\.device$' { 'rook'; exit 0 }
            default { Refuse "shell $line" }
        }
    }
    default { Refuse ($rest -join ' ') }
}
