package nyc.veil.identitysync

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.service.autofill.Dataset
import android.service.autofill.FillResponse
import android.view.autofill.AutofillId
import android.view.autofill.AutofillManager
import android.view.autofill.AutofillValue
import androidx.biometric.BiometricManager
import androidx.biometric.BiometricPrompt
import androidx.core.content.ContextCompat
import androidx.credentials.CreatePublicKeyCredentialResponse
import androidx.credentials.GetCredentialResponse
import androidx.credentials.PasswordCredential
import androidx.credentials.PublicKeyCredential
import androidx.credentials.provider.PendingIntentHandler
import androidx.fragment.app.FragmentActivity
import kotlin.concurrent.thread
import org.json.JSONObject

// The gate. Every credential surface lands here — an autofill dataset tap,
// a Credential Manager entry pick, a create request — all via PendingIntent.
// The activity exists only to run BiometricPrompt and then — and only then —
// call the origin for the credential. Every failure path ends in
// RESULT_CANCELED: denied, errored, or unavailable biometrics all mean the
// secret never leaves.

class FillAuthActivity : FragmentActivity() {

    companion object {
        const val EXTRA_MODE = "mode"
        const val EXTRA_UUID = "uuid"
        const val EXTRA_URL = "url"
        const val EXTRA_USER_ID = "userId"
        const val EXTRA_PASS_ID = "passId"
        const val EXTRA_REQUEST_JSON = "requestJson"

        const val MODE_AUTOFILL = 0
        const val MODE_GET_PASSWORD = 1
        const val MODE_GET_PASSKEY = 2
        const val MODE_CREATE_PASSKEY = 3
    }

    private var mode = MODE_AUTOFILL
    private var uuid = ""
    private var url = ""

    @Suppress("DEPRECATION")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        mode = intent.getIntExtra(EXTRA_MODE, MODE_AUTOFILL)
        uuid = intent.getStringExtra(EXTRA_UUID) ?: ""
        url = intent.getStringExtra(EXTRA_URL) ?: ""
        VaultStore.log("auth: launch mode=$mode")
        val userId = intent.getParcelableExtra<AutofillId>(EXTRA_USER_ID)
        val passId = intent.getParcelableExtra<AutofillId>(EXTRA_PASS_ID)

        val valid = when (mode) {
            MODE_AUTOFILL ->
                uuid.isNotEmpty() && url.isNotEmpty() && (userId != null || passId != null)
            MODE_GET_PASSWORD -> uuid.isNotEmpty() && url.isNotEmpty()
            MODE_GET_PASSKEY ->
                uuid.isNotEmpty() &&
                    PendingIntentHandler.retrieveProviderGetCredentialRequest(intent) != null
            MODE_CREATE_PASSKEY ->
                PendingIntentHandler.retrieveProviderCreateCredentialRequest(intent) != null
            else -> false
        }
        if (!valid) {
            VaultStore.log("auth: invalid request mode=$mode")
            finishCancel(); return
        }

