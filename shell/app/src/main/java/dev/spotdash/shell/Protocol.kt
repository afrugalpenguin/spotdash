package dev.spotdash.shell

/**
 * The protocol version handshake with the agent. Logic that needs no Android, kept out of the
 * activity for unit tests. See docs/architecture.md, "Protocol version".
 */

/** What the shell speaks: the bridge methods and the provisioning payload. Bump it only when either changes incompatibly. */
internal const val PROTOCOL_VERSION = 1

/** Which side is behind when the shell and the agent disagree. */
internal enum class ProtocolMismatch { NONE, UPDATE_SHELL, UPDATE_AGENT }

/** The WebView user agent with the shell token appended. The agent and the panel read it back. */
internal fun shellUserAgent(defaultAgent: String, versionName: String): String {
    val version = versionName.replace(Regex("\\s+"), "_")
    return "$defaultAgent spotdash-shell/$version proto/$PROTOCOL_VERSION"
}

private val PROTOCOL_FIELD = Regex("\"protocol\"\\s*:\\s*(\\d+)(?![\\d.])")

/** The `protocol` from a `/health` body, or null when it is absent or not a whole number. */
internal fun agentProtocol(healthBody: String): Int? =
    PROTOCOL_FIELD.find(healthBody)?.groupValues?.get(1)?.toIntOrNull()

/** An agent that reports nothing is an older build and is not called a mismatch. */
internal fun protocolMismatch(shell: Int, agent: Int?): ProtocolMismatch = when {
    agent == null || agent == shell -> ProtocolMismatch.NONE
    shell < agent -> ProtocolMismatch.UPDATE_SHELL
    else -> ProtocolMismatch.UPDATE_AGENT
}
