package dev.spotdash.shell

import android.content.pm.ApplicationInfo
import android.util.Log
import android.webkit.ConsoleMessage.MessageLevel

/**
 * Forwarding the panel's console to logcat, kept apart from PanelActivity so the
 * parts that need no Android device can be unit tested.
 */

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

// The panel URL carries the token once as ?token=, and the WebSocket sends it as
// a "bearer." subprotocol. Either can turn up in a console message or as the
// source of one (a failed load of the panel URL is reported against that URL),
// and logcat is readable by anyone with adb.
private val tokenParam = Regex("([?&]token=)[^&#\\s\"')]+")
private val bearerProtocol = Regex("(bearer\\.)[^\\s,\"')]+")

private fun redactSecrets(text: String): String =
    text.replace(tokenParam, "$1<redacted>").replace(bearerProtocol, "$1<redacted>")

/** True when the app is a debug build, judged by the flags on its ApplicationInfo. */
internal fun isDebuggable(applicationFlags: Int): Boolean =
    applicationFlags and ApplicationInfo.FLAG_DEBUGGABLE != 0
