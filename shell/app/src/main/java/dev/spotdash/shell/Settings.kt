package dev.spotdash.shell

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey

/** The agent URL and token, stored encrypted. The token is a shared secret on a device anyone can pick up. */
class Settings(context: Context) {

    private val prefs: SharedPreferences = open(context)

    var agentUrl: String
        get() = prefs.getString(KEY_URL, "").orEmpty()
        set(value) = prefs.edit().putString(KEY_URL, value.trim()).apply()

    var token: String
        get() = prefs.getString(KEY_TOKEN, "").orEmpty()
        set(value) = prefs.edit().putString(KEY_TOKEN, value.trim()).apply()

    /** Stores an address and token in one commit, so a payload lands whole or not at all. */
    fun provision(agentUrl: String, token: String): Boolean =
        prefs.edit()
            .putString(KEY_URL, agentUrl.trim())
            .putString(KEY_TOKEN, token.trim())
            .commit()

    /** True when there is enough configuration to try loading the panel. */
    val isConfigured: Boolean
        get() = agentUrl.isNotBlank()

    /** The URL to load, with the token as a query parameter on this one request. The page then keeps it in memory. */
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
                // A corrupted keystore would crash the launcher at boot, and with
                // no other launcher that bricks the device. Plain storage keeps
                // the settings screen usable so the token can be re-entered.
                Log.e(TAG, "encrypted settings unavailable, falling back to plain storage", error)
                context.getSharedPreferences(FILE + "-plain", Context.MODE_PRIVATE)
            }
        }
    }
}
