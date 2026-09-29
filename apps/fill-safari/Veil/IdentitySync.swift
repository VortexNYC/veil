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

    /// NSLog is not reliably observable from a launched accessory app —
    /// drop a line in the container instead (readable unsandboxed).
    private func vlog(_ msg: String) {
        let path = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("veil-sync.log")
        let line = "\(ISO8601DateFormatter().string(from: Date())) \(msg)\n"
        if let h = try? FileHandle(forWritingTo: path) {
            h.seekToEndOfFile(); h.write(Data(line.utf8)); try? h.close()
        } else {
            try? line.write(to: path, atomically: true, encoding: .utf8)
        }
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
        vlog("sync begin")
        Task {
            let state = await ASCredentialIdentityStore.shared.state()
            vlog("state enabled=\(state.isEnabled)")
            guard state.isEnabled else {
                NSLog("veil: autofill provider not enabled — skipping identity sync")
                return
            }
            await Task.detached {
                let entries: [[String: Any]]
                do {
                    entries = try await FillBridge.shared.list()
                    self.vlog("list ok entries=\(entries.count)")
                } catch {
                    self.vlog("list failed \(error.localizedDescription)")
                    return
                }
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
                self.vlog("pushing identities=\(identities.count)")
                ASCredentialIdentityStore.shared.replaceCredentialIdentities(identities) { ok, err in
                    if let err {
                        self.vlog("sync failed \(err.localizedDescription)")
                        NSLog("veil: identity sync failed %@", err.localizedDescription)
                    } else {
                        self.vlog("sync wrote \(identities.count) enabled=\(ok ? 1 : 0)")
                        NSLog("veil: identity sync wrote %d (enabled=%d)", identities.count, ok ? 1 : 0)
                    }
                }
            }
        }
    }
}
