package dev.spotdash.shell

import android.app.Dialog
import android.graphics.Color
import android.os.Bundle
import android.text.InputType
import android.view.Gravity
import android.view.ViewGroup
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatDialogFragment

/** The settings screen: agent URL, token and a way into the system Wi-Fi settings. Reached by the long press. */
class SettingsSheet(
    private val onWifi: () -> Unit = {},
    private val onClosed: (saved: Boolean) -> Unit = {},
) : AppCompatDialogFragment() {

    override fun onCreateDialog(savedInstanceState: Bundle?): Dialog {
        val activity = requireActivity()
        val settings = Settings(activity)
        val density = resources.displayMetrics.density
        fun dp(value: Int) = (value * density).toInt()

        val urlField = EditText(activity).apply {
            hint = getString(R.string.settings_url_hint)
            setText(settings.agentUrl)
            inputType = InputType.TYPE_TEXT_VARIATION_URI
            setSingleLine()
            setTextColor(Color.parseColor("#EDF1EE"))
            setHintTextColor(Color.parseColor("#5A6A6D"))
            textSize = 14f
        }

        val tokenField = EditText(activity).apply {
            hint = getString(R.string.settings_token_hint)
            setText(settings.token)
            // Visible: a masked field is miserable to type accurately on a 480px
            // circle.
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD
            setSingleLine()
            setTextColor(Color.parseColor("#EDF1EE"))
            setHintTextColor(Color.parseColor("#5A6A6D"))
            textSize = 14f
        }

        val title = TextView(activity).apply {
            text = getString(R.string.settings_title)
            setTextColor(Color.parseColor("#EDF1EE"))
            textSize = 17f
            gravity = Gravity.CENTER
        }

        // The shell is the launcher, so this is the only way to the system Wi-Fi
        // settings without adb. It works with nothing configured.
        val wifiButton = Button(activity).apply {
            text = getString(R.string.settings_wifi)
            setOnClickListener { onWifi() }
        }

        val saveButton = Button(activity).apply {
            text = getString(R.string.settings_save)
        }
        val cancelButton = Button(activity).apply {
            text = getString(R.string.settings_cancel)
        }

        val buttons = LinearLayout(activity).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER
            addView(cancelButton)
            addView(saveButton)
        }

        val content = LinearLayout(activity).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(Color.parseColor("#0A0C0C"))
            setPadding(dp(20), dp(20), dp(20), dp(16))
            layoutParams = ViewGroup.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.WRAP_CONTENT,
            )
            addView(title)
            addView(urlField)
            addView(tokenField)
            addView(wifiButton)
            addView(buttons)
        }

        val dialog = Dialog(activity, R.style.Theme_Spotdash_Dialog)
        dialog.setContentView(content)
        dialog.setCanceledOnTouchOutside(false)

        saveButton.setOnClickListener {
            settings.agentUrl = urlField.text.toString()
            settings.token = tokenField.text.toString()
            dialog.dismiss()
            onClosed(true)
        }
        cancelButton.setOnClickListener {
            dialog.dismiss()
            onClosed(false)
        }

        return dialog
    }

    companion object {
        const val TAG = "spotdash-settings"
    }
}
