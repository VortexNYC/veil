package nyc.veil.identitysync

import android.content.Context
import android.util.Log
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

// Mirrors the iOS appex's VaultStore: reads the handoff the containing app
// wrote (token + item metadata — never secrets) and calls the origin's fill
// endpoint only *after* the biometric gate passed. Failures are quiet —
// autofill must fail closed, not leak.

internal object VaultStore {
    private const val TAG = "veil-autofill"

    fun log(msg: String) = Log.i(TAG, msg)

    internal fun handoff(ctx: Context): JSONObject? = try {
        val f = handoffFile(ctx)
        if (f.exists()) JSONObject(f.readText()) else null
    } catch (_: Exception) { null }

    private fun expired(jwt: String): Boolean {
        val parts = jwt.split(".")
        if (parts.size < 2) return true
        return try {
            val pad = parts[1].let { it + "=".repeat((4 - it.length % 4) % 4) }
            val claims = JSONObject(
                String(android.util.Base64.decode(pad, android.util.Base64.URL_SAFE or android.util.Base64.NO_WRAP))
            )
            claims.optLong("exp", 0) <= System.currentTimeMillis() / 1000 + 15
        } catch (_: Exception) { true }
    }

    /// The handoff with a live bearer: an expired id_token remints through
    /// the refresh grant, and the rotated pair is written back so the app
    /// converges on it. Null means sign in again.
    private fun freshened(ctx: Context): JSONObject? {
        val h = handoff(ctx) ?: return null
        if (!expired(h.optString("token"))) return h
        val rt = h.optString("refresh")
        val iss = h.optString("issuer")
        if (rt.isEmpty() || iss.isEmpty()) { log("session expired, no refresh"); return null }
        return try {
            val conn = (URL("$iss/oauth2/token").openConnection() as HttpURLConnection).apply {
                requestMethod = "POST"
                connectTimeout = 10_000
                readTimeout = 15_000
                setRequestProperty("Content-Type", "application/x-www-form-urlencoded")
                doOutput = true
            }
            val form = "grant_type=refresh_token&client_id=veil&scope=openid+offline_access" +
                "&refresh_token=${java.net.URLEncoder.encode(rt, "UTF-8")}"
            conn.outputStream.use { it.write(form.toByteArray()) }
            if (conn.responseCode != 200) { log("refresh http ${conn.responseCode}"); return null }
            val body = JSONObject(conn.inputStream.use { s -> s.readBytes().decodeToString() })
            val id = body.optString("id_token")
            val refresh = body.optString("refresh_token")
            if (id.isEmpty() || refresh.isEmpty()) { log("refresh bad body"); return null }
            h.put("token", id)
            h.put("refresh", refresh)
            handoffFile(ctx).writeText(h.toString())
            log("session reminted")
            h
        } catch (e: Exception) {
            log("refresh error ${e.javaClass.simpleName}")
            null
        }
    }

    /// Vault items whose URI list contains `host`. Password kinds only —
    /// passkeys go through Credential Manager, not form autofill.
    internal fun matching(ctx: Context, host: String): List<JSONObject> {
        val h = handoff(ctx) ?: return emptyList()
        val items = h.optJSONArray("items") ?: return emptyList()
        val out = mutableListOf<JSONObject>()
        for (i in 0 until items.length()) {
            val it = items.optJSONObject(i) ?: continue
            val kind = it.optString("kind")
            if (kind != "api_key" && kind != "login") continue
            val uris = it.optJSONArray("uris") ?: continue
            for (j in 0 until uris.length()) {
                if (hostOf(uris.optString(j)) == host) { out.add(it); break }
            }
        }
        return out
    }

    /// Passkey items — CredMan matches by rpId in the RP's request JSON,
    /// not by page URIs.
    internal fun matchingPasskeys(ctx: Context, rpId: String): List<JSONObject> {
        val h = handoff(ctx) ?: return emptyList()
        val items = h.optJSONArray("items") ?: return emptyList()
        val out = mutableListOf<JSONObject>()
        for (i in 0 until items.length()) {
            val it = items.optJSONObject(i) ?: continue
            if (it.optString("kind") != "passkey") continue
            if (rpId.isEmpty() || it.optString("rpId") == rpId) out.add(it)
        }
        return out
    }

    /// Every password-kind item — the CredMan fallback when the calling app
    /// doesn't populate a web origin to match against.
    internal fun allPasswords(ctx: Context): List<JSONObject> {
        val h = handoff(ctx) ?: return emptyList()
        val items = h.optJSONArray("items") ?: return emptyList()
        val out = mutableListOf<JSONObject>()
        for (i in 0 until items.length()) {
            val it = items.optJSONObject(i) ?: continue
            val kind = it.optString("kind")
            if (kind == "api_key" || kind == "login") out.add(it)
        }
        return out
    }

    internal fun hostOf(uri: String): String? {
        val u = if (uri.startsWith("http")) uri else "https://$uri"
        return try { URL(u).host?.takeIf { it.isNotEmpty() } } catch (_: Exception) { null }
    }

