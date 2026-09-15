# Bringing up the Echo Spot

Built and verified against an emulator matching the device: Android 11, API 30, 480x480, circular, 1GB. This is what only the real hardware can settle, in order. Spot on the desk, USB attached. No agent rebuild needed.

## 0. Get LineageOS onto the device

No official build for `rook` (Echo Spot 2017). These two XDA threads:

- [\[UNLOCK\]\[ROOT\]\[TWRP\]\[UNBRICK\] Amazon Echo Spot 2017 (rook)](https://xdaforums.com/t/unlock-root-twrp-unbrick-amazon-echo-spot-2017-rook.4754878/) - bootloader unlock + TWRP, do first.
- [\[ROM\]\[UNOFFICIAL\]\[11\]\[rook\] LineageOS 18.1](https://xdaforums.com/t/rom-unofficial-11-rook-lineageos-18-1-for-the-amazon-echo-spot-2017.4762459/) - flash through TWRP.

Flashing steps live in those threads, not here. Known rough edges: WPA3 unsupported (WPA2 only); speaker, Bluetooth, camera, mic, sensors all experimental. spotdash only touches display and network (for now).

Once booted: Settings > About > tap build number 7x for Developer Options, then enable USB debugging.

## Before you start

- LineageOS 18.1, developer options + USB debugging on
- Desktop and Spot on the same network
- `adb devices` should list the Spot

## 1. Find the agent's LAN address

The emulator uses `10.0.2.2`; the real device needs the desktop's actual LAN IP.

```powershell
ipconfig | Select-String IPv4
curl http://<desktop-ip>:8765/health   # from another machine, before touching the Spot
```

Fails remotely but works locally -> Windows firewall, not the agent (it listens on `0.0.0.0` by default).

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

```powershell
adb shell ime disable com.android.inputmethod.latin/.LatinIME
adb shell input text "http://192.168.1.50:8765"
adb shell ime enable com.android.inputmethod.latin/.LatinIME
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

or Settings > Apps > Default apps > Home app. Reboot and confirm the panel comes back with nothing plugged in - the real test of the HOME intent filter.

If the shell crashes on start as default launcher, there's no fallback launcher. Recovery: `adb uninstall dev.spotdash.shell`. Keep debugging on until it survives a few reboots.

## 6. Grant brightness control

`window.shell.setBrightness()` no-ops without this and can't be prompted for:

```powershell
adb shell pm grant dev.spotdash.shell android.permission.WRITE_SETTINGS
adb logcat -c
adb shell am start -n dev.spotdash.shell/.PanelActivity
adb logcat -d | Select-String "setBrightness"
```

"Ignored" in the log -> grant didn't take. Silence -> it worked.

## 7. Check the clock

Issue 12. Status face shows source age as device clock minus agent timestamp. No battery-backed RTC, so after a power cut it's wrong until NTP settles.

```powershell
adb shell date
```

Off by more than a second or two vs the desktop -> ages will be wrong by that much; fix is issue 12 (agent reports its own time, panel corrects for the offset).

## 8. Sleep window

Set `sleep_start`/`sleep_end` in `config.json`, reload from the tray, wait for the window. Should go true black with one dim dot. Worth checking at the real time, not by moving the clock forward - you're judging comfort in a dark room.

## What is likely to need a change

1. **Type sizes** - sized for 60cm viewing distance on a simulated panel; only the real thing judges it. `agent/web/style.css`.
2. **Clock skew** if the Spot's clock drifts. Issue 12.
3. **Brightness at night** if too bright even dimmed - bridge can set it, could drive from the sleep window.
4. **Touch accuracy** on the tap zones (half the panel each) - real digitiser isn't the emulator's.
