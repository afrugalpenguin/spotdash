package dev.spotdash.shell

import android.app.Activity
import android.content.Context
import android.os.Build
import android.os.PowerManager
import android.provider.Settings as AndroidSettings
import android.util.Log
import android.view.WindowManager
import android.webkit.JavascriptInterface

internal const val TAG = "spotdash"

/**
 * The `window.shell` bridge.
 *
 * Exactly four methods, and every one of them is optional at runtime. The Echo
 * Spot grants some of this and an emulator grants none of it, and the panel has
 * to behave identically either way, so nothing here throws: a method that
 * cannot do its job logs why and returns.
 *
 * Every method hops to the main thread. WebView calls these on its own JavaScript
 * thread, and touching a window from there is a crash.
 */
class ShellBridge(private val activity: Activity) {

    private val powerManager by lazy {
        activity.getSystemService(Context.POWER_SERVICE) as PowerManager
    }

    private var wakeLock: PowerManager.WakeLock? = null

    /** Panel brightness, 0 to 255. */
    @JavascriptInterface
    fun setBrightness(level: Int) {
        val clamped = level.coerceIn(0, 255)

        if (!canWriteSystemSettings()) {
            Log.i(
                TAG,
                "setBrightness($clamped) ignored: WRITE_SETTINGS is not granted. " +
                    "Grant it with: adb shell pm grant ${activity.packageName} android.permission.WRITE_SETTINGS",
            )
            return
        }

        activity.runOnUiThread {
            try {
                AndroidSettings.System.putInt(
                    activity.contentResolver,
                    AndroidSettings.System.SCREEN_BRIGHTNESS_MODE,
                    AndroidSettings.System.SCREEN_BRIGHTNESS_MODE_MANUAL,
                )
                AndroidSettings.System.putInt(
                    activity.contentResolver,
                    AndroidSettings.System.SCREEN_BRIGHTNESS,
                    clamped,
                )
                // Also set it on this window, so the change is visible at once
                // rather than at the next system brightness evaluation.
                val params = activity.window.attributes
                params.screenBrightness = clamped / 255f
                activity.window.attributes = params
                Log.d(TAG, "setBrightness($clamped)")
            } catch (error: Exception) {
                Log.w(TAG, "setBrightness($clamped) failed", error)
            }
        }
    }

    /** Blank the panel. */
    @JavascriptInterface
    fun screenOff() {
        activity.runOnUiThread {
            val params = activity.window.attributes
            // A brightness of zero is the portable way to blank a kiosk panel.
            // Actually powering the display down needs DEVICE_ADMIN, which is a
            // heavier grant than this is worth and is not available on an
            // emulator at all.
            params.screenBrightness = 0f
            activity.window.attributes = params
            activity.window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            Log.d(TAG, "screenOff")
        }
    }

    /** Wake the panel. */
    @JavascriptInterface
    fun screenOn() {
        activity.runOnUiThread {
            val params = activity.window.attributes
            params.screenBrightness = WindowManager.LayoutParams.BRIGHTNESS_OVERRIDE_NONE
            activity.window.attributes = params
            activity.window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            Log.d(TAG, "screenOn")
        }
    }

    /** Hold or release the wake lock. */
    @JavascriptInterface
    fun keepAwake(awake: Boolean) {
        activity.runOnUiThread {
            if (awake) {
                activity.window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                if (wakeLock == null) {
                    @Suppress("DEPRECATION")
                    wakeLock = powerManager.newWakeLock(
                        PowerManager.SCREEN_BRIGHT_WAKE_LOCK,
                        "spotdash:panel",
                    )
                }
                val lock = wakeLock
                if (lock != null && !lock.isHeld) {
                    try {
                        lock.acquire()
                    } catch (error: SecurityException) {
                        Log.i(TAG, "keepAwake(true): WAKE_LOCK is not granted, relying on the window flag", error)
                    }
                }
            } else {
                activity.window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                wakeLock?.takeIf { it.isHeld }?.release()
            }
            Log.d(TAG, "keepAwake($awake)")
        }
    }

    /** Release anything held, on the way out. */
    fun release() {
        wakeLock?.takeIf { it.isHeld }?.release()
        wakeLock = null
    }

    private fun canWriteSystemSettings(): Boolean {
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            AndroidSettings.System.canWrite(activity)
        } else {
            true
        }
    }
}
