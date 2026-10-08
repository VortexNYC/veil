package nyc.veil.identitysync

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import org.json.JSONArray
import org.json.JSONObject
import java.io.File

// The Android half of the identity-sync bridge: the RN side hands us the
// bearer token plus item *metadata* (never secrets) and we drop the handoff
// file where VeilAutofillService reads it. Same process as the app — no
// app-group container needed the way iOS does.

private const val HANDOFF = "veil-autofill.json"

internal fun handoffFile(ctx: android.content.Context): File =
    File(ctx.applicationContext.filesDir, HANDOFF)

internal fun writeHandoff(ctx: android.content.Context, token: String, refresh: String,
                          origin: String, issuer: String, items: List<Map<String, Any?>>) {
    val arr = JSONArray()
    for (raw in items) {
        val o = JSONObject()
        o.put("uuid", raw["uuid"] as? String ?: continue)
        o.put("name", raw["name"] as? String ?: "")
        o.put("login", raw["login"] as? String ?: "")
        o.put("kind", raw["kind"] as? String ?: "")
        o.put("credId", raw["credId"] as? String)
        o.put("rpId", raw["rpId"] as? String)
        o.put("userHandle", raw["userHandle"] as? String)
        val uris = JSONArray()
        (raw["uris"] as? List<*>)?.forEach { uris.put(it?.toString()) }
        o.put("uris", uris)
        arr.put(o)
    }
    val h = JSONObject()
    h.put("token", token)
    h.put("refresh", refresh)
    h.put("origin", origin)
    h.put("issuer", issuer)
    h.put("items", arr)
    handoffFile(ctx).writeText(h.toString())
}

class IdentitySyncModule : Module() {
    override fun definition() = ModuleDefinition {
        Name("IdentitySync")

        AsyncFunction("syncAutofill") { token: String, refresh: String, origin: String,
                                        issuer: String, items: List<Map<String, Any?>> ->
            writeHandoff(appContext.reactContext!!, token, refresh, origin, issuer, items)
        }

        // Remint landed in JS — rotate the auth pair in place without
        // touching the item list the service renders.
        AsyncFunction("refreshAutofill") { token: String, refresh: String ->
            val f = handoffFile(appContext.reactContext!!)
            if (f.exists()) {
                val h = JSONObject(f.readText())
                h.put("token", token)
                h.put("refresh", refresh)
                f.writeText(h.toString())
            }
        }

        // The auth pair the service last knew — the app retries with it
        // when its own refresh token rotated out from under it.
        AsyncFunction("autofillAuth") {
            val h = VaultStore.handoff(appContext.reactContext!!)
            if (h == null) null else mapOf(
                "token" to h.optString("token", ""),
                "refresh" to h.optString("refresh", ""),
            )
        }

        AsyncFunction("clearAutofill") {
            handoffFile(appContext.reactContext!!).delete()
        }
    }
}
