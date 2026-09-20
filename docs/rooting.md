# From a stock Echo Spot to LineageOS

This guide takes a 2017 Echo Spot from stock to LineageOS 18.1, ready for the rest of [`docs/device.md`](device.md). It is a walkthrough of other people's work. The bootloader unlock and the ROM are theirs (credits at the end), and the risk of bricking the device sits with whoever runs them.

Two kinds of fact appear here, and each section says which it uses. "Upstream" means taken from the first post of the upstream thread as it stood on 20 September 2026. Those threads are the authority and may change, so read the relevant thread before starting. "Confirmed" means done on real hardware on 20 September 2026, on Windows 11, with amonet-rook v2.0.0 and ROM release v0.3. Anything in neither group is unknown, and the text says so.

- Unlock thread: [Amazon Echo Spot 2017 (rook), unlock, root, TWRP, unbrick](https://xdaforums.com/t/unlock-root-twrp-unbrick-amazon-echo-spot-2017-rook.4754878/)
- ROM thread: [LineageOS 18.1 for the Amazon Echo Spot (2017)](https://xdaforums.com/t/rom-unofficial-11-rook-lineageos-18-1-for-the-amazon-echo-spot-2017.4762459/)

## Before you start

This section is from the upstream threads, with two notes confirmed on hardware.

The procedure is for one device only: the 2017 Echo Spot, model VN94DQ, codename rook. It does not apply to any other Echo.

Most units have no bootrom download access, so a failed unlock can leave the device permanently dead. Upstream describes a recovery route for a bricked unit that needs Linux, disassembly and shorting a test point. This guide does not cover it, and the thread does. If you follow it, check the board photo yourself: the upstream text calls the test point TP30 in one step and TM18 in the next.

Keep the Spot unregistered and off Wi-Fi until the flash is done. It was kept that way throughout the confirmed run. Upstream states no firmware limit, and which FireOS version the confirmed device was on is unknown.

You need a Windows PC (Linux also works upstream, and was not tried), a micro USB cable, the Spot's AC adapter, an XDA account to download the unlock package, and [Android platform-tools](https://developer.android.com/tools/releases/platform-tools) for `adb`.

## Get the files

From the upstream threads. Download `amonet-rook-v2.0.0.zip`, about 40.7 MB, from the attachments on the first post of the unlock thread. Downloading needs an XDA account, and the package is not mirrored here. Check the thread for a newer version before using this one.

For the ROM, use the [release page](https://github.com/amazon-oss/releases/releases/tag/lineage-18.1-rook-v0.3) of the release used here, or check the [list of all releases](https://github.com/amazon-oss/releases/releases) for a newer one.

## Unlock and install TWRP

The steps are from the upstream thread. The notes on the port, the timing and the first TWRP prompt were confirmed on hardware.

Upstream offers two routes. Option 1 is for a working device, runs on Windows or Linux and needs only a micro USB cable. It is the one described here. Option 2 is for a bricked device.

On Windows, first install the [Kindle Fire USB driver](https://developer.amazon.com/docs/fire-tablets/connecting-adb-to-device.html#install_usb_driver). Google's [USB driver](https://developer.android.com/studio/run/win-usb) is the fallback. Optionally, and untested, put the Spot into fastboot mode with the three-button hold below and run the `fastboot` binary shipped in the zip:

```bat
fastboot devices
```

If it lists the device, the driver works.

Extract the zip. With the Spot on its AC adapter, hold all three buttons on top until the screen shows `=> FASTBOOT mode...`. The micro USB port is on the back under a small plastic cover, and no disassembly was needed. Connect the cable. In the extracted folder run `fastbrick.bat` from cmd:

```bat
fastbrick.bat
```

The script detects the device. Type `YES` when it asks. There is then a 10 second grace period, and after it any interruption bricks the device, so leave the cable, the power and the PC alone. Allow up to five minutes. The device reboots into TWRP. On the confirmed run this finished inside the five minutes.

TWRP's first prompt asks whether to keep the system partition read only. Choose Keep Read Only, as on the confirmed run. It keeps the backup pristine and avoids a dm-verity boot loop on stock. Then check that the PC sees the device:

```bat
adb devices
```

The serial should appear with the state `recovery`.

A device that is already unlocked and only needs a newer amonet flashes the new zip from TWRP.

## Partitions never to touch

From the upstream thread, with one note confirmed on hardware.

Never modify the Preloader, LK or TEE/TZ partitions. Flash stock firmware only through TWRP: the stock packages are `.bin` files that can be renamed to `.zip`. From v2.0.0 there is also an insecure preloader download mode, where the screen stays black. A tool such as MTKClient may recover a device from it, and even there TEE1, LK and the Preloader must never be written.

In TWRP, the by-name entries `lk`, `tee1` and `tee2` point at `/dev/null` as write protection. The real partitions are `lk_real`, `tee1_real` and `tee2_real`.

The boot key combinations are from the upstream thread and none of them was tried on hardware. Holding only Volume Down after connecting power gives the hacked fastboot. Holding only Volume Up after connecting power gives TWRP. Holding only Mute and Volume Down while connecting power, with USB connected, gives preloader download mode. The zip also ships `boot-recovery.sh` and `boot-fastboot.sh`, which force those modes from a connected PC.

## Back up before any wipe

Confirmed on hardware. The backup is of the state after the unlock, and a pure factory image is no longer possible at that point. It is still the only way back to what the device holds now.

List the partition map yourself and do not trust the numbers below without checking them:

```bat
adb shell ls -l /dev/block/platform/mtk-msdc.0/by-name
```

On the confirmed device it read: kb p1, dkb p2, lk_real p3, tee1_real p4, logo p5, tee2_real p6, expdb p7, MISC p8, boot p9, recovery p10, system p11, cache p12 and userdata p13, plus `mmcblk0boot0` and `mmcblk0boot1`.

`adb pull` writes into the current directory of the terminal, so make a folder, change into it, and confirm the prompt shows it before pulling. `cd` on its own prints the current directory.

```bat
mkdir spot-backup
cd spot-backup
cd
adb pull /dev/block/mmcblk0p1 kb.img
adb pull /dev/block/mmcblk0p2 dkb.img
adb pull /dev/block/mmcblk0p3 lk_real.img
adb pull /dev/block/mmcblk0p4 tee1_real.img
adb pull /dev/block/mmcblk0p6 tee2_real.img
adb pull /dev/block/mmcblk0p5 logo.img
adb pull /dev/block/mmcblk0p8 MISC.img
adb pull /dev/block/mmcblk0p9 boot.img
adb pull /dev/block/mmcblk0p10 recovery.img
adb pull /dev/block/mmcblk0p11 system.img
adb pull /dev/block/mmcblk0boot0 mmcblk0boot0.img
adb pull /dev/block/mmcblk0boot1 mmcblk0boot1.img
```

Check each file two ways. The size in bytes must equal the block count in `/proc/partitions` times 1024, and the sha256 the device computes must equal the one the PC computes. `system.img` is 1,744,830,464 bytes, and the whole eMMC (`mmcblk0`) is 7,820,083,200 bytes if a full image is wanted, so check free disk space first.

```bat
adb shell cat /proc/partitions
adb shell sha256sum /dev/block/mmcblk0p11
certutil -hashfile system.img SHA256
```

Sizes matched, and the hashes matched for `system`, `boot`, `kb` and `dkb` on the confirmed run. Repeat the hash check for every image. Then copy the folder to a second place. The images hold data unique to the device, so keep them private.

## Flash the ROM

The steps are from the ROM thread. The wipe selection, the file route and the first boot were confirmed on hardware.

Upstream asks for the latest TWRP, and the ROM thread links a post in the unlock thread for it. Whether a TWRP newer than the one bundled with amonet v2.0.0 existed was not checked, so look at that post before flashing.

In TWRP choose Wipe, then Advanced Wipe. Tick Dalvik/ART Cache, System, Data and Cache, and nothing else. Afterwards `/sdcard` showed 5.1G with 9.3M used. Whether the System wipe needs the read-only mount option cleared first was not observed either way. If it refuses, go to Mount, untick the read-only option and retry.

The ROM used was `lineage-18.1-20251108-UNOFFICIAL-rook.zip`, 453 MB, with this SHA256:

```
2755428c124df88ffd3bc6a9c36db16bbe3b7e639883fd1092dc839ac5ae9d13
```

That matches the digest GitHub shows on the release asset. A newer release has a different name and hash. Take the hash from the asset's own digest or from the `.sha256sum` file beside it, and treat the one above as an example only.

Push the zip after the wipe, since it is not known whether a data wipe clears `/sdcard`. Compare the two hashes, and go on only if they are identical:

```bat
adb push lineage-18.1-20251108-UNOFFICIAL-rook.zip /sdcard/
adb shell sha256sum /sdcard/lineage-18.1-20251108-UNOFFICIAL-rook.zip
certutil -hashfile lineage-18.1-20251108-UNOFFICIAL-rook.zip SHA256
```

In TWRP choose Install, pick the zip and swipe to confirm. In this TWRP build ADB Sideload is on the Install screen and not under Advanced, but sideload itself was not tried. Patching the system image was quick and the log ended clean. Then choose Reboot, then System, and decline the offer to install the TWRP app.

The first boot shows the LineageOS animation and reaches the Trebuchet launcher within a few minutes. There is no setup wizard, by design. A one-time LineageOS Trust prompt appears after a boot and is harmless.

## What to expect

From the ROM thread as of 20 September 2026. The build is experimental. Known issues are unreliable Bluetooth, the camera, the mute (privacy) switch, apps that do not suit a round screen, and a stock keyboard that mishandles numbers. SELinux is permissive, so keep nothing sensitive on the device. The mute button doubles as the power button, deep sleep is disabled, WPA3 is not supported, and the system reports a fake battery at 100 percent. 5 GHz Wi-Fi works, and that was confirmed on hardware for 802.11ac, including on a DFS channel. Upstream recommends `scrcpy` for driving the device.

## After the first boot

Everything from here is in [`docs/device.md`](device.md) and is not repeated. Enabling USB debugging is the last line of [section 0](device.md#0-get-lineageos-onto-the-device). If `adb devices` lists nothing, see [adb shows nothing over USB](device.md#adb-shows-nothing-over-usb). Screen mirroring is under [Screen mirroring](device.md#screen-mirroring) (`scrcpy --no-audio`), joining Wi-Fi from the PC under [Join Wi-Fi from the PC](device.md#join-wi-fi-from-the-pc), the timezone under [Check the clock](device.md#7-check-the-clock), the generic Wi-Fi MAC under [Wi-Fi MAC and DHCP](device.md#wi-fi-mac-and-dhcp), and the shell under [Install the shell](device.md#2-install-the-shell).

## What was not checked

Sideload itself, returning to stock, anything on Linux, and which FireOS version the device ran are all unverified.

## Credits

The unlock is the work of Rortiz2, k4y0z and bengris32, building on the original amonet exploit by xyz. Upstream also thanks AntiEngineer for board bring-up and UART work and alextrack2013 for the patched Windows fastboot binaries. The ROM is the work of bengris32 and R0rt1z2. The risk of bricking sits with whoever runs their unlock.