    /// POST /v1/fill/logins — the only call that releases a secret. Caller
    /// must already have passed BiometricPrompt; the audit row lands
    /// server-side. Returns (login, password) or null on any failure.
    internal fun fill(ctx: Context, uuid: String, url: String): Pair<String, String>? {
        val h = freshened(ctx) ?: run { log("no session"); return null }
        val body = JSONObject().apply {
            put("uuid", uuid)
            put("url", url)
        }
        return try {
            val conn = (URL(h.getString("origin") + "/v1/fill/logins").openConnection() as HttpURLConnection).apply {
                requestMethod = "POST"
                connectTimeout = 10_000
                readTimeout = 15_000
                setRequestProperty("Authorization", "Bearer ${h.getString("token")}")
                setRequestProperty("Content-Type", "application/json")
                doOutput = true
            }
            conn.outputStream.use { it.write(body.toString().toByteArray()) }
            if (conn.responseCode != 200) {
                log("fill http ${conn.responseCode}")
                return null
            }
            val resp = JSONObject(conn.inputStream.use { s -> s.readBytes().decodeToString() })
            val entry = resp.optJSONArray("entries")?.optJSONObject(0)
            val login = entry?.optString("login")
            val password = entry?.optString("password")
            if (login.isNullOrEmpty() || password.isNullOrEmpty()) null else login to password
        } catch (e: Exception) {
            log("fill error ${e.javaClass.simpleName}")
            null
        }
    }

    /// The item that already covers host+login — an empty login can never
    /// match, so unknown users always create rather than overwrite.
    internal fun savedItem(ctx: Context, host: String, user: String): JSONObject? {
        if (user.isEmpty()) return null
        return matching(ctx, host).firstOrNull { it.optString("login") == user }
    }

    /// POST /v1/items or PATCH /v1/items/{name} — the same contract iOS
    /// uses. `existing` is an item name to overwrite: PATCH rotates the
    /// secret on the same row so no duplicate is made. Returns the item
    /// uuid or null on failure. Callers must be past the biometric gate
    /// or acting on a system-save request the user just confirmed.
    internal fun save(ctx: Context, existing: String?, name: String,
                      user: String, password: String, uri: String): String? {
        val h = freshened(ctx) ?: run { log("no session"); return null }
        val create = existing.isNullOrEmpty()
        val body = JSONObject().apply {
            put("login", user)
            put("secret", password)
            put("uri", uri)
            if (create) put("name", name)
        }
        val path = if (create) "/v1/items" else
            "/v1/items/" + java.net.URLEncoder.encode(existing, "UTF-8").replace("+", "%20")
        return try {
            val conn = (URL(h.getString("origin") + path).openConnection() as HttpURLConnection).apply {
                requestMethod = if (create) "POST" else "PATCH"
                connectTimeout = 10_000
                readTimeout = 15_000
                setRequestProperty("Authorization", "Bearer ${h.getString("token")}")
                setRequestProperty("Content-Type", "application/json")
                doOutput = true
            }
            conn.outputStream.use { it.write(body.toString().toByteArray()) }
            if (conn.responseCode != 200) {
                log("save http ${conn.responseCode}")
                return null
            }
            val id = JSONObject(conn.inputStream.use { s -> s.readBytes().decodeToString() })
                .optString("id").takeIf { it.isNotEmpty() } ?: return null
            // Reflect the save in the handoff's item cache — otherwise the
            // just-saved credential can't fill until the app syncs again.
            val items = h.optJSONArray("items") ?: org.json.JSONArray().also { h.put("items", it) }
            if (existing.isNullOrEmpty()) {
                items.put(JSONObject().apply {
                    put("uuid", id)
                    put("name", name)
                    put("login", user)
                    put("kind", "login")
                    put("uris", org.json.JSONArray().put(uri))
                })
            } else {
                for (i in 0 until items.length()) {
                    val it = items.optJSONObject(i) ?: continue
                    if (it.optString("name") == existing) { it.put("login", user); break }
                }
            }
            handoffFile(ctx).writeText(h.toString())
            id
        } catch (e: Exception) {
            log("save error ${e.javaClass.simpleName}")
            null
        }
    }

    /// WebAuthn ceremony through the origin — same contract as iOS. The
    /// caller passes the RP's publicKey request object verbatim; the reply's
    /// `response` is a PublicKeyCredential-shaped JSON ready to hand to
    /// Credential Manager. The biometric gate precedes this call.
    internal fun passkeys(ctx: Context, register: Boolean, origin: String, publicKey: JSONObject): String? {
        val h = freshened(ctx) ?: run { log("no session"); return null }
        val path = if (register) "/v1/fill/passkeys/register" else "/v1/fill/passkeys/get"
        val body = JSONObject().apply {
            put("origin", origin)
            put("publicKey", publicKey)
        }
        return try {
            val conn = (URL(h.getString("origin") + path).openConnection() as HttpURLConnection).apply {
                requestMethod = "POST"
                connectTimeout = 10_000
                readTimeout = 15_000
                setRequestProperty("Authorization", "Bearer ${h.getString("token")}")
                setRequestProperty("Content-Type", "application/json")
                doOutput = true
            }
            conn.outputStream.use { it.write(body.toString().toByteArray()) }
            if (conn.responseCode != 200) {
                log("passkeys http ${conn.responseCode}")
                return null
            }
            JSONObject(conn.inputStream.use { s -> s.readBytes().decodeToString() })
                .optJSONObject("response")?.toString()
        } catch (e: Exception) {
            log("passkeys error ${e.javaClass.simpleName}")
            null
        }
    }
}
