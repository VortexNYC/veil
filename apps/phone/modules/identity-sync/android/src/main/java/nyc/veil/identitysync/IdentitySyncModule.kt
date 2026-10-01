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

internal fun writeHandoff(ctx: android.content.Context, token: String, origin: String, items: List<Map<String, Any?>>) {
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
    h.put("origin", origin)
    h.put("items", arr)
    handoffFile(ctx).writeText(h.toString())
}

class IdentitySyncModule : Module() {
    override fun definition() = ModuleDefinition {
        Name("IdentitySync")

        AsyncFunction("syncAutofill") { token: String, origin: String, items: List<Map<String, Any?>> ->
            writeHandoff(appContext.reactContext!!, token, origin, items)
        }

        AsyncFunction("clearAutofill") {
            handoffFile(appContext.reactContext!!).delete()
        }
    }
}
