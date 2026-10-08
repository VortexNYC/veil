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
    let refresh: String?
    let issuer: String?
    let origin: String
    let items: [HandoffItem]
}

enum VaultStoreError: Error {
    case noHandoff
    case sessionExpired
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

    /// Write the handoff back after a refresh — the appex and the app
    /// share this file, so a token rotated here is what the app reads
    /// next time it asks.
    private static func storeHandoff(_ h: Handoff) {
        guard let url = handoffURL else { return }
        var raw: [String: Any] = [
            "token": h.token, "origin": h.origin,
        ]
        if let refresh = h.refresh { raw["refresh"] = refresh }
        if let issuer = h.issuer { raw["issuer"] = issuer }
        // Preserve the item cache verbatim — the appex holds no model
        // objects for it, so re-encode the decoded items losslessly by
        // keeping the file's own array untouched.
        if let data = try? Data(contentsOf: url),
           let existing = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           let items = existing["items"] {
            raw["items"] = items
        }
        guard let data = try? JSONSerialization.data(withJSONObject: raw) else { return }
        try? data.write(to: url,
                        options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }

    /// exp claim on a JWT payload — no signature check needed here; the
    /// token is ours, we only need to know whether origin will 401 it.
    private static func expired(_ jwt: String) -> Bool {
        let parts = jwt.split(separator: ".")
        guard parts.count >= 2,
              let data = Data(base64Encoded: padded(parts[1])),
              let claims = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let exp = claims["exp"] as? Double
        else { return true }
        return exp <= Date().timeIntervalSince1970 + 15
    }

    private static func padded(_ s: Substring) -> String {
        var b64 = s.replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")
        let pad = (4 - b64.count % 4) % 4
        if pad > 0 { b64 += String(repeating: "=", count: pad) }
        return b64
    }

    /// The handoff with a live bearer: nil → never synced; sessionExpired →
    /// token dead and the refresh grant failed (user must sign in again).
    /// A live handoff may carry a token the appex just reminted — written
    /// back to the file so the app converges on the newest pair.
    static func freshened(_ done: @escaping (Result<Handoff, VaultStoreError>) -> Void) {
        guard let h = loadHandoff() else { done(.failure(.noHandoff)); return }
        guard expired(h.token), let rt = h.refresh, !rt.isEmpty,
              let issuer = h.issuer, !issuer.isEmpty
        else { done(expired(h.token) ? .failure(.sessionExpired) : .success(h)); return }
        var req = URLRequest(url: URL(string: issuer + "/oauth2/token")!)
        req.httpMethod = "POST"
        req.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        req.httpBody = "grant_type=refresh_token&client_id=veil&scope=openid+offline_access&refresh_token=\(rt)"
            .data(using: .utf8)
        req.timeoutInterval = 10
        URLSession.shared.dataTask(with: req) { data, resp, _ in
            guard let data,
                  (resp as? HTTPURLResponse)?.statusCode == 200,
                  let body = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let id = body["id_token"] as? String, !id.isEmpty,
                  let refresh = body["refresh_token"] as? String, !refresh.isEmpty
            else { done(.failure(.sessionExpired)); return }
            let fresh = Handoff(token: id, refresh: refresh, issuer: h.issuer,
                                origin: h.origin, items: h.items)
            storeHandoff(fresh)
            NSLog("veil-autofill: session reminted")
            done(.success(fresh))
        }.resume()
    }

    /// Live items list from origin; falls back to the handoff cache. A
    /// stale cache still renders when the token is dead so the picker can
    /// say *why* a fill would fail instead of opening empty.
    static func items(_ done: @escaping ([HandoffItem], VaultStoreError?) -> Void) {
        freshened { r in
            let h: Handoff
            var authErr: VaultStoreError?
            switch r {
            case .success(let fresh): h = fresh
            case .failure(let e):
                guard let cached = loadHandoff() else { done([], e); return }
                h = cached
                authErr = e
            }
            var req = URLRequest(url: URL(string: h.origin + "/v1/items")!)
            req.setValue("Bearer \(h.token)", forHTTPHeaderField: "Authorization")
            req.timeoutInterval = 10
            URLSession.shared.dataTask(with: req) { data, resp, _ in
                guard let data,
                      (resp as? HTTPURLResponse)?.statusCode == 200,
                      let body = try? JSONDecoder().decode(ItemsBody.self, from: data)
                else {
                    done(h.items, authErr)
                    return
                }
                done(body.items.map {
                    HandoffItem(uuid: $0.id, name: $0.name, login: $0.login ?? "",
                                uris: $0.uris ?? [], kind: $0.kind ?? "login",
                                credId: $0.cred_id, rpId: $0.rp_id, userHandle: $0.user_handle)
                }, nil)
            }.resume()
        }
    }

    /// Release a credential. The caller must have passed biometrics first —
    /// this is the one path where a secret crosses the wire, and the audit
    /// row lands server-side via the fill endpoint.
    static func fill(uuid: String, url: String, _ done: @escaping (Result<(user: String, password: String), Error>) -> Void) {
        freshened { r in
            guard case .success(let h) = r else {
                if case .failure(let e) = r { done(.failure(e)) }
                return
            }
            fillWith(h, uuid: uuid, url: url, done)
        }
    }

    private static func fillWith(_ h: Handoff, uuid: String, url: String,
                                 _ done: @escaping (Result<(user: String, password: String), Error>) -> Void) {
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


    /// Store a captured sign-in. `existing` is an item name to overwrite
    /// (PATCH rotates the secret on the same item — never a duplicate);
    /// nil creates a new login. Returns the item uuid so the caller can
    /// publish the credential identity for inline fill.
    static func save(existing: String?, name: String, user: String, password: String,
                     url: String, _ done: @escaping (Result<String, Error>) -> Void) {
        freshened { r in
            guard case .success(let h) = r else {
                if case .failure(let e) = r { done(.failure(e)) }
                return
            }
            saveWith(h, existing: existing, name: name, user: user,
                     password: password, url: url, done)
        }
    }

    private static func saveWith(_ h: Handoff, existing: String?, name: String,
                                 user: String, password: String, url: String,
                                 _ done: @escaping (Result<String, Error>) -> Void) {
        var req: URLRequest
        var body: [String: Any] = ["login": user, "secret": password, "uri": url]
        if let existing, !existing.isEmpty {
            // Overwrite intent must never degrade into a second item —
            // a bad encoding fails the save rather than POSTing a twin.
            guard let escaped = existing.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed),
                  let u = URL(string: h.origin + "/v1/items/" + escaped)
            else { done(.failure(VaultStoreError.badResponse)); return }
            req = URLRequest(url: u)
            req.httpMethod = "PATCH"
        } else if let u = URL(string: h.origin + "/v1/items") {
            req = URLRequest(url: u)
            req.httpMethod = "POST"
            body["name"] = name
        } else {
            done(.failure(VaultStoreError.badResponse))
            return
        }
        req.setValue("Bearer \(h.token)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)
        req.timeoutInterval = 15
        URLSession.shared.dataTask(with: req) { data, resp, err in
            if let err { done(.failure(err)); return }
            let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
            guard code == 200, let data,
                  let body = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let id = body["id"] as? String
            else { done(.failure(VaultStoreError.origin(code))); return }
            done(.success(id))
        }.resume()
    }

    /// WebAuthn ceremony through the origin. The caller passes the RP's
    /// publicKey request object verbatim; the reply is the origin's
    /// `response` payload — a PublicKeyCredential-shaped dict with the
    /// attestation (register) or assertion (get) inside. Face ID gates the
    /// call the same way a password fill does.
    static func passkeys(register: Bool, origin: String, publicKey: [String: Any],
                         _ done: @escaping (Result<[String: Any], Error>) -> Void) {
        freshened { r in
            guard case .success(let h) = r else {
                if case .failure(let e) = r { done(.failure(e)) }
                return
            }
            passkeysWith(h, register: register, origin: origin,
                         publicKey: publicKey, done)
        }
    }

    private static func passkeysWith(_ h: Handoff, register: Bool, origin: String,
                                     publicKey: [String: Any],
                                     _ done: @escaping (Result<[String: Any], Error>) -> Void) {
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
