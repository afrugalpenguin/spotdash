package dev.spotdash.shell

import android.content.pm.ApplicationInfo
import android.util.Log
import android.webkit.ConsoleMessage.MessageLevel

/** Forwarding the panel's console to logcat, kept out of PanelActivity for unit tests. */

/** The logcat priority for a console level. */
internal fun consolePriority(level: MessageLevel): Int = when (level) {
    MessageLevel.ERROR -> Log.ERROR
    MessageLevel.WARNING -> Log.WARN
    MessageLevel.LOG -> Log.INFO
    MessageLevel.DEBUG, MessageLevel.TIP -> Log.DEBUG
}

/** One logcat line for a console message, with the source and line number when there is one. */
internal fun consoleLine(message: String, sourceId: String, lineNumber: Int): String {
    val text = redactSecrets(message)
    val source = redactSecrets(sourceId)
    return if (source.isEmpty()) "console: $text" else "console: $text ($source:$lineNumber)"
}

// The token appears once as ?token= and again as a "bearer." subprotocol. Either
// can reach a console message or its source, and anyone with adb can read logcat.
private val tokenParam = Regex("([?&]token=)[^&#\\s\"')]+")
private val bearerProtocol = Regex("(bearer\\.)[^\\s,\"')]+")

private fun redactSecrets(text: String): String =
    text.replace(tokenParam, "$1<redacted>").replace(bearerProtocol, "$1<redacted>")

/** True when the app is a debug build, judged by the flags on its ApplicationInfo. */
internal fun isDebuggable(applicationFlags: Int): Boolean =
    applicationFlags and ApplicationInfo.FLAG_DEBUGGABLE != 0
