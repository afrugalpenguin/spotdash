package dev.spotdash.shell

import android.content.pm.ApplicationInfo
import android.util.Log
import android.webkit.ConsoleMessage.MessageLevel
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WebConsoleTest {

    @Test
    fun `console levels map onto the matching logcat priorities`() {
        assertEquals(Log.ERROR, consolePriority(MessageLevel.ERROR))
        assertEquals(Log.WARN, consolePriority(MessageLevel.WARNING))
        assertEquals(Log.INFO, consolePriority(MessageLevel.LOG))
        assertEquals(Log.DEBUG, consolePriority(MessageLevel.DEBUG))
        assertEquals(Log.DEBUG, consolePriority(MessageLevel.TIP))
    }

    @Test
    fun `a line carries the message and where it came from`() {
        assertEquals(
            "console: boom (http://host:8765/app.js:42)",
            consoleLine("boom", "http://host:8765/app.js", 42),
        )
    }

    @Test
    fun `a message with no source is not padded with an empty location`() {
        assertEquals("console: boom", consoleLine("boom", "", 0))
    }

    // The panel URL carries the token once, and a failed load of it is reported
    // by WebView with that URL as the source. logcat is readable by anyone with
    // adb, so it must never get there.
    @Test
    fun `the token is redacted from the source url`() {
        val line = consoleLine("Failed to load resource", "http://host:8765/?token=s3cretvalue&face=clock", 0)

        assertFalse(line, line.contains("s3cretvalue"))
        assertTrue(line, line.contains("face=clock"))
    }

    @Test
    fun `the token is redacted from the message text`() {
        val line = consoleLine("GET http://host:8765/?token=s3cretvalue failed", "", 0)

        assertFalse(line, line.contains("s3cretvalue"))
    }

    @Test
    fun `a websocket bearer subprotocol is redacted`() {
        val line = consoleLine("protocols were spotdash.v1, bearer.s3cretvalue", "", 0)

        assertFalse(line, line.contains("s3cretvalue"))
    }

    @Test
    fun `debugging is only enabled for a debuggable build`() {
        assertTrue(isDebuggable(ApplicationInfo.FLAG_DEBUGGABLE or ApplicationInfo.FLAG_HAS_CODE))
        assertFalse(isDebuggable(ApplicationInfo.FLAG_HAS_CODE))
        assertFalse(isDebuggable(0))
    }
}
