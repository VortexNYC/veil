//
//  VaultStore.swift
//  Veil AutoFill (iOS)
//
//  The appex's only window onto the vault. The containing app writes a
//  handoff file into the shared app-group container (token + item
//  metadata — never secrets); this store reads it and calls the origin
//  over HTTPS when a credential is actually released. The origin is the
//  source of truth; the handoff items list is a display cache so the
//  picker still opens when the fetch is slow.
//

import Foundation

struct HandoffItem: Decodable {
    let uuid: String
    let name: String
    let login: String
    let uris: [String]
    let kind: String
    let credId: String?
    let rpId: String?
    let userHandle: String?
}

struct Handoff: Decodable {
    let token: String
    let origin: String
    let items: [HandoffItem]
}

enum VaultStoreError: Error {
    case noHandoff
    case origin(Int)
    case badResponse
}

final class VaultStore {

    static let appGroup = "group.nyc.veil.phone"
    private static let handoffName = "veil-autofill.json"

    private static var handoffURL: URL? {
        FileManager.default
            .containerURL(forSecurityApplicationGroupIdentifier: appGroup)?
            .appendingPathComponent(handoffName)
    }

    /// The handoff as last written by the containing app. Nil means the
    /// app has never synced (or the user signed out) — fail closed.
    static func loadHandoff() -> Handoff? {
        guard let url = handoffURL,
              let data = try? Data(contentsOf: url),
              let h = try? JSONDecoder().decode(Handoff.self, from: data)
        else { return nil }
        return h
    }

    /// Live items list from origin; falls back to the handoff cache.
    static func items(_ done: @escaping ([HandoffItem]) -> Void) {
        guard let h = loadHandoff() else { done([]); return }
        var req = URLRequest(url: URL(string: h.origin + "/v1/items")!)
        req.setValue("Bearer \(h.token)", forHTTPHeaderField: "Authorization")
        req.timeoutInterval = 10
        URLSession.shared.dataTask(with: req) { data, resp, _ in
            guard let data,
                  (resp as? HTTPURLResponse)?.statusCode == 200,
                  let body = try? JSONDecoder().decode(ItemsBody.self, from: data)
            else {
                done(h.items)
                return
            }
            done(body.items.map {
                HandoffItem(uuid: $0.id, name: $0.name, login: $0.login ?? "",
                            uris: $0.uris ?? [], kind: $0.kind ?? "login",
                            credId: $0.cred_id, rpId: $0.rp_id, userHandle: $0.user_handle)
            })
        }.resume()
    }

    /// Release a credential. The caller must have passed biometrics first —
    /// this is the one path where a secret crosses the wire, and the audit
    /// row lands server-side via the fill endpoint.
    static func fill(uuid: String, url: String, _ done: @escaping (Result<(user: String, password: String), Error>) -> Void) {
        guard let h = loadHandoff() else { done(.failure(VaultStoreError.noHandoff)); return }
        var req = URLRequest(url: URL(string: h.origin + "/v1/fill/logins")!)
        req.httpMethod = "POST"
        req.setValue("Bearer \(h.token)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try? JSONSerialization.data(withJSONObject: ["uuid": uuid, "url": url])
        req.timeoutInterval = 15
        URLSession.shared.dataTask(with: req) { data, resp, err in
            if let err { done(.failure(err)); return }
            let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
            guard code == 200, let data,
                  let body = try? JSONDecoder().decode(FillBody.self, from: data),
                  let first = body.entries.first, !first.password.isEmpty
            else { done(.failure(VaultStoreError.origin(code))); return }
            done(.success((first.login, first.password)))
        }.resume()
    }

    /// WebAuthn ceremony through the origin. The caller passes the RP's
    /// publicKey request object verbatim; the reply is the origin's
    /// `response` payload — a PublicKeyCredential-shaped dict with the
    /// attestation (register) or assertion (get) inside. Face ID gates the
    /// call the same way a password fill does.
    static func passkeys(register: Bool, origin: String, publicKey: [String: Any],
                         _ done: @escaping (Result<[String: Any], Error>) -> Void) {
        guard let h = loadHandoff() else { done(.failure(VaultStoreError.noHandoff)); return }
        let path = register ? "/v1/fill/passkeys/register" : "/v1/fill/passkeys/get"
        var req = URLRequest(url: URL(string: h.origin + path)!)
        req.httpMethod = "POST"
        req.setValue("Bearer \(h.token)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try? JSONSerialization.data(withJSONObject: [
            "origin": origin,
            "publicKey": publicKey,
        ])
        req.timeoutInterval = 15
        URLSession.shared.dataTask(with: req) { data, resp, err in
            if let err { done(.failure(err)); return }
            let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
            guard code == 200, let data,
                  let body = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let response = body["response"] as? [String: Any]
            else { done(.failure(VaultStoreError.origin(code))); return }
            done(.success(response))
        }.resume()
    }

    private struct ItemsBody: Decodable {
        struct Item: Decodable {
            let id: String
            let name: String
            let kind: String?
            let login: String?
            let uris: [String]?
            let cred_id: String?
            let rp_id: String?
            let user_handle: String?
        }
        let items: [Item]
    }

    private struct FillBody: Decodable {
        struct Entry: Decodable {
            let login: String
            let password: String
        }
        let entries: [Entry]
    }
}
