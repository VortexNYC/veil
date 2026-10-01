package nyc.veil.identitysync

import android.app.PendingIntent
import android.content.Intent
import android.os.CancellationSignal
import android.os.OutcomeReceiver
import androidx.credentials.exceptions.ClearCredentialException
import androidx.credentials.exceptions.ClearCredentialUnsupportedException
import androidx.credentials.exceptions.CreateCredentialException
import androidx.credentials.exceptions.GetCredentialException
import androidx.credentials.provider.BeginCreateCredentialRequest
import androidx.credentials.provider.BeginCreateCredentialResponse
import androidx.credentials.provider.BeginCreatePublicKeyCredentialRequest
import androidx.credentials.provider.BeginGetCredentialRequest
import androidx.credentials.provider.BeginGetCredentialResponse
import androidx.credentials.provider.BeginGetPasswordOption
import androidx.credentials.provider.BeginGetPublicKeyCredentialOption
import androidx.credentials.provider.CallingAppInfo
import androidx.credentials.provider.CredentialEntry
import androidx.credentials.provider.CreateEntry
import androidx.credentials.provider.CredentialProviderService
import androidx.credentials.provider.PasswordCredentialEntry
import androidx.credentials.provider.ProviderClearCredentialStateRequest
import androidx.credentials.provider.PublicKeyCredentialEntry
import org.json.JSONObject

// Credential Manager provider — the real credential surface on Android 14+.
// Chrome and modern apps call CredentialManager directly (autofill dumpsys
// showed their requests land on com.google.android.gms, never on the
// classic AutofillService), so this service is what makes Veil visible to
// them. Same contract as everywhere else: entries carry metadata only,
// every release runs through FillAuthActivity's biometric gate, and the
// secret is fetched from origin after approval. No handoff → empty
// response, which fails closed like an unmatched host.

class VeilCredentialProviderService : CredentialProviderService() {

    override fun onBeginGetCredentialRequest(
        request: BeginGetCredentialRequest,
        cancellationSignal: CancellationSignal,
        callback: OutcomeReceiver<BeginGetCredentialResponse, GetCredentialException>,
    ) {
        val info = request.callingAppInfo
        val host = VaultStore.hostOf(callingOrigin(info) ?: "")
        VaultStore.log("beginGet pkg=${info?.packageName} populated=${info?.isOriginPopulated()}")
        for (opt in request.beginGetCredentialOptions) {
            val entries = mutableListOf<CredentialEntry>()
            when (opt) {
                is BeginGetPasswordOption -> {
                    val items = if (host == null) {
                        VaultStore.allPasswords(this)
                    } else {
                        VaultStore.matching(this, host)
                    }
                    items.forEach { entries.add(passwordEntry(it, host ?: firstUri(it), opt)) }
                }
                is BeginGetPublicKeyCredentialOption -> {
                    val rpId = try {
                        JSONObject(opt.requestJson).optString("rpId")
                    } catch (_: Exception) { "" }
                    VaultStore.matchingPasskeys(this, rpId).forEach {
                        entries.add(passkeyEntry(it, opt))
                    }
                }
            }
            if (entries.isNotEmpty()) {
                VaultStore.log("beginGet host=$host entries=${entries.size}")
                callback.onResult(
                    BeginGetCredentialResponse.Builder().apply {
                        entries.forEach { addCredentialEntry(it) }
                    }.build()
                )
                return
            }
        }
        VaultStore.log("beginGet host=$host entries=0")
        callback.onResult(BeginGetCredentialResponse())
    }

