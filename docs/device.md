# Bringing up the Echo Spot

Everything in phase 1 was built and verified against an emulator matching the
device: Android 11, API 30, 480x480, circular, 1 GB. This is the list of things
that could only be settled with the hardware present, and the order to do them
in.

Work through it with the Spot on the desk and a USB cable attached. Nothing here
needs the agent to be rebuilt.

## 0. Get LineageOS onto the device

There's no official LineageOS build for the Echo Spot (`rook`); this whole
thing rides on unofficial community work. Two XDA threads cover it:

- [\[UNLOCK\]\[ROOT\]\[TWRP\]\[UNBRICK\] Amazon Echo Spot 2017 (rook)](https://xdaforums.com/t/unlock-root-twrp-unbrick-amazon-echo-spot-2017-rook.4754878/) -
  unlocking the bootloader and getting TWRP recovery on, which has to happen
  first.
- [\[ROM\]\[UNOFFICIAL\]\[11\]\[rook\] LineageOS 18.1 for the Amazon Echo Spot (2017)](https://xdaforums.com/t/rom-unofficial-11-rook-lineageos-18-1-for-the-amazon-echo-spot-2017.4762459/) -
  the actual ROM, flashed through the TWRP from the step above.

Not maintaining flashing steps here since that thread is the actual source of
truth and moves independently of this repo. Known rough edges from that
build worth knowing before you start: WPA3 wifi is unsupported (WPA2 only),
and speaker quality, Bluetooth, camera, mic, and sensors are all flagged
experimental. spotdash only touches the display and network, so the rest
doesn't matter here, but it's why this whole device is "unofficial" territory
rather than a supported LineageOS target.

Once LineageOS 18.1 is on and booted, enable Developer Options (tap the
build number seven times in Settings > About) and turn on USB debugging.

## Before you start

- The Spot is on LineageOS 18.1 and has developer options enabled, with USB
  debugging on.
- The desktop running the agent and the Spot are on the same network.
- `adb devices` lists the Spot.

## 1. Find the agent's LAN address

The emulator reaches the host at `10.0.2.2`, which is a special address only the
emulator understands. The real device needs the desktop's actual LAN address.

```powershell
ipconfig | Select-String IPv4
```

Check the agent is reachable from somewhere other than the desktop itself before
going near the Spot, because `/health` needs no token:

```powershell
curl http://<desktop-ip>:8765/health
```

If that fails from another machine but works locally, it is the Windows firewall
rather than the agent. The agent listens on `0.0.0.0` by default.

## 2. Install the shell

```powershell
cd shell
.\gradlew assembleDebug
adb install -r app\build\outputs\apk\debug\app-debug.apk
```

The APK contains no native code, so the same build that ran on the x86_64
emulator runs on the Spot's ARM chip unchanged. Confirm with:

```powershell
adb shell getprop ro.product.cpu.abi
```

## 3. Configure it

Start the shell, then press and hold the display for three seconds. Enter the
agent URL as `http://<desktop-ip>:8765` and the token from `config.json`.

Typing a long token on a 480px circle is unpleasant. If the on-screen keyboard
gets in the way, the emulator trick works here too: disable the IME, type
through `adb`, then re-enable it.

```powershell
adb shell ime disable com.android.inputmethod.latin/.LatinIME
adb shell input text "http://192.168.1.50:8765"
adb shell ime enable com.android.inputmethod.latin/.LatinIME
```

## 4. Check the scale

This is the one most likely to need attention. The panel is a fixed 480 CSS
pixel layout, and the shell scales it using the display's real width, so it
should fit whatever density the Spot reports. Confirm rather than assume:

```powershell
adb shell wm size
adb shell wm density
adb logcat -d | Select-String "panel scale"
```

The log line reports the scale it chose and the width it chose it from. If the
panel is clipped or letterboxed, that line is where to look first.

## 5. Make it the launcher

```powershell
adb shell cmd package set-home-activity dev.spotdash.shell/.PanelActivity
```

or set it through Settings, Apps, Default apps, Home app.

Reboot and confirm the panel comes back on its own with nothing plugged in. That
is the real test: the point of the HOME intent filter is that the device is
useful after a power cut without anyone touching it.

If the shell ever crashes on start once it is the default launcher, the device
has no launcher to fall back to. Recovery is `adb uninstall dev.spotdash.shell`,
so keep debugging enabled until you have watched it survive a few reboots.

## 6. Grant brightness control

`window.shell.setBrightness()` is a no-op without this, and logs that it is. It
cannot be granted by a prompt:

```powershell
adb shell pm grant dev.spotdash.shell android.permission.WRITE_SETTINGS
```

Then check it took effect:

```powershell
adb logcat -c
adb shell am start -n dev.spotdash.shell/.PanelActivity
adb logcat -d | Select-String "setBrightness"
```

A line saying it was ignored means the grant did not apply. Silence means it
worked.

## 7. Check the clock

Known issue 12. The status face shows how long ago each source last updated, and
it calculates that as the device's clock minus the agent's timestamp. The Spot
has no battery backed real time clock, so after a power cut it comes up with
whatever LineageOS decides until NTP settles.

```powershell
adb shell date
```

Compare with the desktop. If they disagree by more than a second or two, source
ages on the status face will be wrong by exactly that much, and the fix is the
one described in issue 12: have the agent report its own time and let the panel
correct for the offset.

## 8. Sleep window

Set `sleep_start` and `sleep_end` in `config.json`, reload from the tray, and
wait for the window to open. The panel should go to true black with a single dim
dot rather than looking switched off.

Worth doing once at the real time rather than by moving the clock forward, since
what you are judging is whether a dark room at 23:30 is comfortable.

## What is likely to need a change

In rough order of likelihood:

1. **Type sizes.** Everything was sized for a 60cm viewing distance on a
   simulated panel. The real thing is the only way to judge it, and the sizes
   are all in `agent/web/style.css`.
2. **Clock skew**, if the Spot's clock drifts. Issue 12.
3. **Brightness at night**, if the panel is too bright even dimmed. The bridge
   can set it, so the clock source could drive it from the sleep window.
4. **Touch accuracy** on the tap zones, which are half the panel each and should
   be forgiving, but the Spot's digitiser is not the emulator's.
