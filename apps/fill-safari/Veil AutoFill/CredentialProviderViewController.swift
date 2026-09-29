//
//  CredentialProviderViewController.swift
//  Veil AutoFill
//
//  The system credential-provider surface: when a field's AutoFill menu
//  offers Veil (or the user taps one of our synced inline suggestions),
//  this view controller lists matching logins and completes the request.
//  The secret never leaves the Go host without Touch ID — `fill` over the
//  bridge confirms before returning material, and the OS inserts the
//  returned credential into the field itself.
//

import Cocoa
import AuthenticationServices

final class CredentialProviderViewController: ASCredentialProviderViewController {

    private var entries: [[String: Any]] = []
    private var serviceURL = ""
    /// Set when the system is asking for a passkey — the OS owns
    /// clientDataJSON and hands us its hash to sign.
    private var passkeyParams: ASPasskeyCredentialRequestParameters?
    private let table = NSTableView()
    private let status = NSTextField(labelWithString: "Veil")
    private var scroll: NSScrollView!

    /// Same deal as the app's IdentitySync vlog — appex NSLog is invisible;
    /// trace invocations in the container instead.
    private func vlog(_ msg: String) {
        let path = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("veil-appex.log")
        let line = "\(ISO8601DateFormatter().string(from: Date())) \(msg)\n"
        if let h = try? FileHandle(forWritingTo: path) {
            h.seekToEndOfFile(); h.write(Data(line.utf8)); try? h.close()
        } else {
            try? line.write(to: path, atomically: true, encoding: .utf8)
        }
    }

    override func loadView() {
        let view = NSView(frame: NSRect(x: 0, y: 0, width: 380, height: 280))
        preferredContentSize = view.frame.size

        status.font = .systemFont(ofSize: 13, weight: .semibold)
        status.frame = NSRect(x: 14, y: 244, width: 280, height: 20)
        view.addSubview(status)

        let cancel = NSButton(title: "Cancel", target: self, action: #selector(cancelRequest))
        cancel.bezelStyle = .rounded
        cancel.frame = NSRect(x: 288, y: 240, width: 80, height: 24)
        view.addSubview(cancel)

        let col = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("c"))
        table.addTableColumn(col)
        table.headerView = nil
        table.rowHeight = 26
        table.dataSource = self
        table.delegate = self
        table.target = self
        table.action = #selector(rowClicked(_:))
        table.doubleAction = #selector(rowClicked(_:))

        scroll = NSScrollView(frame: NSRect(x: 0, y: 0, width: 380, height: 232))
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        view.addSubview(scroll)
        self.view = view
    }

    /// Field-menu picks arrive through the legacy password-only entry point —
    /// the two-arg overload only runs when a passkey request is in flight.
    /// Both funnel into the same list logic; nil parameters means passwords.
    override func prepareCredentialList(for serviceIdentifiers: [ASCredentialServiceIdentifier]) {
        prepareCredentialList(for: serviceIdentifiers, requestParameters: nil)
    }

