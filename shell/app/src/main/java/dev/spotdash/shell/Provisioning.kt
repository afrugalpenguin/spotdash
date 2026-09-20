package dev.spotdash.shell

import java.io.File
import java.io.IOException
import java.net.URI
import java.net.URISyntaxException
import java.nio.ByteBuffer
import java.nio.charset.CharacterCodingException
import java.nio.charset.CodingErrorAction

/**
 * Takes the agent URL and token from a provision.json that adb pushed. Every rule fails closed and
 * no rejection reason quotes the payload (an I/O error message is safe). See docs/architecture.md,
 * "Provisioning from adb".
 */

/** The file adb pushes, in the directory getExternalFilesDir returns. */
internal const val PROVISIONING_FILE_NAME = "provision.json"

/** The largest payload read. A real one is about 100 bytes. A file pushed by mistake is not read whole. */
internal const val MAX_PROVISIONING_BYTES = 4096

private const val PROVISIONING_VERSION = 1L
private val BYTE_ORDER_MARK = Char(0xFEFF).toString()
private const val MAX_TOKEN_LENGTH = 256
private val ALLOWED_KEYS = setOf("version", "agent_url", "token")

internal sealed class ProvisioningResult {
    data class Valid(val agentUrl: String, val token: String) : ProvisioningResult() {
        // The default data class toString would print the token.
        override fun toString() = "Valid(agentUrl=$agentUrl, token=<redacted>)"
    }

    data class Rejected(val reason: String) : ProvisioningResult()
}

/** A result, and whether the file it came from could be removed afterwards. */
internal class Consumed(val result: ProvisioningResult, val deleted: Boolean)

/** Reads and deletes the provisioning file, or returns null if there is none. Deleting stops a replay. */
internal fun consumeProvisioning(file: File): Consumed? {
    if (!file.exists()) return null

    val result = try {
        parseProvisioningBytes(readAtMost(file, MAX_PROVISIONING_BYTES + 1))
    } catch (error: IOException) {
        ProvisioningResult.Rejected("the file could not be read (${error.message ?: error.javaClass.simpleName})")
    }
    return Consumed(result, deleted = file.delete())
}

private fun readAtMost(file: File, limit: Int): ByteArray {
    val buffer = ByteArray(limit)
    var filled = 0
    file.inputStream().use { input ->
        while (filled < limit) {
            val n = input.read(buffer, filled, limit - filled)
            if (n < 0) break
            filled += n
        }
    }
    return buffer.copyOf(filled)
}

private fun parseProvisioningBytes(bytes: ByteArray): ProvisioningResult {
    if (bytes.size > MAX_PROVISIONING_BYTES) {
        return ProvisioningResult.Rejected("the payload is larger than $MAX_PROVISIONING_BYTES bytes")
    }
    val text = try {
        Charsets.UTF_8.newDecoder()
            .onMalformedInput(CodingErrorAction.REPORT)
            .onUnmappableCharacter(CodingErrorAction.REPORT)
            .decode(ByteBuffer.wrap(bytes))
            .toString()
    } catch (error: CharacterCodingException) {
        return ProvisioningResult.Rejected("the payload is not valid UTF-8")
    }
    // Windows PowerShell 5.1 writes a byte order mark with -Encoding utf8.
    return parseProvisioning(text.removePrefix(BYTE_ORDER_MARK))
}

/** Checks a payload's text and returns the settings it carries, or why not. */
internal fun parseProvisioning(text: String): ProvisioningResult {
    if (text.length > MAX_PROVISIONING_BYTES) {
        return ProvisioningResult.Rejected("the payload is larger than $MAX_PROVISIONING_BYTES characters")
    }

    val fields = try {
        FlatJson(text).parseObject()
    } catch (error: BadPayload) {
        return ProvisioningResult.Rejected(error.message ?: "the payload is not valid")
    }

    if (fields.keys.any { it !in ALLOWED_KEYS }) {
        return ProvisioningResult.Rejected("unexpected key (allowed: ${ALLOWED_KEYS.joinToString(", ")})")
    }
    for (key in ALLOWED_KEYS) {
        if (key !in fields) return ProvisioningResult.Rejected("\"$key\" is missing")
    }

    if (fields["version"] != PROVISIONING_VERSION) {
        return ProvisioningResult.Rejected("\"version\" must be the number $PROVISIONING_VERSION")
    }
    val agentUrl = fields["agent_url"] as? String
        ?: return ProvisioningResult.Rejected("\"agent_url\" must be a string")
    val token = fields["token"] as? String
        ?: return ProvisioningResult.Rejected("\"token\" must be a string")

    checkAgentUrl(agentUrl)?.let { return ProvisioningResult.Rejected(it) }
    checkToken(token)?.let { return ProvisioningResult.Rejected(it) }

    return ProvisioningResult.Valid(agentUrl, token)
}

/** The host of an address that has already passed [parseProvisioning], for the log. */
internal fun provisioningHost(agentUrl: String): String =
    try {
        URI(agentUrl).host.orEmpty()
    } catch (error: URISyntaxException) {
        ""
    }