        val prompt = BiometricPrompt(this, ContextCompat.getMainExecutor(this),
            object : BiometricPrompt.AuthenticationCallback() {
                override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                    release(userId, passId)
                }
                override fun onAuthenticationError(code: Int, errString: CharSequence) {
                    VaultStore.log("biometric error $code")
                    finishCancel()
                }
                override fun onAuthenticationFailed() {
                    // Bad scan — the OS keeps the prompt up; nothing to do.
                }
            })

        val info = BiometricPrompt.PromptInfo.Builder()
            .setTitle("Veil")
            .setSubtitle("Unlock to fill this credential")
            .setAllowedAuthenticators(
                BiometricManager.Authenticators.BIOMETRIC_STRONG or
                    BiometricManager.Authenticators.DEVICE_CREDENTIAL)
            .build()
        prompt.authenticate(info)
    }

    private fun release(userId: AutofillId?, passId: AutofillId?) {
        when (mode) {
            MODE_AUTOFILL -> releaseAutofill(userId, passId)
            MODE_GET_PASSWORD -> releasePassword()
            MODE_GET_PASSKEY -> releasePasskeyGet()
            MODE_CREATE_PASSKEY -> releasePasskeyCreate()
            else -> finishCancel()
        }
    }

    private fun releaseAutofill(userId: AutofillId?, passId: AutofillId?) {
        thread {
            val cred = VaultStore.fill(this, uuid, url)
            if (cred == null) {
                runOnUiThread { finishCancel() }
                return@thread
            }
            val ds = Dataset.Builder()
            userId?.let { ds.setValue(it, AutofillValue.forText(cred.first)) }
            passId?.let { ds.setValue(it, AutofillValue.forText(cred.second)) }
            // A bare Dataset result autofills immediately; a FillResponse would
            // just present another picker row.
            val reply = Intent().putExtra(AutofillManager.EXTRA_AUTHENTICATION_RESULT, ds.build())
            runOnUiThread {
                VaultStore.log("fill ok uuid=${uuid.take(8)}")
                setResult(Activity.RESULT_OK, reply)
                finish()
            }
        }
    }

    private fun releasePassword() {
        thread {
            val cred = VaultStore.fill(this, uuid, url)
            runOnUiThread {
                if (cred == null) { finishCancel(); return@runOnUiThread }
                val result = Intent()
                PendingIntentHandler.setGetCredentialResponse(
                    result,
                    GetCredentialResponse(PasswordCredential(cred.first, cred.second)),
                )
                VaultStore.log("credman fill ok uuid=${uuid.take(8)}")
                setResult(Activity.RESULT_OK, result)
                finish()
            }
        }
    }

    private fun releasePasskeyGet() {
        val req = PendingIntentHandler.retrieveProviderGetCredentialRequest(intent)
        val origin = VeilCredentialProviderService.callingOrigin(req?.callingAppInfo)
            ?: "https://$url"
        val requestJson = intent.getStringExtra(EXTRA_REQUEST_JSON) ?: ""
        thread {
            val body = try { JSONObject(requestJson) } catch (_: Exception) { null }
            val json = body?.let { VaultStore.passkeys(this, register = false, origin, it) }
            runOnUiThread {
                if (json == null) { finishCancel(); return@runOnUiThread }
                val merged = webAuthnJson(json, "webauthn.get", body?.optString("challenge") ?: "", origin)
                val result = Intent()
                PendingIntentHandler.setGetCredentialResponse(
                    result,
                    GetCredentialResponse(PublicKeyCredential(merged)),
                )
                VaultStore.log("credman passkey ok uuid=${uuid.take(8)}")
                setResult(Activity.RESULT_OK, result)
                finish()
            }
        }
    }

    private fun releasePasskeyCreate() {
        val req = PendingIntentHandler.retrieveProviderCreateCredentialRequest(intent)
        val create = req?.callingRequest as? androidx.credentials.CreatePublicKeyCredentialRequest
        val body = try { JSONObject(create?.requestJson ?: "") } catch (_: Exception) { null }
        val origin = VeilCredentialProviderService.callingOrigin(req?.callingAppInfo)
            ?: body?.optJSONObject("rp")?.optString("id")?.let { "https://$it" }
        if (create == null || body == null || origin.isNullOrEmpty()) {
            VaultStore.log("credman register bail create=${create != null} body=${body != null} origin=$origin")
            finishCancel(); return
        }
        thread {
            val json = VaultStore.passkeys(this, register = true, origin, body)
            runOnUiThread {
                if (json == null) { finishCancel(); return@runOnUiThread }
                val merged = webAuthnJson(json, "webauthn.create", body.optString("challenge"), origin)
                val result = Intent()
                PendingIntentHandler.setCreateCredentialResponse(
                    result,
                    CreatePublicKeyCredentialResponse(merged),
                )
                VaultStore.log("credman register ok")
                setResult(Activity.RESULT_OK, result)
                finish()
            }
        }
    }

    // The framework validates the credential JSON against the WebAuthn
    // schema — clientDataJSON and clientExtensionResults must be present.
    // clientDataHash in the request was over the standard clientData
    // string, so reconstruct exactly that and hand it back.
    private fun webAuthnJson(json: String, type: String, challenge: String, origin: String): String = try {
        val clientData = JSONObject().apply {
            put("type", type)
            put("challenge", challenge)
            put("origin", origin)
            put("crossOrigin", false)
        }.toString().toByteArray(Charsets.UTF_8)
        val b64 = android.util.Base64.encodeToString(
            clientData,
            android.util.Base64.URL_SAFE or android.util.Base64.NO_WRAP or android.util.Base64.NO_PADDING,
        )
        JSONObject(json).apply {
            optJSONObject("response")?.put("clientDataJSON", b64)
            if (!has("clientExtensionResults")) put("clientExtensionResults", JSONObject())
        }.toString()
    } catch (_: Exception) { json }

    private fun finishCancel() {
        setResult(Activity.RESULT_CANCELED)
        finish()
    }
}