    /// User picked Veil from the field's AutoFill menu — list every login we
    /// can offer for this service (or all of them when the system sends no
    /// identifier). Passkey requests are out of scope for this slice.
    override func prepareCredentialList(
        for serviceIdentifiers: [ASCredentialServiceIdentifier],
        requestParameters: ASPasskeyCredentialRequestParameters?,
    ) {
        vlog("prepareCredentialList ids=\(serviceIdentifiers.map { $0.identifier }) passkey=\(requestParameters != nil)")
        passkeyParams = requestParameters
        if let params = requestParameters {
            // Passkey assertion: entries need credId/rpId/userHandle, which
            // only `list` (the trusted surface) carries — `match` strips them.
            serviceURL = "https://" + params.relyingPartyIdentifier
            DispatchQueue.global().async {
                let got = (try? FillBridge.shared.list()) ?? []
                self.vlog("passkey list got=\(got.count) rp=\(params.relyingPartyIdentifier)")
                DispatchQueue.main.async {
                    self.entries = got.filter { e in
                        guard (e["kind"] as? String) == "passkey",
                              (e["rpId"] as? String) == params.relyingPartyIdentifier,
                              let credID = e["credId"] as? String, !credID.isEmpty
                        else { return false }
                        // Empty allow-list means any credential is acceptable.
                        if params.allowedCredentials.isEmpty { return true }
                        guard let data = Self.data(b64url: credID) else { return false }
                        return params.allowedCredentials.contains(data)
                    }
                    self.status.stringValue = self.entries.isEmpty
                        ? "Veil — no passkeys for \(params.relyingPartyIdentifier)"
                        : "Veil — choose a passkey"
                    self.table.reloadData()
                }
            }
            return
        }
        serviceURL = serviceIdentifiers.first.map(Self.url(for:)) ?? ""
        DispatchQueue.global().async {
            let got: [[String: Any]]
            do {
                if self.serviceURL.isEmpty {
                    got = try FillBridge.shared.list()
                } else {
                    got = try FillBridge.shared.match(url: self.serviceURL)
                }
                self.vlog("bridge ok got=\(got.count)")
            } catch {
                self.vlog("bridge failed \(error.localizedDescription)")
                got = []
            }
            DispatchQueue.main.async {
                self.entries = got.filter { ($0["kind"] as? String) == "login" }
                if self.entries.isEmpty {
                    self.status.stringValue = "Veil — no logins for this site"
                } else {
                    self.status.stringValue = "Veil — choose a sign-in"
                }
                self.table.reloadData()
            }
        }
    }

    /// The user tapped one of our inline suggestions — the identity store
    /// already knows which record; confirm at the host and complete.
    override func prepareInterfaceToProvideCredential(for credentialRequest: any ASCredentialRequest) {
        vlog("prepareInterfaceToProvideCredential")
        if let req = credentialRequest as? ASPasskeyCredentialRequest,
           let identity = req.credentialIdentity as? ASPasskeyCredentialIdentity {
            assertPasskey(
                rpID: identity.relyingPartyIdentifier,
                credID: identity.credentialID,
                userHandle: identity.userHandle,
                clientDataHash: req.clientDataHash,
            )
            return
        }
        guard let req = credentialRequest as? ASPasswordCredentialRequest,
              let identity = req.credentialIdentity as? ASPasswordCredentialIdentity,
              let uuid = identity.recordIdentifier, !uuid.isEmpty
        else {
            cancel(with: .credentialIdentityNotFound)
            return
        }
        serviceURL = Self.url(for: identity.serviceIdentifier)
        status.stringValue = "Veil — confirming \(identity.user)"
        fill(uuid: uuid)
    }

