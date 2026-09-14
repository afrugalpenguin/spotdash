package dev.spotdash.shell

import android.annotation.SuppressLint
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
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity

/**
 * The whole shell.
 *
 * One Activity holding a WebView, a native fallback screen, and a settings
 * screen reached by a long press. All logic and all UI live in the agent; this
 * exists to put the agent's page on the glass and keep it there.
 */
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

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        settings = Settings(this)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)

        root = FrameLayout(this).apply { setBackgroundColor(Color.BLACK) }
        setContentView(root)

        // After setContentView, not before. The insets controller lives on the
        // decor view, which does not exist until the content is set, and asking
        // for it earlier returns null and takes the process down on launch.
        goFullscreen()

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
        )

        if (settings.isConfigured) {
            loadPanel()
        } else {
            // Nothing configured yet, which on first install is the normal
            // state rather than an error.
            showFallback(getString(R.string.not_configured))
        }
    }

    override fun onResume() {
        super.onResume()
        goFullscreen()
        if (settings.isConfigured) {
            watcher.start()
        }
    }

    override fun onPause() {
        super.onPause()
        watcher.stop()
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
        // The system bars come back after a dialog or a notification, so they
        // are pushed away again every time focus returns.
        if (hasFocus) goFullscreen()
    }

    /** There is nowhere to go back to. Back must not leave the panel. */
    @Suppress("DEPRECATION")
    override fun onBackPressed() {
        if (fallback.visibility == View.VISIBLE && settings.isConfigured) {
            hideFallbackAndReload()
            return
        }
        // Deliberately no super call.
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun buildWebView(): WebView {
        val view = WebView(this)
        view.setBackgroundColor(Color.BLACK)

        view.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            // The panel is a fixed 480x480 CSS layout, and CSS pixels are
            // density independent. This display is 240dpi, so without viewport
            // fitting those 480 CSS pixels become 720 physical ones and two
            // thirds of the panel falls off the glass.
            //
            // Honouring the page's own viewport tag and scaling it to the window
            // makes the panel fit whatever density it lands on, which matters
            // because the Echo Spot's density need not match the emulator's.
            useWideViewPort = true
            loadWithOverviewMode = false
            setSupportZoom(false)
            builtInZoomControls = false
            displayZoomControls = false
            // The device caches aggressively and the agent is rebuilt often.
            // Stale UI on a panel with no address bar is painful to diagnose.
            cacheMode = android.webkit.WebSettings.LOAD_NO_CACHE
            mediaPlaybackRequiresUserGesture = false
        }

        // Scale the panel's fixed 480 CSS pixel layout onto however many
        // physical pixels this display actually has. Left alone, a 240dpi
        // screen renders those 480 CSS pixels as 720 physical ones and two
        // thirds of the panel falls off the glass.
        val widthPx = resources.displayMetrics.widthPixels
        val scalePercent = ((widthPx.toFloat() / PANEL_CSS_WIDTH) * 100f).toInt().coerceIn(25, 400)
        view.setInitialScale(scalePercent)
        Log.i(TAG, "panel scale: $scalePercent% for a ${widthPx}px wide display")

        view.isVerticalScrollBarEnabled = false
        view.isHorizontalScrollBarEnabled = false
        view.overScrollMode = View.OVER_SCROLL_NEVER

        bridge = ShellBridge(this)
        view.addJavascriptInterface(bridge, "shell")

        view.webViewClient = object : WebViewClient() {
            override fun onReceivedError(
                view: WebView?,
                request: WebResourceRequest?,
                error: WebResourceError?,
            ) {
                // Subresource failures are not worth a fallback screen; only a
                // failure of the page itself is.
                if (request?.isForMainFrame != true) return
                val description = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                    error?.description?.toString().orEmpty()
                } else {
                    ""
                }
                val reason = description.ifBlank { getString(R.string.load_failed) }
                Log.w(TAG, "page load failed: $reason")
                showFallback(reason)
            }

            override fun onPageFinished(view: WebView?, url: String?) {
                Log.i(TAG, "page loaded")
                lastError = ""
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
        webView.loadUrl(url)
        watcher.start()
    }

    private fun showFallback(reason: String) {
        lastError = reason
        // An unconfigured shell has not failed at anything, so it should not
        // say it cannot reach something it was never told about.
        val title = if (settings.isConfigured) {
            getString(R.string.fallback_title)
        } else {
            getString(R.string.fallback_title_unconfigured)
        }
        fallback.show(title, settings.agentUrl.ifBlank { getString(R.string.no_url) }, reason)
        fallback.visibility = View.VISIBLE
        scheduleRetry()
    }

    private fun hideFallback() {
        fallback.visibility = View.GONE
    }

    private fun hideFallbackAndReload() {
        hideFallback()
        loadPanel()
    }

    /**
     * Retries on a fixed interval while the fallback is up.
     *
     * There is nobody standing at the device to press anything, so recovery has
     * to be automatic and has to keep trying for as long as it takes.
     */
    private fun scheduleRetry() {
        if (retryScheduled) return
        retryScheduled = true
        main.postDelayed({
            retryScheduled = false
            if (fallback.visibility == View.VISIBLE && settings.isConfigured) {
                Log.i(TAG, "retrying the panel")
                loadPanel()
                scheduleRetry()
            }
        }, RETRY_INTERVAL_MS)
    }

    /**
     * A three second press anywhere opens settings.
     *
     * The device has no buttons worth using and no keyboard, so this gesture is
     * the only way in. Three seconds is long enough that nobody reaches it by
     * dusting the screen.
     */
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
            // Never consume: the page still has to receive its own taps for
            // switching faces.
            false
        }
    }

    private fun openSettings() {
        if (supportFragmentManager.findFragmentByTag(SettingsSheet.TAG) != null) return
        SettingsSheet { saved ->
            if (saved) {
                Log.i(TAG, "settings saved, reloading")
                hideFallbackAndReload()
            }
        }.show(supportFragmentManager, SettingsSheet.TAG)
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
        const val RETRY_INTERVAL_MS = 10_000L
    }
}

/**
 * The native fallback.
 *
 * Native rather than a local HTML page on purpose: it is shown precisely when
 * the web layer is what is in question, so it must not depend on it.
 */
class FallbackView(
    activity: AppCompatActivity,
    private val onSettings: () -> Unit,
) : LinearLayout(activity) {

    private val title = TextView(activity)
    private val urlLabel = TextView(activity)
    private val errorLabel = TextView(activity)

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

        val settingsButton = Button(activity).apply {
            text = context.getString(R.string.fallback_settings)
            setOnClickListener { onSettings() }
        }

        addView(title)
        addView(urlLabel, marginParams(10))
        addView(errorLabel, marginParams(8))
        addView(settingsButton, marginParams(14))
    }

    fun show(heading: String, url: String, error: String) {
        title.text = heading
        urlLabel.text = url
        errorLabel.text = error
    }

    private fun marginParams(topDp: Int): LayoutParams {
        return LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT).apply {
            topMargin = (topDp * resources.displayMetrics.density).toInt()
            gravity = Gravity.CENTER_HORIZONTAL
        }
    }
}