/** Null when the address is acceptable, otherwise why not. */
private fun checkAgentUrl(url: String): String? {
    if (url.any { it.isWhitespace() }) return "\"agent_url\" contains whitespace"

    val parsed = try {
        URI(url)
    } catch (error: URISyntaxException) {
        return "\"agent_url\" is not a valid address"
    }
    val scheme = parsed.scheme?.lowercase()
    if (scheme != "http" && scheme != "https") return "\"agent_url\" scheme must be http or https"
    if (parsed.host.isNullOrEmpty()) return "\"agent_url\" has no valid host"
    if (parsed.rawUserInfo != null) return "\"agent_url\" must not contain credentials"
    if (parsed.port != -1 && parsed.port !in 1..65535) return "\"agent_url\" port must be 1 to 65535"
    // The shell appends its own token query and loads the root page, so a path
    // would point somewhere else.
    if (!parsed.rawPath.isNullOrEmpty() && parsed.rawPath != "/") return "\"agent_url\" must not have a path"
    if (parsed.rawQuery != null) return "\"agent_url\" must not have a query"
    if (parsed.rawFragment != null) return "\"agent_url\" must not have a fragment"
    return null
}

/**
 * Null when the token is acceptable, otherwise why not. Printable ASCII with no
 * whitespace, since it goes into a URL query and a WebSocket subprotocol. There
 * is no minimum length: the agent decides what a token has to be.
 */
private fun checkToken(token: String): String? {
    if (token.isEmpty()) return "\"token\" is empty"
    if (token.length > MAX_TOKEN_LENGTH) return "\"token\" is longer than $MAX_TOKEN_LENGTH characters"
    if (token.any { it.code !in 0x21..0x7E }) return "\"token\" must be printable ASCII with no whitespace"
    return null
}

private class BadPayload(message: String) : Exception(message)

/** Reads one flat JSON object of strings and whole numbers. Hand written since org.json is stubbed in unit tests. */
private class FlatJson(private val text: String) {
    private var pos = 0

    fun parseObject(): Map<String, Any> {
        skipSpace()
        expect('{', "the payload must be a JSON object")
        val out = LinkedHashMap<String, Any>()

        skipSpace()
        if (peek() == '}') {
            pos++
        } else {
            while (true) {
                skipSpace()
                if (peek() != '"') fail("expected a key")
                val key = readString()
                if (key in out) fail("a key is repeated")
                skipSpace()
                expect(':', "expected a colon after a key")
                skipSpace()
                out[key] = readValue()
                skipSpace()
                when (peek()) {
                    ',' -> pos++
                    '}' -> { pos++; break }
                    else -> fail("expected a comma or the end of the object")
                }
            }
        }

        skipSpace()
        if (pos != text.length) fail("there is text after the object")
        return out
    }

    private fun readValue(): Any = when (val c = peek()) {
        '"' -> readString()
        in '0'..'9' -> readNumber()
        else -> fail(if (c == null) "the payload ends early" else "a value is not a string or a whole number")
    }

    private fun readNumber(): Long {
        val start = pos
        while (peek() in '0'..'9') pos++
        val digits = text.substring(start, pos)
        if (digits.length > 1 && digits[0] == '0') fail("a number has a leading zero")
        if (digits.length > 9) fail("a number is too large")
        if (peek() == '.' || peek() == 'e' || peek() == 'E') fail("a value is not a whole number")
        return digits.toLong()
    }

    private fun readString(): String {
        pos++ // opening quote
        val out = StringBuilder()
        while (true) {
            val c = peek() ?: fail("a string is not closed")
            pos++
            when {
                c == '"' -> return out.toString()
                c.code < 0x20 -> fail("a string contains a control character")
                c == '\\' -> out.append(readEscape())
                else -> out.append(c)
            }
        }
    }

    private fun readEscape(): Char {
        val c = peek() ?: fail("a string is not closed")
        pos++
        return when (c) {
            '"', '\\', '/' -> c
            'b' -> '\b'
            'f' -> Char(0x0C)
            'n' -> '\n'
            'r' -> '\r'
            't' -> '\t'
            'u' -> {
                if (pos + 4 > text.length) fail("a unicode escape is cut short")
                val hex = text.substring(pos, pos + 4)
                if (!hex.all { it in '0'..'9' || it in 'a'..'f' || it in 'A'..'F' }) fail("a unicode escape is not valid")
                pos += 4
                hex.toInt(16).toChar()
            }
            else -> fail("a string has an unknown escape")
        }
    }

    private fun peek(): Char? = if (pos < text.length) text[pos] else null

    private fun skipSpace() {
        while (peek().let { it == ' ' || it == '\t' || it == '\n' || it == '\r' }) pos++
    }

    private fun expect(c: Char, message: String) {
        if (peek() != c) fail(message)
        pos++
    }

    private fun fail(message: String): Nothing = throw BadPayload(message)
}
