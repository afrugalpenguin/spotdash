package dev.spotdash.shell

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ProtocolTest {

    private val chrome = "Mozilla/5.0 (Linux; Android 11) AppleWebKit/537.36 Chrome/83.0 Mobile Safari/537.36"

    @Test
    fun `the user agent keeps the browser string and adds the token`() {
        val agent = shellUserAgent(chrome, "0.1.0-rc1")

        assertEquals("$chrome spotdash-shell/0.1.0-rc1 proto/$PROTOCOL_VERSION", agent)
    }

    @Test
    fun `a version name with spaces still gives one token`() {
        val agent = shellUserAgent(chrome, "1.0 beta")

        assertTrue(agent, agent.endsWith(" spotdash-shell/1.0_beta proto/$PROTOCOL_VERSION"))
    }

    @Test
    fun `the protocol is read from a health body`() {
        val body = """{"version":"v0.1.0","protocol":3,"uptime_seconds":12.5,"now":"2026-01-01T00:00:00Z","sources":{}}"""

        assertEquals(3, agentProtocol(body))
    }

    @Test
    fun `a health body from an agent with no handshake gives null`() {
        val body = """{"version":"v0.0.9","uptime_seconds":12.5,"sources":{}}"""

        assertNull(agentProtocol(body))
    }

    @Test
    fun `a protocol that is not a whole number gives null`() {
        assertNull(agentProtocol("""{"protocol":"2"}"""))
        assertNull(agentProtocol("""{"protocol":2.5}"""))
        assertNull(agentProtocol("""{"protocol":-1}"""))
        assertNull(agentProtocol("""{"protocol":99999999999}"""))
    }

    @Test
    fun `text that is not a health body gives null`() {
        assertNull(agentProtocol(""))
        assertNull(agentProtocol("<html>captive portal</html>"))
    }

    // A string value that quotes the key is escaped in JSON, so it never matches.
    @Test
    fun `an error string that mentions protocol is not read as the value`() {
        val body = """{"sources":{"spotify":{"status":"error","last_error":"bad \"protocol\": 9"}}}"""

        assertNull(agentProtocol(body))
    }

    @Test
    fun `a lower shell protocol says to update the shell`() {
        assertEquals(ProtocolMismatch.UPDATE_SHELL, protocolMismatch(shell = 1, agent = 2))
    }

    @Test
    fun `a higher shell protocol says to update the agent`() {
        assertEquals(ProtocolMismatch.UPDATE_AGENT, protocolMismatch(shell = 3, agent = 2))
    }

    @Test
    fun `equal protocols are no mismatch`() {
        assertEquals(ProtocolMismatch.NONE, protocolMismatch(shell = 2, agent = 2))
    }

    @Test
    fun `an agent that reports no protocol is not called a mismatch`() {
        assertEquals(ProtocolMismatch.NONE, protocolMismatch(shell = 1, agent = null))
    }

    @Test
    fun `a mismatch keeps the fallback up after the page finishes`() {
        assertEquals(
            false,
            finishedPageClearsFallback(pageFailed = false, agentDown = false, protocolMismatched = true),
        )
        assertEquals(
            true,
            finishedPageClearsFallback(pageFailed = false, agentDown = false, protocolMismatched = false),
        )
    }
}
