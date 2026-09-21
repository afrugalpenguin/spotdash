package dev.spotdash.shell

/** Loading logic that needs no Android, kept out of PanelActivity for unit tests. */

/** True when the agent answered but refused the token. Retrying the same token will not help. */
internal fun isAuthRejection(status: Int): Boolean = status == 401 || status == 403

/** What the fallback screen says about an HTTP error: the code and the reason phrase, if any. */
internal fun httpFailureReason(status: Int, phrase: String?): String {
    val text = phrase?.trim().orEmpty()
    return if (text.isEmpty()) "HTTP $status" else "HTTP $status $text"
}

/**
 * Whether a finished page should hide the fallback. A protocol mismatch keeps it up, since the page
 * loads fine from an agent the shell cannot work with. See docs/architecture.md, "Failure behaviour".
 */
internal fun finishedPageClearsFallback(
    pageFailed: Boolean,
    agentDown: Boolean,
    protocolMismatched: Boolean = false,
): Boolean = !pageFailed && !agentDown && !protocolMismatched

/** The delay before the next load attempt: short first, doubling to a ceiling. */
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

    /** Called after a load succeeds. The next outage starts fast again. */
    fun reset() {
        nextMs = firstMs
    }
}