    /// Site wants a new passkey: the host mints the ES256 credential, the
    /// OS owns clientData. We return the attestation and then save the
    /// identity so future assertions find it.
    override func prepareInterface(forPasskeyRegistration registrationRequest: any ASCredentialRequest) {
        vlog("prepareInterfaceForPasskeyRegistration")
        guard let req = registrationRequest as? ASPasskeyCredentialRequest,
              let identity = req.credentialIdentity as? ASPasskeyCredentialIdentity
        else {
            cancel(with: .failed)
            return
        }
        // We mint ES256 only — if the RP's list excludes it we cannot serve.
        let algs = req.supportedAlgorithms.map(\.rawValue)
        if !algs.isEmpty && !algs.contains(ASCOSEAlgorithmIdentifier.ES256.rawValue) {
            cancel(with: .failed)
            return
        }
        let rpID = identity.relyingPartyIdentifier
        let origin = "https://" + rpID
        var excluded: [[String: Any]] = []
        if #available(macOS 15.0, *) {
            for c in req.excludedCredentials ?? [] {
                excluded.append(["type": "public-key", "id": Self.b64url(c.credentialID)])
            }
        }
        let publicKey: [String: Any] = [
            "clientDataHash": Self.b64url(req.clientDataHash),
            "rp": ["id": rpID, "name": rpID],
            "user": [
                "id": Self.b64url(identity.userHandle),
                "name": identity.userName,
                "displayName": identity.userName,
            ],
            "pubKeyCredParams": [["type": "public-key", "alg": -7]],
            "excludeCredentials": excluded,
        ]
        status.stringValue = "Veil — confirming passkey for \(identity.userName)"
        DispatchQueue.global().async {
            guard let resp = try? FillBridge.shared.passkeyRegister(origin: origin, publicKey: publicKey),
                  let attB64 = (resp["response"] as? [String: Any])?["attestationObject"] as? String,
                  let att = Self.data(b64url: attB64),
                  let rawID = resp["rawId"] as? String ?? resp["id"] as? String,
                  let credID = Self.data(b64url: rawID)
            else {
                self.vlog("passkeyRegister failed")
                DispatchQueue.main.async { self.cancel(with: .userCanceled) }
                return
            }
            let registration = ASPasskeyRegistrationCredential(
                relyingParty: rpID,
                clientDataHash: req.clientDataHash,
                credentialID: credID,
                attestationObject: att,
            )
            let saved = ASPasskeyCredentialIdentity(
                relyingPartyIdentifier: rpID,
                userName: identity.userName,
                credentialID: credID,
                userHandle: identity.userHandle,
                recordIdentifier: rawID,
            )
            DispatchQueue.main.async {
                ASCredentialIdentityStore.shared.saveCredentialIdentities([saved]) { _, _ in }
                self.vlog("passkey registered rp=\(rpID)")
                self.extensionContext.completeRegistrationRequest(using: registration)
            }
        }
    }

    /// Both passkey assertion entry points land here: the user picked a
    /// synced identity inline, or picked a row off our list. Sign the OS's
    /// clientDataHash over the bridge — Touch ID gates it there.
    private func assertPasskey(rpID: String, credID: Data, userHandle: Data, clientDataHash: Data) {
        let origin = "https://" + rpID
        let publicKey: [String: Any] = [
            "clientDataHash": Self.b64url(clientDataHash),
            "rpId": rpID,
            "allowCredentials": [["type": "public-key", "id": Self.b64url(credID)]],
        ]
        status.stringValue = "Veil — confirming passkey"
        vlog("passkeyGet rp=\(rpID)")
        DispatchQueue.global().async {
            guard let resp = try? FillBridge.shared.passkeyGet(origin: origin, publicKey: publicKey),
                  let inner = resp["response"] as? [String: Any],
                  let authB64 = inner["authenticatorData"] as? String,
                  let sigB64 = inner["signature"] as? String,
                  let auth = Self.data(b64url: authB64),
                  let sig = Self.data(b64url: sigB64)
            else {
                self.vlog("passkeyGet failed")
                DispatchQueue.main.async { self.cancel(with: .userCanceled) }
                return
            }
            let assertion = ASPasskeyAssertionCredential(
                userHandle: userHandle,
                relyingParty: rpID,
                signature: sig,
                clientDataHash: clientDataHash,
                authenticatorData: auth,
                credentialID: credID,
            )
            DispatchQueue.main.async {
                self.vlog("passkey assertion ok rp=\(rpID)")
                self.extensionContext.completeAssertionRequest(using: assertion)
            }
        }
    }

    /// Passwords settings → Veil gear. Nothing to configure here — the vault
    /// and grants live in the host; this screen just confirms identity.
    override func prepareInterfaceForExtensionConfiguration() {
        status.stringValue = "Veil — credentials are managed by the Veil app"
        entries = []
        table.reloadData()
    }

    /// We never answer silently — every secret release is a Touch ID. Tell
    /// the system our UI is needed; it calls back into
    /// prepareInterfaceToProvideCredential(for:).
    override func provideCredentialWithoutUserInteraction(for credentialRequest: any ASCredentialRequest) {
        vlog("provideCredentialWithoutUserInteraction -> userInteractionRequired")
        cancel(with: .userInteractionRequired)
    }

    private func fill(uuid: String) {
        let url = serviceURL
        DispatchQueue.global().async {
            guard let (entries, _) = try? FillBridge.shared.fill(url: url, uuid: uuid),
                  let first = entries.first,
                  let password = first["password"] as? String, !password.isEmpty
            else {
                DispatchQueue.main.async { self.cancel(with: .userCanceled) }
                return
            }
            let login = first["login"] as? String ?? ""
            let credential = ASPasswordCredential(user: login, password: password)
            DispatchQueue.main.async {
                self.extensionContext.completeRequest(withSelectedCredential: credential)
            }
        }
    }

    @objc private func rowClicked(_ sender: Any?) {
        let row = table.selectedRow
        guard row >= 0, row < entries.count else { return }
        let e = entries[row]
        if let params = passkeyParams, (e["kind"] as? String) == "passkey",
           let credID = Self.data(b64url: e["credId"] as? String ?? ""),
           let handle = Self.data(b64url: e["userHandle"] as? String ?? "")
        {
            assertPasskey(
                rpID: params.relyingPartyIdentifier,
                credID: credID,
                userHandle: handle,
                clientDataHash: params.clientDataHash,
            )
            return
        }
        guard let uuid = e["uuid"] as? String, !uuid.isEmpty else { return }
        status.stringValue = "Veil — confirming…"
        fill(uuid: uuid)
    }

    @objc private func cancelRequest() {
        cancel(with: .userCanceled)
    }

    private func cancel(with code: ASExtensionError.Code) {
        extensionContext.cancelRequest(withError: ASExtensionError(code))
    }

    /// The host speaks unpadded base64url (the WebAuthn encoding) — Data's
    /// own codec is the padded standard alphabet.
    private static func b64url(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .trimmingCharacters(in: CharacterSet(charactersIn: "="))
    }

    private static func data(b64url: String) -> Data? {
        var s = b64url
            .replacingOccurrences(of: "-", with: "+")
            .replacingOccurrences(of: "_", with: "/")
        let pad = (4 - s.count % 4) % 4
        if pad > 0 { s += String(repeating: "=", count: pad) }
        return Data(base64Encoded: s)
    }

    private static func url(for id: ASCredentialServiceIdentifier) -> String {
        switch id.type {
        case .domain:
            return "https://" + id.identifier
        case .URL:
            return id.identifier
        @unknown default:
            return id.identifier
        }
    }
}

extension CredentialProviderViewController: NSTableViewDataSource, NSTableViewDelegate {
    func numberOfRows(in tableView: NSTableView) -> Int { entries.count }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let e = entries[row]
        let name = e["name"] as? String ?? "item"
        let login = e["login"] as? String ?? ""
        let id = NSUserInterfaceItemIdentifier("cell")
        let cell = tableView.makeView(withIdentifier: id, owner: nil) as? NSTableCellView ?? {
            let v = NSTableCellView()
            let tf = NSTextField(labelWithString: "")
            tf.translatesAutoresizingMaskIntoConstraints = false
            v.addSubview(tf)
            v.textField = tf
            NSLayoutConstraint.activate([
                tf.leadingAnchor.constraint(equalTo: v.leadingAnchor, constant: 12),
                tf.centerYAnchor.constraint(equalTo: v.centerYAnchor),
                tf.trailingAnchor.constraint(lessThanOrEqualTo: v.trailingAnchor, constant: -8),
            ])
            v.identifier = id
            return v
        }()
        cell.textField?.stringValue = login.isEmpty ? name : "\(name) — \(login)"
        cell.textField?.lineBreakMode = .byTruncatingTail
        return cell
    }
}
