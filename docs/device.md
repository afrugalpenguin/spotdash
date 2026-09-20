# Bringing up the Echo Spot

Written against an emulator matching the device (Android 11, API 30, 480x480, circular, 1GB), then run on a real Echo Spot 2017 (`rook`) on unofficial LineageOS 18.1: 480x480 at density 160, `ro.config.low_ram=true`. This is what only the real hardware can settle, in order. Spot on the desk, USB attached. No agent rebuild needed.

Command blocks below are for cmd on Windows unless they say PowerShell.

## 0. Get LineageOS onto the device

No official build for `rook` (Echo Spot 2017). These two XDA threads:

- [\[UNLOCK\]\[ROOT\]\[TWRP\]\[UNBRICK\] Amazon Echo Spot 2017 (rook)](https://xdaforums.com/t/unlock-root-twrp-unbrick-amazon-echo-spot-2017-rook.4754878/) - bootloader unlock + TWRP, do first.
- [\[ROM\]\[UNOFFICIAL\]\[11\]\[rook\] LineageOS 18.1](https://xdaforums.com/t/rom-unofficial-11-rook-lineageos-18-1-for-the-amazon-echo-spot-2017.4762459/) - flash through TWRP.

Flashing steps live in those threads, not here. Known rough edges: WPA3 unsupported (WPA2 only); speaker, Bluetooth, camera, mic, sensors all experimental. spotdash only touches display and network (for now).

Once booted: Settings > About > tap build number 7x for Developer Options, then enable USB debugging.

## Before you start

- LineageOS 18.1, developer options + USB debugging on
- Desktop and Spot on the same network (or see "No Wi-Fi yet" in step 1)
- `adb devices` should list the Spot
- Skip `shell\tools\avd.ps1`. It creates the emulator and has no use on real hardware.

### adb shows nothing over USB

With the default MTP configuration adb is not exposed. On Windows there is a single portable device called "Echo Spot" (VID_1949 PID_0331) and `adb devices` is empty. Fix: Developer options > Default USB configuration > No data transfer, switch USB debugging off and on, unplug and replug.

### Screen mirroring

`scrcpy` fails by default because the ROM has no opus encoder. Either skip audio or ask for aac:

```bat
scrcpy --no-audio
scrcpy --audio-codec=aac
```

### Join Wi-Fi from the PC

Typing a password on the 480px circle is painful. Needs root adb:

```bat
adb root
adb shell "cmd wifi connect-network '<ssid>' wpa2 '<password>'"
adb shell cmd wifi status
```

WPA3 is unsupported (see step 0). To see what an access point offers:

```bat
adb shell cmd wifi start-scan
adb shell cmd wifi list-scan-results
```

`PSK` in the flags is WPA2. Only `SAE` means WPA3 and the Spot won't join it. Related: issue 72 (on-device provisioning).

## 1. Find the agent's LAN address

The emulator uses `10.0.2.2`; the real device needs the desktop's actual LAN IP.

```powershell
ipconfig | Select-String IPv4
curl http://<desktop-ip>:8765/health   # from another machine, before touching the Spot
```

Fails remotely but works locally -> Windows firewall, not the agent (it listens on `0.0.0.0` by default).

### Wi-Fi MAC and DHCP

The Spot's Wi-Fi MAC is a generic chipset default, not randomised. A DHCP reservation keyed on it therefore survives reboots and forgetting the network, which is what you want for a fixed agent address. It also means two units of the same ROM may report the same address, and two devices with one MAC cannot share a LAN. Read it before adding a second:

```bat
adb shell cat /sys/class/net/wlan0/address
```

### No Wi-Fi yet

Testing over USB alone works. Forward the agent's port to the Spot and point the shell at the loopback address:

```bat
adb reverse tcp:8765 tcp:8765
```

Then use `http://127.0.0.1:8765` as the agent URL in step 3.

## 2. Install the shell

```powershell
cd shell
.\gradlew assembleDebug
adb install -r app\build\outputs\apk\debug\app-debug.apk
adb shell getprop ro.product.cpu.abi   # confirm ARM
```

No native code in the APK, so the emulator (x86_64) build runs unchanged.

## 3. Configure it

Long-press the display 3s, enter `http://<desktop-ip>:8765` and the token from `config.json`. On-screen keyboard is painful on a 480px circle; type through adb instead:

```bat
adb shell ime disable com.android.inputmethod.latin/.LatinIME
adb shell input text "http://<desktop-ip>:8765"
adb shell ime enable com.android.inputmethod.latin/.LatinIME
```

Use an alphanumeric token (letters and digits only). `adb shell input text` goes through the device shell, which mangles most punctuation, and a mangled token is a silent 401.

An open on-screen keyboard pushes the panel off-centre. Dismiss it:

```bat
adb shell input keyevent KEYCODE_BACK
```

## 4. Check the scale

Most likely to need attention. The shell scales the fixed 480 CSS px layout from the display's real width.

```powershell
adb shell wm size
adb shell wm density
adb logcat -d | Select-String "panel scale"
```

Clipped or letterboxed -> start with that log line.

## 5. Make it the launcher

```powershell
adb shell cmd package set-home-activity dev.spotdash.shell/.PanelActivity
```

or Settings > Apps > Default apps > Home app. Check which launcher won:

```bat
adb shell cmd package resolve-activity --brief -a android.intent.action.MAIN -c android.intent.category.HOME
```

The last line should be `dev.spotdash.shell/.PanelActivity`. Reboot and confirm the panel comes back with nothing plugged in - the real test of the HOME intent filter.

Keep the stock launcher installed. It is what HOME falls back to if the shell crashes on start. Recovery: `adb uninstall dev.spotdash.shell`. Keep debugging on until it survives a few reboots.

## 6. Grant brightness control

`window.shell.setBrightness()` no-ops without this and can't be prompted for. WRITE_SETTINGS is an app-op, not a runtime permission, so the runtime-permission grant command refuses it ("not a changeable permission type"). Set the app-op:

```bat
adb shell appops set dev.spotdash.shell WRITE_SETTINGS allow
adb shell appops get dev.spotdash.shell WRITE_SETTINGS
adb logcat -c
adb shell am start -n dev.spotdash.shell/.PanelActivity
adb logcat -d -s spotdash | findstr setBrightness
```

`appops get` should print `WRITE_SETTINGS: allow`. "Ignored" in the log -> it didn't take. Silence -> it worked.

## 7. Check the clock

No battery-backed RTC, so after a power cut the clock is wrong until NTP settles. Status face ages don't depend on it: `/health` carries the agent's time and the panel subtracts the difference (issue 12). Other faces count elapsed time from when data arrived, so they don't either.

```powershell
adb shell date
```

To see it work, note how far `adb shell date` is from the desktop, then check the ages on the status face read a few seconds, not that difference.

The device timezone defaults to GMT. The clock and calendar faces show text the agent has already formatted in the desktop's timezone, so this does not change what the panel shows. Set it anyway so `adb shell date` and logcat read in local time. Needs root adb (a plain shell is refused):

```bat
adb root
adb shell setprop persist.sys.timezone Europe/London
adb shell date
```

## 8. Sleep window

Set `sleep_start`/`sleep_end` in `config.json`, reload from the tray, wait for the window. Should go true black with one dim dot. Worth checking at the real time, not by moving the clock forward - you're judging comfort in a dark room.

## Troubleshooting

**Black panel.** A rejected token or an unreachable agent now shows an error screen instead of black, so check for that first (`adb logcat -d -s spotdash`). If it is still black, take the shell out of the picture and load the panel URL in the Jelly browser:

```bat
adb shell am start -a android.intent.action.VIEW -d "<url>"
```

Then inspect that page from chrome://inspect/#devices on the desktop. Chromium prints "tile memory limits exceeded" on this low_ram build. It is noise and was not the cause of the black screen.

**Trust prompt.** LineageOS shows a one-time Trust onboarding prompt after boot. It is harmless.

## What is likely to need a change

1. **Type sizes** - sized for 60cm viewing distance on a simulated panel; only the real thing judges it. `agent/web/style.css`.
2. **Brightness at night** if too bright even dimmed - bridge can set it, could drive from the sleep window.
3. **Touch accuracy** on the tap zones (half the panel each) - real digitiser isn't the emulator's.