    override fun onBeginCreateCredentialRequest(
        request: BeginCreateCredentialRequest,
        cancellationSignal: CancellationSignal,
        callback: OutcomeReceiver<BeginCreateCredentialResponse, CreateCredentialException>,
    ) {
        VaultStore.log("beginCreate type=${request.type}")
        if (request !is BeginCreatePublicKeyCredentialRequest) {
            callback.onResult(BeginCreateCredentialResponse())
            return
        }
        val intent = Intent(this, FillAuthActivity::class.java).apply {
            putExtra(FillAuthActivity.EXTRA_MODE, FillAuthActivity.MODE_CREATE_PASSKEY)
        }
        val pi = PendingIntent.getActivity(
            this, 1, intent,
            PendingIntent.FLAG_MUTABLE or PendingIntent.FLAG_CANCEL_CURRENT,
        )
        callback.onResult(
            BeginCreateCredentialResponse.Builder().apply {
                addCreateEntry(CreateEntry.Builder("Save passkey in Veil", pi).build())
            }.build()
        )
    }

    override fun onClearCredentialStateRequest(
        request: ProviderClearCredentialStateRequest,
        cancellationSignal: CancellationSignal,
        callback: OutcomeReceiver<Void?, ClearCredentialException>,
    ) {
        callback.onError(ClearCredentialUnsupportedException())
    }

    private fun passwordEntry(item: JSONObject, host: String, opt: BeginGetPasswordOption): CredentialEntry {
        val login = item.optString("login").ifEmpty { item.optString("name") }
        val intent = Intent(this, FillAuthActivity::class.java).apply {
            putExtra(FillAuthActivity.EXTRA_MODE, FillAuthActivity.MODE_GET_PASSWORD)
            putExtra(FillAuthActivity.EXTRA_UUID, item.optString("uuid"))
            putExtra(FillAuthActivity.EXTRA_URL, "https://$host")
        }
        val pi = PendingIntent.getActivity(
            this, intent.hashCode(), intent,
            PendingIntent.FLAG_MUTABLE or PendingIntent.FLAG_CANCEL_CURRENT,
        )
        return PasswordCredentialEntry.Builder(this, login, pi, opt)
            .setDisplayName(login)
            .build()
    }

    private fun passkeyEntry(item: JSONObject, opt: BeginGetPublicKeyCredentialOption): CredentialEntry {
        val intent = Intent(this, FillAuthActivity::class.java).apply {
            putExtra(FillAuthActivity.EXTRA_MODE, FillAuthActivity.MODE_GET_PASSKEY)
            putExtra(FillAuthActivity.EXTRA_UUID, item.optString("uuid"))
            putExtra(FillAuthActivity.EXTRA_REQUEST_JSON, opt.requestJson)
            putExtra(FillAuthActivity.EXTRA_URL, item.optString("rpId"))
        }
        val pi = PendingIntent.getActivity(
            this, intent.hashCode(), intent,
            PendingIntent.FLAG_MUTABLE or PendingIntent.FLAG_CANCEL_CURRENT,
        )
        return PublicKeyCredentialEntry.Builder(
            this, item.optString("login").ifEmpty { item.optString("name") }, pi, opt
        ).build()
    }

    private fun firstUri(item: JSONObject): String {
        val uris = item.optJSONArray("uris")
        val u = uris?.optString(0) ?: "veil.nyc"
        return VaultStore.hostOf(u) ?: "veil.nyc"
    }

    companion object {
        /// getOrigin(privilegedAllowlist) only returns the web origin when the
        /// caller is in our privileged-app allowlist. Chrome is signed with
        /// Google's release cert.
        private const val PRIVILEGED_ALLOWLIST = """{"apps":[{"type":"android_signing_cert","info":{"package_name":"com.android.chrome","signatures":[{"build":"release","cert_fingerprint_sha256":"F0:FD:6C:5B:41:0F:25:CB:25:C3:B5:33:46:C8:97:2F:AE:30:F8:EE:74:11:DF:91:04:80:3A:D9:98:EB"}]}}]}"""

        internal fun callingOrigin(info: CallingAppInfo?): String? {
            if (info == null || !info.isOriginPopulated()) return null
            return try { info.getOrigin(PRIVILEGED_ALLOWLIST) } catch (_: Exception) { null }
        }
    }
}
