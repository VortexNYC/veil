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
                NSLog("veil: autofill provider not enabled — skipping identity sync")
                return
            }
            await Task.detached {
                guard let entries = try? await FillBridge.shared.list() else { return }
                var seen = Set<String>()
                var identities: [ASPasswordCredentialIdentity] = []
                for e in entries where (e["kind"] as? String) == "login" {
                    guard let uuid = e["uuid"] as? String, !uuid.isEmpty else { continue }
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
                }
                ASCredentialIdentityStore.shared.replaceCredentialIdentities(identities) { ok, err in
                    if let err {
                        NSLog("veil: identity sync failed %@", err.localizedDescription)
                    } else {
                        NSLog("veil: identity sync wrote %d (enabled=%d)", identities.count, ok ? 1 : 0)
                    }
                }
            }
        }
    }
}
