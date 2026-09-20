package dev.spotdash.shell

/**
 * The parts of loading the panel that need no Android to reason about, kept
 * apart from PanelActivity so they can be unit tested.
 */

/** True when the agent answered but refused the token, which retrying the same token will not fix. */
internal fun isAuthRejection(status: Int): Boolean = status == 401 || status == 403

/** What the fallback screen says about an HTTP error: the code, and the reason phrase when there is one. */
internal fun httpFailureReason(status: Int, phrase: String?): String {
    val text = phrase?.trim().orEmpty()
    return if (text.isEmpty()) "HTTP $status" else "HTTP $status $text"
}

/**
 * Whether WebView reporting a page finished should take the fallback screen down.
 *
 * WebView reports a load finished after a failed one too, and again when a retry
 * abandons a load that was hanging on an agent that is not there. Neither means
 * the panel is showing, so the error stays up unless the load had no error and
 * the agent is not known to be down.
 */
internal fun finishedPageClearsFallback(pageFailed: Boolean, agentDown: Boolean): Boolean =
    !pageFailed && !agentDown

/**
 * How long to wait before the next attempt to load the panel.
 *
 * Starts short, because the usual cause is an agent that is about to come back,
 * and slows to a ceiling, because the other cause is a token nobody has fixed
 * yet and hammering the agent does nothing for that.
 */
internal class RetryBackoff(
    private val firstMs: Long = 5_000L,
    private val maxMs: Long = 60_000L,
) {
    private var nextMs = firstMs

    /** The delay to use now. The one after it is longer, up to the ceiling. */
    fun next(): Long {
        val delay = nextMs
        nextMs = minOf(nextMs * 2, maxMs)
        return delay
    }

    /** Called after a load succeeds, so the next outage starts fast again. */
    fun reset() {
        nextMs = firstMs
    }
}
