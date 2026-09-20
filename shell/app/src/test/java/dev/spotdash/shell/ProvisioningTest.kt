package dev.spotdash.shell

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class ProvisioningTest {

    // Not a real token or address: placeholders for the tests only.
    private val token = "Abc123Def456Ghi789Jkl012Mno345Pq"
    private val url = "http://192.0.2.10:8765"

    private fun payload(
        version: String = "1",
        agentUrl: String = url,
        tokenValue: String = token,
    ) = """{"version":$version,"agent_url":"$agentUrl","token":"$tokenValue"}"""

    private fun assertRejected(text: String, reasonContains: String? = null) {
        val result = parseProvisioning(text)
        assertTrue("got $result, want a rejection", result is ProvisioningResult.Rejected)
        val reason = (result as ProvisioningResult.Rejected).reason
        if (reasonContains != null) {
            assertTrue("reason = \"$reason\", want \"$reasonContains\"", reason.contains(reasonContains))
        }
        // The reason ends up in logcat, so it must never carry the secret.
        assertFalse("reason leaks the token: $reason", reason.contains(token))
    }

    // ---- valid ----

    @Test
    fun `a valid payload gives the url and token`() {
        val result = parseProvisioning(payload())
        assertEquals(ProvisioningResult.Valid(url, token), result)
    }

    @Test
    fun `key order and whitespace do not matter`() {
        val text = """
            {
              "token": "$token",
              "version": 1,
              "agent_url": "$url"
            }
        """.trimIndent()
        assertEquals(ProvisioningResult.Valid(url, token), parseProvisioning(text))
    }

    @Test
    fun `https, a hostname, a trailing slash and no port are all fine`() {
        assertTrue(parseProvisioning(payload(agentUrl = "https://dash.example.test/")) is ProvisioningResult.Valid)
        assertTrue(parseProvisioning(payload(agentUrl = "http://desk-pc:8765")) is ProvisioningResult.Valid)
        assertTrue(parseProvisioning(payload(agentUrl = "http://127.0.0.1:8765")) is ProvisioningResult.Valid)
    }

    @Test
    fun `json string escapes are decoded`() {
        // The JSON has an escaped slash and a four digit unicode escape for A.
        // The backslashes are doubled because this is an ordinary Kotlin string.
        val text = "{\"version\":1,\"agent_url\":\"http:\\/\\/192.0.2.10:8765\"," +
            "\"token\":\"\\u0041bc123Def456Ghi789Jkl012Mno345Pq\"}"
        assertEquals(
            ProvisioningResult.Valid("http://192.0.2.10:8765", "Abc123Def456Ghi789Jkl012Mno345Pq"),
            parseProvisioning(text),
        )
    }

    // ---- malformed json ----

    @Test
    fun `truncated json is rejected`() {
        assertRejected(payload().dropLast(8))
        assertRejected("""{"version":1,"agent_url":"$url","token":"$token""")
        assertRejected("{")
    }

    @Test
    fun `empty or blank input is rejected`() {
        assertRejected("")
        assertRejected("   \n")
    }

    @Test
    fun `input that is not an object is rejected`() {
        assertRejected("[]")
        assertRejected("\"text\"")
        assertRejected("42")
    }

    @Test
    fun `anything after the object is rejected`() {
        assertRejected(payload() + " x")
        assertRejected(payload() + payload())
    }

    @Test
    fun `values that are not a string or a whole number are rejected`() {
        assertRejected("""{"version":1,"agent_url":"$url","token":null}""")
        assertRejected("""{"version":1,"agent_url":"$url","token":true}""")
        assertRejected("""{"version":1,"agent_url":["$url"],"token":"$token"}""")
        assertRejected("""{"version":1,"agent_url":{"a":"$url"},"token":"$token"}""")
        assertRejected("""{"version":1.5,"agent_url":"$url","token":"$token"}""")
    }

    @Test
    fun `a bad escape or a raw control character in a string is rejected`() {
        assertRejected("""{"version":1,"agent_url":"$url","token":"ab\qcd"}""")
        assertRejected("""{"version":1,"agent_url":"$url","token":"ab\u12"}""")
        assertRejected("{\"version\":1,\"agent_url\":\"$url\",\"token\":\"ab\ncd\"}")
    }

    @Test
    fun `a repeated key is rejected`() {
        assertRejected("""{"version":1,"version":1,"agent_url":"$url","token":"$token"}""")
    }

    // ---- shape ----

    @Test
    fun `a missing field is rejected and named`() {
        assertRejected("""{"version":1,"agent_url":"$url"}""", "token")
        assertRejected("""{"version":1,"token":"$token"}""", "agent_url")
        assertRejected("""{"agent_url":"$url","token":"$token"}""", "version")
    }

    @Test
    fun `an unknown key rejects the whole payload`() {
        assertRejected("""{"version":1,"agent_url":"$url","token":"$token","extra":"x"}""", "unexpected key")
    }

    @Test
    fun `a wrong version is rejected`() {
        assertRejected(payload(version = "2"), "version")
        assertRejected(payload(version = "0"), "version")
        assertRejected(payload(version = "\"1\""), "version")
    }

    @Test
    fun `a number too large to be a version or anything else is rejected`() {
        assertRejected("""{"version":12345678901234567890,"agent_url":"$url","token":"$token"}""")
    }

    @Test
    fun `a field of the wrong type is rejected`() {
        assertRejected("""{"version":1,"agent_url":"$url","token":12345}""", "token")
        assertRejected("""{"version":1,"agent_url":8765,"token":"$token"}""", "agent_url")
    }

    // ---- url ----

    @Test
    fun `an unsupported scheme is rejected`() {
        assertRejected(payload(agentUrl = "ftp://192.0.2.10:8765"), "scheme")
        assertRejected(payload(agentUrl = "javascript:alert(1)"), "scheme")
        assertRejected(payload(agentUrl = "file:///sdcard/x"), "scheme")
        assertRejected(payload(agentUrl = "192.0.2.10:8765"))
    }

    @Test
    fun `a url with no host is rejected`() {
        assertRejected(payload(agentUrl = "http://"))
        assertRejected(payload(agentUrl = "http://:8765"))
    }

    @Test
    fun `credentials in the url are rejected`() {
        assertRejected(payload(agentUrl = "http://user:pw@192.0.2.10:8765"), "credentials")
    }

    @Test
    fun `a port outside 1 to 65535 is rejected`() {
        assertRejected(payload(agentUrl = "http://192.0.2.10:0"), "port")
        assertRejected(payload(agentUrl = "http://192.0.2.10:70000"), "port")
    }

    @Test
    fun `a path, query or fragment is rejected`() {
        assertRejected(payload(agentUrl = "http://192.0.2.10:8765/admin"), "path")
        assertRejected(payload(agentUrl = "http://192.0.2.10:8765/?token=x"), "query")
        assertRejected(payload(agentUrl = "http://192.0.2.10:8765/#x"), "fragment")
    }

    @Test
    fun `a url with whitespace in it is rejected`() {
        assertRejected(payload(agentUrl = " http://192.0.2.10:8765"), "whitespace")
        assertRejected(payload(agentUrl = "http://192.0.2.10:8765 "), "whitespace")
        assertRejected(payload(agentUrl = "http://192.0.2.10 :8765"), "whitespace")
    }

    // ---- token ----

    @Test
    fun `an empty token is rejected`() {
        assertRejected(payload(tokenValue = ""), "token")
    }

    @Test
    fun `a token with a space or other whitespace is rejected`() {
        assertRejected(payload(tokenValue = "abc def"), "token")
        assertRejected(payload(tokenValue = "abc\\tdef"), "token")
    }

    @Test
    fun `a token with non-ascii or control characters is rejected`() {
        assertRejected(payload(tokenValue = "abc\\u00e9def"), "token")
        assertRejected(payload(tokenValue = "abc\\u0007def"), "token")
    }

    @Test
    fun `an oversized token is rejected`() {
        assertRejected(payload(tokenValue = "a".repeat(257)), "token")
        assertTrue(parseProvisioning(payload(tokenValue = "a".repeat(256))) is ProvisioningResult.Valid)
    }

    @Test
    fun `an oversized payload is rejected before it is parsed`() {
        assertRejected(" ".repeat(MAX_PROVISIONING_BYTES + 1) + payload(), "larger")
    }

    @Test
    fun `the host for the log is just the host`() {
        assertEquals("192.0.2.10", provisioningHost("http://192.0.2.10:8765"))
        assertEquals("desk-pc", provisioningHost("https://desk-pc/"))
    }

    // ---- the file ----

    @get:Rule
    val folder = TemporaryFolder()

    private fun provisionFile(): File = File(folder.root, PROVISIONING_FILE_NAME)

    @Test
    fun `no file means nothing to do`() {
        assertNull(consumeProvisioning(provisionFile()))
    }

    @Test
    fun `a valid file is applied and then deleted`() {
        provisionFile().writeText(payload())

        val consumed = consumeProvisioning(provisionFile())!!

        assertEquals(ProvisioningResult.Valid(url, token), consumed.result)
        assertTrue(consumed.deleted)
        assertFalse("file exists after consume", provisionFile().exists())
    }

    @Test
    fun `a malformed file is rejected and still deleted`() {
        provisionFile().writeText("""{"version":1,"agent_url":"$url"}""")

        val consumed = consumeProvisioning(provisionFile())!!

        assertTrue(consumed.result is ProvisioningResult.Rejected)
        assertTrue(consumed.deleted)
        assertFalse(provisionFile().exists())
    }

    @Test
    fun `a byte order mark from a windows editor is tolerated`() {
        provisionFile().writeBytes(byteArrayOf(0xEF.toByte(), 0xBB.toByte(), 0xBF.toByte()) + payload().toByteArray())

        val consumed = consumeProvisioning(provisionFile())!!

        assertEquals(ProvisioningResult.Valid(url, token), consumed.result)
    }

    @Test
    fun `a file that is not utf-8 is rejected`() {
        provisionFile().writeBytes(byteArrayOf(0x7B, 0xFF.toByte(), 0xFE.toByte(), 0x7D))

        val consumed = consumeProvisioning(provisionFile())!!

        assertTrue(consumed.result is ProvisioningResult.Rejected)
        assertFalse(provisionFile().exists())
    }

    @Test
    fun `an oversized file is rejected without being read whole, and deleted`() {
        provisionFile().writeBytes(ByteArray(MAX_PROVISIONING_BYTES * 4) { ' '.code.toByte() })

        val consumed = consumeProvisioning(provisionFile())!!

        assertTrue(consumed.result is ProvisioningResult.Rejected)
        assertFalse(provisionFile().exists())
    }
}
