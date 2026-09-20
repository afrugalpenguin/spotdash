package dev.spotdash.shell

import android.os.Handler
import android.os.Looper
import android.util.Log
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors

/** Polls the agent's `/health` and reports when it has been unreachable too long. It needs no token. */
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

    /** True once the agent has been unreachable long enough to raise the fallback. */
    val isDown: Boolean
        get() = reportedDown

    fun start() {
        if (running) return
        running = true
        // reportedDown survives a stop and start, so a pause cannot make the
        // watcher forget the agent was down. See docs/architecture.md,
        // "Failure behaviour".
        firstFailureAt = 0L
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

        // A restarting agent is back within a second or two. A fallback flashing
        // at every restart is worse than a briefly stale dashboard.
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
