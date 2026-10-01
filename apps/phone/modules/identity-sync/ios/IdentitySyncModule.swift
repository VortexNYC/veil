//
//  IdentitySyncModule.swift
//  Veil (containing app side)
//
//  Writes the AutoFill handoff into the shared app-group container and
//  pushes credential identities to ASCredentialIdentityStore so iOS can
//  offer Veil items in QuickType. Metadata only — the appex fetches the
//  secret from origin after Face ID. Same shape as the macOS app's
//  IdentitySync, minus the bridge daemon (iOS has no unix socket).
//

import ExpoModulesCore
import AuthenticationServices

public class IdentitySyncModule: Module {

    private static let appGroup = "group.nyc.veil.phone"
    private static let handoffName = "veil-autofill.json"

    private static func handoffURL() -> URL? {
        FileManager.default
            .containerURL(forSecurityApplicationGroupIdentifier: appGroup)?
            .appendingPathComponent(handoffName)
    }

    public func definition() -> ModuleDefinition {
        Name("IdentitySync")

        AsyncFunction("syncAutofill") { (token: String, origin: String, items: [[String: Any]]) in
            guard let dir = FileManager.default
                .containerURL(forSecurityApplicationGroupIdentifier: Self.appGroup)
            else {
                throw NSError(domain: "nyc.veil.identity-sync", code: 1,
                              userInfo: [NSLocalizedDescriptionKey: "app group container unavailable"])
            }
            let handoff: [String: Any] = ["token": token, "origin": origin, "items": items]
            let data = try JSONSerialization.data(withJSONObject: handoff)
            try data.write(to: dir.appendingPathComponent(Self.handoffName),
                           options: [.atomic, .completeFileProtection])

            let passwords: [ASPasswordCredentialIdentity] = items.compactMap { raw in
                // The vault's password kind is "api_key" (ItemAPIKey — login
                // + password envelope). Keep "login" so older/other clients
                // still surface.
                guard ["api_key", "login"].contains((raw["kind"] as? String) ?? "api_key"),
                      let uuid = raw["uuid"] as? String, !uuid.isEmpty,
                      let user = raw["login"] as? String, !user.isEmpty,
                      let host = Self.firstHost(raw["uris"] as? [String] ?? [])
                else { return nil }
                return ASPasswordCredentialIdentity(
                    serviceIdentifier: ASCredentialServiceIdentifier(identifier: host, type: .domain),
                    user: user,
                    recordIdentifier: uuid,
                )
            }
            Self.replaceIdentities(passwords: passwords, items: items)
        }

        AsyncFunction("clearAutofill") {
            if let url = Self.handoffURL() {
                try? FileManager.default.removeItem(at: url)
            }
            Self.replaceIdentities(passwords: [], items: [])
        }
    }

    /// replaceCredentialIdentities is iOS 17+ — it takes the widened
    /// ASCredentialIdentity set, so passkeys ride the same replace. Below
    /// that it's remove-all then save, passwords only (passkey identities
    /// are an iOS 17 API).
    private static func replaceIdentities(passwords: [ASPasswordCredentialIdentity], items: [[String: Any]]) {
        let store = ASCredentialIdentityStore.shared
        if #available(iOS 17.0, *) {
            var all: [ASCredentialIdentity] = passwords
            for raw in items {
                guard (raw["kind"] as? String) == "passkey",
                      let uuid = raw["uuid"] as? String, !uuid.isEmpty,
                      let rpID = raw["rpId"] as? String, !rpID.isEmpty,
                      let credB64 = raw["credId"] as? String,
                      let credID = Self.data(b64url: credB64), !credID.isEmpty,
                      let handleB64 = raw["userHandle"] as? String,
                      let handle = Self.data(b64url: handleB64), !handle.isEmpty
                else { continue }
                let user = (raw["login"] as? String) ?? (raw["name"] as? String) ?? "passkey"
                all.append(ASPasskeyCredentialIdentity(
                    relyingPartyIdentifier: rpID,
                    userName: user,
                    credentialID: credID,
                    userHandle: handle,
                    recordIdentifier: uuid,
                ))
            }
            store.replaceCredentialIdentities(all) { _, _ in }
        } else {
            store.removeAllCredentialIdentities { _, _ in
                store.saveCredentialIdentities(passwords) { _, _ in }
            }
        }
    }

    /// cred_id/user_handle ride the handoff as unpadded base64url (the
    /// WebAuthn encoding); Data's codec wants the padded standard alphabet.
    private static func data(b64url: String) -> Data? {
        var s = b64url
            .replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")
        let pad = (4 - s.count % 4) % 4
        if pad > 0 { s += String(repeating: "=", count: pad) }
        return Data(base64Encoded: s)
    }

    /// uris may be bare hosts or full URLs — the identity store wants the
    /// domain form (matching ASCredentialServiceIdentifier .domain).
    private static func firstHost(_ uris: [String]) -> String? {
        for u in uris {
            let s = u.hasPrefix("http") ? u : "https://" + u
            if let host = URL(string: s)?.host, !host.isEmpty {
                return host
            }
        }
        return nil
    }
}
