package dev.spotdash.shell

import android.os.Handler
import android.os.Looper
import android.util.Log
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors

/**
 * Watches the agent and reports when it has been unreachable for too long.
 *
 * The shell cannot ask the page whether its socket is up: the bridge is exactly
 * four methods and none of them report connection state. It does not need to.
 * `/health` is unauthenticated precisely so it stays usable when everything else
 * is broken, so the shell polls it directly.
 *
 * Doing it natively also means the fallback still works when the WebView itself
 * is what has failed, which is the case where asking the page would be useless.
 */
class AgentWatcher(
    private val healthUrl: () -> String,
    private val onDown: (String) -> Unit,
    private val onUp: () -> Unit,
) {

    private val main = Handler(Looper.getMainLooper())
    private val pool = Executors.newSingleThreadExecutor()

    private var running = false
    private var firstFailureAt = 0L
    private var reportedDown = false

    fun start() {
        if (running) return
        running = true
        firstFailureAt = 0L
        reportedDown = false
        main.post(pollTask)
    }

    fun stop() {
        running = false
        main.removeCallbacks(pollTask)
    }

    fun shutdown() {
        stop()
        pool.shutdownNow()
    }

    private val pollTask = object : Runnable {
        override fun run() {
            if (!running) return
            check()
            main.postDelayed(this, POLL_INTERVAL_MS)
        }
    }

    private fun check() {
        val url = healthUrl()
        if (url.isBlank()) return

        pool.execute {
            val failure = probe(url)
            main.post { record(failure) }
        }
    }

    private fun record(failure: String?) {
        if (!running) return

        if (failure == null) {
            if (reportedDown) {
                Log.i(TAG, "agent reachable again")
                onUp()
            }
            firstFailureAt = 0L
            reportedDown = false
            return
        }

        val now = System.currentTimeMillis()
        if (firstFailureAt == 0L) {
            firstFailureAt = now
            Log.w(TAG, "agent unreachable: $failure")
        }

        // A brief blip is not worth taking the panel down for. A restarting
        // agent is back within a second or two, and flashing a fallback screen
        // at every restart would be worse than showing a stale dashboard.
        val downFor = now - firstFailureAt
        if (!reportedDown && downFor >= DOWN_AFTER_MS) {
            reportedDown = true
            onDown(failure)
        }
    }

    /** Returns null when the agent answered, or a reason when it did not. */
    private fun probe(url: String): String? {
        var connection: HttpURLConnection? = null
        return try {
            connection = (URL(url).openConnection() as HttpURLConnection).apply {
                connectTimeout = TIMEOUT_MS
                readTimeout = TIMEOUT_MS
                requestMethod = "GET"
            }
            val code = connection.responseCode
            if (code in 200..299) null else "agent returned HTTP $code"
        } catch (error: Exception) {
            error.message ?: error.javaClass.simpleName
        } finally {
            connection?.disconnect()
        }
    }

    private companion object {
        const val POLL_INTERVAL_MS = 5_000L
        const val DOWN_AFTER_MS = 30_000L
        const val TIMEOUT_MS = 4_000
    }
}
