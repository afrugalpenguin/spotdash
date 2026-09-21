package dev.spotdash.shell

import android.annotation.SuppressLint
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.Gravity
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.view.WindowManager
import android.webkit.WebResourceError
import android.webkit.ConsoleMessage
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import java.io.File

/** The whole shell: a WebView, a native fallback screen and a settings screen behind a long press. */
class PanelActivity : AppCompatActivity() {

    private lateinit var settings: Settings
    private lateinit var root: FrameLayout
    private lateinit var webView: WebView
    private lateinit var fallback: FallbackView
    private lateinit var bridge: ShellBridge
    private lateinit var watcher: AgentWatcher

    private val main = Handler(Looper.getMainLooper())
    private var lastError: String = ""
    private var retryScheduled = false
    private val backoff = RetryBackoff()

    // WebView calls onPageFinished after a failed load too. This keeps it from
    // hiding the error. See docs/architecture.md, "Failure behaviour".
    private var pageFailed = false

    // Which side is behind, from the agent's last /health. NONE until it answers.
    private var mismatch = ProtocolMismatch.NONE

    // Set when the Wi-Fi settings were opened, so coming back retries at once
    // and skips the backoff built up while the network was down.
    private var wifiOpened = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        settings = Settings(this)
        // Asking creates the directory, so `adb push` has somewhere to write a
        // provisioning file on a fresh install.
        getExternalFilesDir(null)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        root = FrameLayout(this).apply { setBackgroundColor(Color.BLACK) }
        setContentView(root)

        // The insets controller lives on the decor view, which exists only after
        // setContentView. Asking earlier returns null and crashes on launch.
        goFullscreen()

        // Static and process wide, so it runs before the WebView exists. Debug
        // builds only: it lets anything on the USB cable inspect the panel.
        if (isDebuggable(applicationInfo.flags)) {
            WebView.setWebContentsDebuggingEnabled(true)
        }

