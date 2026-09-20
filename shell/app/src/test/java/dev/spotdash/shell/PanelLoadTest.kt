package dev.spotdash.shell

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PanelLoadTest {

    @Test
    fun `a rejected token is told apart from other failures`() {
        assertTrue(isAuthRejection(401))
        assertTrue(isAuthRejection(403))
        assertFalse(isAuthRejection(404))
        assertFalse(isAuthRejection(500))
    }

    @Test
    fun `the reason carries the status code and the phrase`() {
        assertEquals("HTTP 401 Unauthorized", httpFailureReason(401, "Unauthorized"))
    }

    @Test
    fun `the reason still names the status when there is no phrase`() {
        assertEquals("HTTP 500", httpFailureReason(500, null))
        assertEquals("HTTP 500", httpFailureReason(500, "  "))
    }

    @Test
    fun `a finished page only clears the error when nothing says it failed`() {
        assertTrue(finishedPageClearsFallback(pageFailed = false, agentDown = false))
        assertFalse(finishedPageClearsFallback(pageFailed = true, agentDown = false))
    }

    // WebView also reports a load finished when a retry abandons one that was
    // hanging on an unreachable agent, and that must not hide the fallback.
    @Test
    fun `a finished page does not clear the error while the agent is known to be down`() {
        assertFalse(finishedPageClearsFallback(pageFailed = false, agentDown = true))
    }

    @Test
    fun `retries slow down to a ceiling`() {
        val backoff = RetryBackoff()

        val delays = List(7) { backoff.next() }

        assertEquals(listOf(5_000L, 10_000L, 20_000L, 40_000L, 60_000L, 60_000L, 60_000L), delays)
    }

    @Test
    fun `a successful load starts the retries over`() {
        val backoff = RetryBackoff()
        repeat(4) { backoff.next() }

        backoff.reset()

        assertEquals(5_000L, backoff.next())
    }
}
