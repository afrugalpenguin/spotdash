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

/** The `window.shell` bridge. A method that cannot act logs why and returns. See docs/architecture.md, "JavaScript bridge". */
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
                    "Grant it with: adb shell appops set ${activity.packageName} WRITE_SETTINGS allow",
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
                // Also set on this window so the change shows at once.
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
            // Zero brightness blanks the panel. See docs/architecture.md,
            // "JavaScript bridge".
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
