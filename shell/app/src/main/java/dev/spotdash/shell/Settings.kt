package dev.spotdash.shell

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/**
 * The agent URL and token, stored encrypted.
 *
 * The token is a shared secret, and this device sits on a desk where anyone can
 * pick it up, so it is never written in the clear. Encrypted storage on Android
 * is keyed by the hardware backed keystore, which means the file is useless if
 * lifted off the device.
 */
class Settings(context: Context) {

    private val prefs: SharedPreferences = open(context)

    var agentUrl: String
        get() = prefs.getString(KEY_URL, "").orEmpty()
        set(value) = prefs.edit().putString(KEY_URL, value.trim()).apply()

    var token: String
        get() = prefs.getString(KEY_TOKEN, "").orEmpty()
        set(value) = prefs.edit().putString(KEY_TOKEN, value.trim()).apply()

    /**
     * Stores an address and token together, in one commit, and says whether it
     * worked.
     *
     * The two setters above are for the settings screen, where a person edits
     * one box at a time. A provisioning payload has to land whole or not at all,
     * because an address with the old token is a panel that loads and is
     * rejected, and a new token sent to the old address is a secret going
     * somewhere it was not meant to.
     */
    fun provision(agentUrl: String, token: String): Boolean =
        prefs.edit()
            .putString(KEY_URL, agentUrl.trim())
            .putString(KEY_TOKEN, token.trim())
            .commit()

    /** True when there is enough configuration to try loading the panel. */
    val isConfigured: Boolean
        get() = agentUrl.isNotBlank()

    /**
     * The URL to load, with the token attached.
     *
     * The token travels as a query parameter on this one request only. The page
     * reads it, strips it from its own address bar, and holds it in memory from
     * then on, so the shell hands it over once and forgets about it.
     */
    fun panelUrl(): String {
        val base = agentUrl.trim().trimEnd('/')
        if (base.isEmpty()) return ""
        if (token.isBlank()) return base
        val separator = if (base.contains('?')) "&" else "?"
        return base + separator + "token=" + android.net.Uri.encode(token)
    }

    /** The health endpoint, which needs no token. */
    fun healthUrl(): String {
        val base = agentUrl.trim().trimEnd('/')
        if (base.isEmpty()) return ""
        return "$base/health"
    }

    private companion object {
        const val FILE = "spotdash-settings"
        const val KEY_URL = "agent_url"
        const val KEY_TOKEN = "token"

        fun open(context: Context): SharedPreferences {
            return try {
                val key = MasterKey.Builder(context)
                    .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                    .build()
                EncryptedSharedPreferences.create(
                    context,
                    FILE,
                    key,
                    EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                    EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
                )
            } catch (error: Exception) {
                // A corrupted keystore would otherwise crash the launcher on
                // boot, which on a device with no other launcher means a brick
                // until it is reflashed. Falling back keeps the panel reachable
                // and the settings screen usable so the token can be re-entered.
                Log.e(TAG, "encrypted settings unavailable, falling back to plain storage", error)
                context.getSharedPreferences(FILE + "-plain", Context.MODE_PRIVATE)
            }
        }
    }
}