        webView = buildWebView()
        root.addView(
            webView,
            FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT,
            ),
        )

        fallback = FallbackView(this) { openSettings() }
        root.addView(
            fallback,
            FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT,
            ),
        )
        fallback.visibility = View.GONE

        installLongPressGesture()

        watcher = AgentWatcher(
            healthUrl = { settings.healthUrl() },
            onDown = { reason -> showFallback(reason) },
            onUp = { hideFallbackAndReload() },
            onProtocol = { agent -> checkProtocol(agent) },
        )

        if (settings.isConfigured) {
            loadPanel()
        } else {
            // A fresh install has nothing configured. That is normal.
            showFallback(getString(R.string.not_configured))
        }
    }

    override fun onResume() {
        super.onResume()
        goFullscreen()
        val provisioned = applyProvisioning()
        if (settings.isConfigured) {
            watcher.start()
        }
        if (provisioned) {
            wifiOpened = false
            hideFallbackAndReload()
        } else if (wifiOpened) {
            wifiOpened = false
            if (settings.isConfigured && fallback.visibility == View.VISIBLE) {
                Log.i(TAG, "back from the wifi settings, retrying the panel")
                backoff.reset()
                loadPanel()
            }
        }
    }

    override fun onPause() {
        super.onPause()
        watcher.stop()
    }

    /** `am start` on a front activity only delivers an intent here. Look for a provisioning file again. */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        if (applyProvisioning()) {
            hideFallbackAndReload()
        }
    }

    /** Applies a provisioning file left by adb, if any. True when the panel should reload. */
    private fun applyProvisioning(): Boolean {
        val directory = getExternalFilesDir(null) ?: return false
        val consumed = consumeProvisioning(File(directory, PROVISIONING_FILE_NAME)) ?: return false

        if (!consumed.deleted) {
            Log.w(TAG, "the provisioning file could not be deleted and is still on the device")
        }
        return when (val result = consumed.result) {
            is ProvisioningResult.Rejected -> {
                Log.w(TAG, "provisioning ignored: ${result.reason}")
                false
            }
            is ProvisioningResult.Valid -> {
                if (settings.provision(result.agentUrl, result.token)) {
                    Log.i(TAG, "provisioning applied for ${provisioningHost(result.agentUrl)}")
                    true
                } else {
                    Log.w(TAG, "provisioning ignored: the settings could not be saved")
                    false
                }
            }
        }
    }

    override fun onDestroy() {
        watcher.shutdown()
        bridge.release()
        main.removeCallbacksAndMessages(null)
        webView.destroy()
        super.onDestroy()
    }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        // The system bars come back after a dialog or a notification.
        if (hasFocus) goFullscreen()
    }

    /** Back must not leave the panel. */
    @Suppress("DEPRECATION")
    override fun onBackPressed() {
        if (fallback.visibility == View.VISIBLE && settings.isConfigured) {
            hideFallbackAndReload()
            return
        }
        // No super call.
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun buildWebView(): WebView {
        val view = WebView(this)
        view.setBackgroundColor(Color.BLACK)

        view.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            useWideViewPort = true
            loadWithOverviewMode = false
            setSupportZoom(false)
            builtInZoomControls = false
            displayZoomControls = false
            // The agent is rebuilt often, and a stale panel with no address bar
            // is hard to diagnose.
            cacheMode = android.webkit.WebSettings.LOAD_NO_CACHE
            mediaPlaybackRequiresUserGesture = false
            // The agent and the panel read the version off this. The bridge stays four methods.
            userAgentString = shellUserAgent(userAgentString, shellVersionName())
        }

        // Fits the fixed 480 CSS pixel layout to this display. See
        // docs/architecture.md, "Scaling".
        val widthPx = resources.displayMetrics.widthPixels
        val scalePercent = ((widthPx.toFloat() / PANEL_CSS_WIDTH) * 100f).toInt().coerceIn(25, 400)
        view.setInitialScale(scalePercent)
        Log.i(TAG, "panel scale: $scalePercent% for a ${widthPx}px wide display")

        view.isVerticalScrollBarEnabled = false
        view.isHorizontalScrollBarEnabled = false
        view.overScrollMode = View.OVER_SCROLL_NEVER

        bridge = ShellBridge(this)
        view.addJavascriptInterface(bridge, "shell")

        // The device has no console, so script errors and socket failures show
        // up only here.
        view.webChromeClient = object : WebChromeClient() {
            override fun onConsoleMessage(message: ConsoleMessage): Boolean {
                Log.println(
                    consolePriority(message.messageLevel()),
                    TAG,
                    consoleLine(message.message(), message.sourceId(), message.lineNumber()),
                )
                return true
            }
        }

        view.webViewClient = object : WebViewClient() {
            override fun onReceivedError(
                view: WebView?,
                request: WebResourceRequest?,
                error: WebResourceError?,
            ) {
                // Only a failure of the page itself gets a fallback screen.
                if (request?.isForMainFrame != true) return
                val description = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                    error?.description?.toString().orEmpty()
                } else {
                    ""
                }
                val reason = description.ifBlank { getString(R.string.load_failed) }
                Log.w(TAG, "page load failed: $reason")
                pageFailed = true
                showFallback(reason)
            }

            // The agent answered but not with the page. A wrong token returns 401,
            // which WebView would show as a loaded page. Here that is black.
            override fun onReceivedHttpError(
                view: WebView?,
                request: WebResourceRequest?,
                errorResponse: WebResourceResponse?,
            ) {
                if (request?.isForMainFrame != true || errorResponse == null) return
                val status = errorResponse.statusCode
                val reason = httpFailureReason(status, errorResponse.reasonPhrase)
                Log.w(TAG, "page load failed: $reason")
                pageFailed = true
                showFallback(reason, rejected = isAuthRejection(status))
            }

            override fun onPageFinished(view: WebView?, url: String?) {
                if (!finishedPageClearsFallback(pageFailed, watcher.isDown, mismatch != ProtocolMismatch.NONE)) {
                    Log.i(TAG, "page finished without a working panel, keeping the error up")
                    return
                }
                Log.i(TAG, "page loaded")
                lastError = ""
                backoff.reset()
                hideFallback()
            }
        }

        return view
    }

    private fun loadPanel() {
        val url = settings.panelUrl()
        if (url.isBlank()) {
            showFallback(getString(R.string.not_configured))
            return
        }
        Log.i(TAG, "loading the panel")
        pageFailed = false
        webView.loadUrl(url)
        watcher.start()
    }

    private fun showFallback(reason: String, rejected: Boolean = false) {
        lastError = reason
        // Neither an unconfigured shell nor a rejected token is an unreachable
        // agent, so each gets its own title.
        val title = when {
            !settings.isConfigured -> getString(R.string.fallback_title_unconfigured)
            mismatch == ProtocolMismatch.UPDATE_SHELL -> getString(R.string.fallback_title_update_shell)
            mismatch == ProtocolMismatch.UPDATE_AGENT -> getString(R.string.fallback_title_update_agent)
            rejected -> getString(R.string.fallback_title_rejected)
            else -> getString(R.string.fallback_title)
        }
        fallback.show(
            title,
            settings.agentUrl.ifBlank { getString(R.string.no_url) },
            reason,
            showHint = settings.isConfigured,
        )
        fallback.visibility = View.VISIBLE
        scheduleRetry()
    }

    /** Shows the mismatch screen when the agent's protocol differs, and clears it once they agree. */
    private fun checkProtocol(agent: Int?) {
        val next = protocolMismatch(PROTOCOL_VERSION, agent)
        val changed = next != mismatch
        mismatch = next
        when {
            next != ProtocolMismatch.NONE -> {
                if (changed || fallback.visibility != View.VISIBLE) {
                    Log.w(TAG, "protocol mismatch: the agent speaks $agent, this shell speaks $PROTOCOL_VERSION")
                    showFallback(getString(R.string.protocol_mismatch, agent, PROTOCOL_VERSION))
                }
            }
            changed -> {
                Log.i(TAG, "agent and shell protocols agree again")
                hideFallbackAndReload()
            }
        }
    }

    private fun shellVersionName(): String = try {
        packageManager.getPackageInfo(packageName, 0).versionName ?: "unknown"
    } catch (error: PackageManager.NameNotFoundException) {
        "unknown"
    }

    private fun hideFallback() {
        fallback.visibility = View.GONE
    }

    private fun hideFallbackAndReload() {
        hideFallback()
        // After a change or a recovery, retry at the fast pace again.
        backoff.reset()
        loadPanel()
    }

    /** Retries with a growing delay while the fallback is up. Nobody is at the device. */
    private fun scheduleRetry() {
        if (retryScheduled) return
        retryScheduled = true
        val delay = backoff.next()
        main.postDelayed({
            retryScheduled = false
            if (fallback.visibility == View.VISIBLE && settings.isConfigured) {
                Log.i(TAG, "retrying the panel")
                loadPanel()
                scheduleRetry()
            }
        }, delay)
    }

    /** A three second press opens settings. Dusting the screen does not trigger it. */
    private fun installLongPressGesture() {
        val opener = Runnable { openSettings() }

        root.setOnTouchListener { _, event ->
            when (event.actionMasked) {
                MotionEvent.ACTION_DOWN -> main.postDelayed(opener, LONG_PRESS_MS)
                MotionEvent.ACTION_UP,
                MotionEvent.ACTION_CANCEL,
                MotionEvent.ACTION_POINTER_UP,
                -> main.removeCallbacks(opener)
            }
            // The page still needs its own taps for switching faces.
            false
        }
    }

    private fun openSettings() {
        if (supportFragmentManager.findFragmentByTag(SettingsSheet.TAG) != null) return
        SettingsSheet(onWifi = { openWifiSettings() }) { saved ->
            if (saved) {
                Log.i(TAG, "settings saved, reloading")
                hideFallbackAndReload()
            }
        }.show(supportFragmentManager, SettingsSheet.TAG)
    }

    /** Hands over to Android's own Wi-Fi settings. See docs/architecture.md, "Shell". */
    private fun openWifiSettings() {
        wifiOpened = true
        try {
            startActivity(Intent(android.provider.Settings.ACTION_WIFI_SETTINGS))
        } catch (error: ActivityNotFoundException) {
            wifiOpened = false
            Log.w(TAG, "this device has no wifi settings screen", error)
            Toast.makeText(this, R.string.wifi_unavailable, Toast.LENGTH_LONG).show()
        }
    }

    @Suppress("DEPRECATION")
    private fun goFullscreen() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            window.setDecorFitsSystemWindows(false)
            window.insetsController?.let { controller ->
                controller.hide(android.view.WindowInsets.Type.systemBars())
                controller.systemBarsBehavior =
                    android.view.WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
            }
        } else {
            window.decorView.systemUiVisibility = (
                View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                    or View.SYSTEM_UI_FLAG_FULLSCREEN
                    or View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                    or View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                    or View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                    or View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION
                )
        }
    }

    private companion object {
        /** The panel's CSS width. The agent's layout is fixed at this. */
        const val PANEL_CSS_WIDTH = 480
        const val LONG_PRESS_MS = 3_000L
    }
}

