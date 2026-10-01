package nyc.veil.identitysync

import android.app.PendingIntent
import android.app.assist.AssistStructure
import android.content.Intent
import android.os.CancellationSignal
import android.service.autofill.AutofillService
import android.service.autofill.Dataset
import android.service.autofill.FillCallback
import android.service.autofill.FillRequest
import android.service.autofill.FillResponse
import android.service.autofill.SaveCallback
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
        callback.onSuccess(builder.build())
    }

    /// Save requests write nothing — adding items stays in the app. Returning
    /// success without a SaveInfo is "we heard you, nothing to do".
    override fun onSaveRequest(request: SaveRequest, callback: SaveCallback) {
        callback.onSuccess()
    }

    /// Walk the view tree for a username + password pair and the page host.
    /// Browsers set webDomain/webScheme on the structure's window; native
    /// forms expose autofillHints or htmlInfo type=password.
    private fun parse(structure: AssistStructure?): Form? {
        if (structure == null) return null
        var user: AutofillId? = null
        var pass: AutofillId? = null
        var host: String? = null

        fun visit(node: AssistStructure.ViewNode) {
            if (host == null) {
                val d = node.webDomain
                if (!d.isNullOrEmpty()) host = d
            }
            if (node.autofillType == android.view.View.AUTOFILL_TYPE_TEXT) {
                VaultStore.log("node id=${node.idEntry} hints=${node.autofillHints?.joinToString(",")} " +
                    "domain=${node.webDomain} isAutofillable=${node.autofillType}")
            }
            val hints = node.autofillHints?.toList() ?: emptyList()
            // Chrome forwards autocomplete= as autofillHints; sites without
            // it only expose htmlInfo (type/name/id), so check both.
            val attrs = node.htmlInfo?.attributes ?: emptyList()
            val attr = { k: String -> attrs.firstOrNull { it.first == k }?.second ?: "" }
            val ident = (attr("type") + " " + attr("name") + " " + attr("id") + " " +
                (node.idEntry ?: "")).lowercase()
            if (pass == null && (View.AUTOFILL_HINT_PASSWORD in hints || "password" in ident)) {
                pass = node.autofillId
            }
            if (user == null && (View.AUTOFILL_HINT_USERNAME in hints ||
                    View.AUTOFILL_HINT_EMAIL_ADDRESS in hints ||
                    attr("type") == "email" ||
                    ident.contains("email") || ident.contains("user") || ident.contains("login"))) {
                user = node.autofillId
            }
            for (i in 0 until node.childCount) visit(node.getChildAt(i))
        }

        for (i in 0 until structure.windowNodeCount) {
            visit(structure.getWindowNodeAt(i).rootViewNode)
        }
        // Two-step forms show only one field — either half alone still fills.
        if (user != null || pass != null) return Form(user, pass, host)
        return null
    }
}
