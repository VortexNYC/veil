//
//  IdentitySync.swift
//  Veil
//
//  Feeds ASCredentialIdentityStore so Veil logins appear as inline
//  suggestions in Safari and native fields without opening our UI —
//  the system matches by service domain, we supply (host, user, uuid).
//  Metadata only: no secret is ever written to the store; tapping a
//  suggestion routes through the appex and the host's Touch ID confirm.
//

import Cocoa
import AuthenticationServices

final class IdentitySync {
    static let shared = IdentitySync()

    private var timer: Timer?
    private var lastSyncedKeys: Set<String>? = nil

    /// Host sends unpadded base64url for credId/userHandle.
    private static func data(b64url: String) -> Data? {
        var s = b64url
            .replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")
        let pad = (4 - s.count % 4) % 4
        if pad > 0 { s += String(repeating: "=", count: pad) }
        return Data(base64Encoded: s)
    }

    /// Sync on launch and every few minutes — cheap `list` over the bridge.
    /// The store write is idempotent; replace-all keeps stale entries out.
    func start() {
        timer = Timer.scheduledTimer(withTimeInterval: 300, repeats: true) { [weak self] _ in
            self?.sync()
        }
        sync()
    }

    func sync() {
        Task {
            let state = await ASCredentialIdentityStore.shared.state()
            guard state.isEnabled else {
                return
            }
            let prevKeys = self.lastSyncedKeys
            await Task.detached {
                let entries: [[String: Any]]
                do {
                    entries = try await FillBridge.shared.list()
                } catch {
                    return
                }
                var seen = Set<String>()
                var identities: [any ASCredentialIdentity] = []
                for e in entries {
                    let kind = e["kind"] as? String
                    guard let uuid = e["uuid"] as? String, !uuid.isEmpty else { continue }
                    if kind == "login" {
                        let user = e["login"] as? String ?? ""
                        let uris = e["uris"] as? [String] ?? []
                        for raw in uris {
                            // app:// entries are native-app bindings — the system
                            // store only understands web service identifiers.
                            guard let host = URL(string: raw)?.host?.lowercased(),
                                  let scheme = URL(string: raw)?.scheme,
                                  scheme == "http" || scheme == "https"
                            else { continue }
                            guard seen.insert(host + "|" + user + "|" + uuid).inserted else { continue }
                            identities.append(ASPasswordCredentialIdentity(
                                serviceIdentifier: ASCredentialServiceIdentifier(
                                    identifier: host, type: .domain,
                                ),
                                user: user,
                                recordIdentifier: uuid,
                            ))
                        }
                        // macOS 15 one-time-code AutoFill: logins carrying an
                        // authenticator also surface in OTP fields. Metadata
                        // only — the code is minted by the host at pick time.
                        if #available(macOS 15.0, *), (e["hasTotp"] as? Bool) == true {
                            let label = e["name"] as? String ?? user
                            for raw in uris {
                                guard let host = URL(string: raw)?.host?.lowercased(),
                                      let scheme = URL(string: raw)?.scheme,
                                      scheme == "http" || scheme == "https"
                                else { continue }
                                guard seen.insert("otp|" + host + "|" + user + "|" + uuid).inserted else { continue }
                                identities.append(ASOneTimeCodeCredentialIdentity(
                                    serviceIdentifier: ASCredentialServiceIdentifier(
                                        identifier: host, type: .domain,
                                    ),
                                    label: label.isEmpty ? user : label,
                                    recordIdentifier: uuid,
                                ))
                            }
                        }
                    } else if kind == "passkey" {
                        // rpId + credId + userHandle come from the replica's
                        // sealed material — metadata only, never the key.
                        let rawCredID = e["credId"] as? String ?? ""
                        let rawHandle = e["userHandle"] as? String ?? ""
                        let user = e["login"] as? String ?? ""
                        guard let rpID = e["rpId"] as? String, !rpID.isEmpty,
                              let credID = Self.data(b64url: rawCredID),
                              let handle = Self.data(b64url: rawHandle)
                        else { continue }
                        let key = "\(rpID)|\(uuid)|\(user)|\(rawCredID)|\(rawHandle)"
                        guard seen.insert(key).inserted else { continue }
                        identities.append(ASPasskeyCredentialIdentity(
                            relyingPartyIdentifier: rpID,
                            userName: user,
                            credentialID: credID,
                            userHandle: handle,
                            recordIdentifier: uuid,
                        ))
                    }
                }
                if let prevKeys, seen == prevKeys {
                    return
                }
                ASCredentialIdentityStore.shared.replaceCredentialIdentities(identities) { ok, err in
                    if let err {
                        NSLog("veil: identity sync failed %@", err.localizedDescription)
                    } else {
                        Task { @MainActor in
                            IdentitySync.shared.lastSyncedKeys = seen
                        }
                        NSLog("veil: identity sync wrote %d (enabled=%d)", identities.count, ok ? 1 : 0)
                    }
                }
            }
        }
    }
}