/** The native fallback. It does not use the web layer, which is what may be failing. */
class FallbackView(
    activity: AppCompatActivity,
    private val onSettings: () -> Unit,
) : LinearLayout(activity) {

    private val title = TextView(activity)
    private val urlLabel = TextView(activity)
    private val errorLabel = TextView(activity)
    private val hintLabel = TextView(activity)

    init {
        orientation = VERTICAL
        gravity = Gravity.CENTER
        setBackgroundColor(Color.BLACK)
        val pad = (24 * resources.displayMetrics.density).toInt()
        setPadding(pad, pad, pad, pad)

        title.apply {
            setTextColor(Color.parseColor("#EDF1EE"))
            textSize = 19f
            gravity = Gravity.CENTER
        }
        urlLabel.apply {
            setTextColor(Color.parseColor("#76878A"))
            textSize = 13f
            gravity = Gravity.CENTER
        }
        errorLabel.apply {
            setTextColor(Color.parseColor("#E04E3C"))
            textSize = 12f
            gravity = Gravity.CENTER
        }

        hintLabel.apply {
            setTextColor(Color.parseColor("#76878A"))
            textSize = 12f
            gravity = Gravity.CENTER
            text = context.getString(R.string.fallback_hint)
        }

        val settingsButton = Button(activity).apply {
            text = context.getString(R.string.fallback_settings)
            setOnClickListener { onSettings() }
        }

        addView(title)
        addView(urlLabel, marginParams(10))
        addView(errorLabel, marginParams(8))
        addView(hintLabel, marginParams(8))
        addView(settingsButton, marginParams(14))
    }

    fun show(heading: String, url: String, error: String, showHint: Boolean) {
        title.text = heading
        urlLabel.text = url
        errorLabel.text = error
        // An unconfigured shell's message already says how to open settings.
        hintLabel.visibility = if (showHint) View.VISIBLE else View.GONE
    }

    private fun marginParams(topDp: Int): LayoutParams {
        return LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT).apply {
            topMargin = (topDp * resources.displayMetrics.density).toInt()
            gravity = Gravity.CENTER_HORIZONTAL
        }
    }
}
