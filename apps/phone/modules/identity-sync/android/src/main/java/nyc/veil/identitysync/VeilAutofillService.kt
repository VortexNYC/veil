package nyc.veil.identitysync

import android.app.PendingIntent
import android.app.assist.AssistStructure
import android.content.Intent
import android.os.CancellationSignal
import android.os.Handler
import android.os.Looper
import android.service.autofill.AutofillService
import android.service.autofill.Dataset
import android.service.autofill.FillCallback
import android.service.autofill.FillRequest
import android.service.autofill.FillResponse
import android.service.autofill.SaveCallback
import android.service.autofill.SaveInfo
import android.service.autofill.SaveRequest
import android.view.View
import android.view.autofill.AutofillId
import android.widget.RemoteViews
import org.json.JSONObject

// AutofillService is the classic Android credential surface — the OS hands us
// the focused screen's AssistStructure, we answer with datasets. Datasets
// carry metadata presentations only; the actual credential is never in the
// response — tapping a row fires FillAuthActivity (BiometricPrompt) which
// fetches the secret from origin and returns the real dataset. No match or
// no handoff → onSuccess(null), which tells the OS "nothing here".

class VeilAutofillService : AutofillService() {

    private class Form(val userId: AutofillId?, val passId: AutofillId?, val host: String?)

    override fun onFillRequest(request: FillRequest, signal: CancellationSignal, callback: FillCallback) {
        VaultStore.log("onFillRequest contexts=${request.fillContexts.size}")
        val structure = request.fillContexts.lastOrNull()?.structure
        val form = parse(structure) ?: run {
            VaultStore.log("fillRequest: no credential fields")
            callback.onSuccess(null); return
        }
        // Native apps have no webDomain — key on the package name instead.
        // With no URI match we still offer the password list: the dataset is
        // metadata-only and the secret still requires the biometric gate.
        val pkg = structure?.activityComponent?.packageName
        val host = form.host ?: pkg ?: ""
        var matches = VaultStore.matching(this, host)
        if (matches.isEmpty() && form.host == null) {
            matches = VaultStore.allPasswords(this)
        }
        // A FillResponse rides a 1MB Binder transaction — hundreds of
        // RemoteViews datasets fail to unparcel and the dropdown never
        // renders. Cap it.
        if (matches.size > 10) matches = matches.take(10)
        if (matches.isEmpty()) { callback.onSuccess(null); return }

        val builder = FillResponse.Builder()
        matches.forEachIndexed { idx, item ->
            val presentation = RemoteViews(packageName, R.layout.list_item_credential).apply {
                val label = "${item.optString("name")} — ${item.optString("login")}"
                setTextViewText(R.id.credential_label, label)
            }
            // For native apps there's no web origin to attest — send the
            // item's own URI so origin's URI check still validates.
            val itemHost = form.host ?: run {
                val uris = item.optJSONArray("uris")
                VaultStore.hostOf(uris?.optString(0) ?: "") ?: host
            }
            val intent = Intent(this, FillAuthActivity::class.java).apply {
                putExtra(FillAuthActivity.EXTRA_UUID, item.optString("uuid"))
                putExtra(FillAuthActivity.EXTRA_URL, "https://$itemHost")
                putExtra(FillAuthActivity.EXTRA_USER_ID, form.userId)
                putExtra(FillAuthActivity.EXTRA_PASS_ID, form.passId)
            }
            val sender = PendingIntent.getActivity(
                this, idx, intent,
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_CANCEL_CURRENT,
            ).intentSender
            val ds = Dataset.Builder()
            form.userId?.let { ds.setValue(it, null, presentation) }
            form.passId?.let { ds.setValue(it, null, presentation) }
            ds.setAuthentication(sender)
            builder.addDataset(ds.build())
        }
        VaultStore.log("fillRequest host=$host matches=${matches.size}")
        // Tell the OS we take saves — without SaveInfo it never calls
        // onSaveRequest and the post-submit offer never happens.
        builder.setSaveInfo(
            SaveInfo.Builder(
                SaveInfo.SAVE_DATA_TYPE_USERNAME or SaveInfo.SAVE_DATA_TYPE_PASSWORD,
                listOfNotNull(form.userId, form.passId).toTypedArray(),
            ).build()
        )
        callback.onSuccess(builder.build())
    }

    /// The OS captured a submitted sign-in — the values the user typed ride
    /// the fillContexts' autofillValues. Match host+login against the vault:
    /// an existing item is overwritten via PATCH (uuid survives), anything
    /// else creates. Fail closed like the fill path.
    override fun onSaveRequest(request: SaveRequest, callback: SaveCallback) {
        val structure = request.fillContexts.lastOrNull()?.structure
        val creds = credentials(structure)
        if (creds.password.isEmpty()) {
            VaultStore.log("saveRequest: no password captured")
            callback.onSuccess(); return
        }
        val pkg = structure?.activityComponent?.packageName ?: "unknown"
        val uri = creds.host?.let { "https://$it" } ?: "androidapp://$pkg"
        val name = creds.host ?: pkg
        // freshened() may hit the network to remint — the callback lands on
        // the main looper, so the whole save goes to a worker.
        kotlin.concurrent.thread {
            val existing = VaultStore.savedItem(this, creds.host ?: "", creds.user)
            val uuid = VaultStore.save(this, existing?.optString("name"),
                name, creds.user, creds.password, uri)
            Handler(Looper.getMainLooper()).post {
                if (uuid != null) {
                    VaultStore.log("saveRequest ok host=$name overwrite=${existing != null}")
                    callback.onSuccess()
                } else {
                    VaultStore.log("saveRequest failed")
                    callback.onFailure("Veil couldn't save")
                }
            }
        }
    }

    /// Field roles a credential form can hold. Chrome forwards
    /// autocomplete= as autofillHints; sites without it only expose
    /// htmlInfo (type/name/id), so check both.
    private class Roles(var user: AssistStructure.ViewNode? = null,
                        var pass: AssistStructure.ViewNode? = null,
                        var host: String? = null)

    private fun classify(node: AssistStructure.ViewNode): Int {
        val hints = node.autofillHints?.toList() ?: emptyList()
        val attrs = node.htmlInfo?.attributes ?: emptyList()
        val attr = { k: String -> attrs.firstOrNull { it.first == k }?.second ?: "" }
        val ident = (attr("type") + " " + attr("name") + " " + attr("id") + " " +
            (node.idEntry ?: "")).lowercase()
        if (View.AUTOFILL_HINT_PASSWORD in hints || "password" in ident) return 2
        if (View.AUTOFILL_HINT_USERNAME in hints ||
            View.AUTOFILL_HINT_EMAIL_ADDRESS in hints ||
            attr("type") == "email" ||
            ident.contains("email") || ident.contains("user") || ident.contains("login")) return 1
        return 0
    }

    private fun walk(structure: AssistStructure?): Roles? {
        if (structure == null) return null
        val r = Roles()
        fun visit(node: AssistStructure.ViewNode) {
            if (r.host == null) {
                val d = node.webDomain
                if (!d.isNullOrEmpty()) r.host = d
            }
            when (classify(node)) {
                2 -> if (r.pass == null) r.pass = node
                1 -> if (r.user == null) r.user = node
            }
            for (i in 0 until node.childCount) visit(node.getChildAt(i))
        }
        for (i in 0 until structure.windowNodeCount) {
            visit(structure.getWindowNodeAt(i).rootViewNode)
        }
        // Two-step forms show only one field — either half alone still counts.
        return if (r.user != null || r.pass != null) r else null
    }

    /// Walk the view tree for a username + password pair and the page host.
    private fun parse(structure: AssistStructure?): Form? {
        val r = walk(structure) ?: return null
        return Form(r.user?.autofillId, r.pass?.autofillId, r.host)
    }

    private class Captured(val user: String, val password: String, val host: String?)

    /// The values the user typed — save requests ride the same structure
    /// but carry autofillValue text where fill only needed the ids.
    private fun credentials(structure: AssistStructure?): Captured {
        val r = walk(structure) ?: return Captured("", "", null)
        return Captured(
            r.user?.autofillValue?.textValue?.toString() ?: "",
            r.pass?.autofillValue?.textValue?.toString() ?: "",
            r.host,
        )
    }
}
