//
//  CredentialProviderViewController.swift
//  Veil AutoFill (iOS)
//
//  The system credential-provider surface: when a field's AutoFill sheet
//  offers Veil (or the user taps one of our synced inline suggestions),
//  this view controller lists matching logins and completes the request.
//  Mirrors the macOS appex contract: Face ID before a secret leaves, no
//  silent releases, deny/down/unknown cancels — fail closed.
//  Passkeys: assertion + registration port the macOS appex (slice 32d)
//  contract — origin signs the OS's clientDataHash; Face ID gates first.
//

import UIKit
import AuthenticationServices
import LocalAuthentication

final class CredentialProviderViewController: ASCredentialProviderViewController {

    /// What the next Face ID gate releases. Everything that crosses the
    /// wire — a password fill, a passkey signature, a new passkey's
    /// attestation — is one of these, deferred until the scene is
    /// foregrounded (LAContext returns .notInteractive before then).
    private enum PendingOp {
        case password(uuid: String)
        case passkeyAssert(rpID: String, credID: Data, userHandle: Data, clientDataHash: Data)
        case passkeyRegister(request: ASPasskeyCredentialRequest, userName: String)
        /// Context-menu AutoFill path (iOS 18+): the OS wants one string
        /// for the focused field — a username or a password, still behind
        /// the Face ID gate.
        case textInsert(uuid: String, password: Bool)
    }

    private var entries: [HandoffItem] = []
    private var serviceURL = ""
    /// Set by prepareInterfaceForUserChoosingTextToInsert — the table then
    /// shows username/password rows per item instead of a single pick.
    private var textInsert = false
    private var pending: PendingOp?
    /// Set when the system is asking for a passkey — the OS owns
    /// clientDataJSON and hands us its hash to sign.
    private var passkeyParams: ASPasskeyCredentialRequestParameters?
    /// A save sheet waiting on the user's tap. Extracted fields, not the
    /// request object — ASSavePasswordRequest is an iOS 26.2 type and
    /// can't be a stored property on this deployment target.
    private struct PendingSave {
        let serviceIdentifier: ASCredentialServiceIdentifier
        let url: String
        let user: String
        let password: String
        let title: String?
    }
    private var pendingSave: PendingSave?

    private let table = UITableView(frame: .zero, style: .plain)
    private let status = UILabel()
    private let emptyLabel = UILabel()
    private let saveButton = UIButton(type: .system)
    /// `let`, not an outlet set in loadView — the OS can call
    /// prepareInterface before the view hierarchy exists, and save mode
    /// relabels this to "Not Now".
    private let cancel = UIButton(type: .system)

    private func vlog(_ msg: String) {
        NSLog("veil-autofill: %@", msg)
    }

    override func loadView() {
        let root = UIView()
        root.backgroundColor = .systemBackground

        let brand = UILabel()
        brand.text = "Veil"
        brand.font = .systemFont(ofSize: 20, weight: .semibold)
        brand.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(brand)

        status.font = .systemFont(ofSize: 13)
        status.textColor = .secondaryLabel
        status.numberOfLines = 0
        status.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(status)

        table.translatesAutoresizingMaskIntoConstraints = false
        table.register(HandoffCell.self, forCellReuseIdentifier: "c")
        table.dataSource = self
        table.delegate = self
        root.addSubview(table)

        emptyLabel.font = .systemFont(ofSize: 14)
        emptyLabel.textColor = .secondaryLabel
        emptyLabel.textAlignment = .center
        emptyLabel.isHidden = true
        emptyLabel.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(emptyLabel)

        let footer = UILabel()
        footer.text = "\(Self.biometricName) required to fill"
        footer.font = .systemFont(ofSize: 12)
        footer.textColor = .secondaryLabel
        footer.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(footer)

        cancel.setTitle("Cancel", for: .normal)
        cancel.addTarget(self, action: #selector(cancelTapped), for: .touchUpInside)
        cancel.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(cancel)

        saveButton.setTitle("Save", for: .normal)
        saveButton.titleLabel?.font = .systemFont(ofSize: 13, weight: .semibold)
        saveButton.addTarget(self, action: #selector(saveTapped), for: .touchUpInside)
        saveButton.isHidden = true
        saveButton.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(saveButton)

        NSLayoutConstraint.activate([
            brand.topAnchor.constraint(equalTo: root.safeAreaLayoutGuide.topAnchor, constant: 16),
            brand.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 20),
            status.topAnchor.constraint(equalTo: brand.bottomAnchor, constant: 2),
            status.leadingAnchor.constraint(equalTo: brand.leadingAnchor),
            status.trailingAnchor.constraint(lessThanOrEqualTo: root.trailingAnchor, constant: -20),
            table.topAnchor.constraint(equalTo: status.bottomAnchor, constant: 12),
            table.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            table.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            table.bottomAnchor.constraint(equalTo: footer.topAnchor, constant: -8),
            emptyLabel.centerXAnchor.constraint(equalTo: table.centerXAnchor),
            emptyLabel.centerYAnchor.constraint(equalTo: table.centerYAnchor),
            footer.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 20),
            footer.bottomAnchor.constraint(equalTo: root.safeAreaLayoutGuide.bottomAnchor, constant: -12),
            cancel.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -20),
            cancel.centerYAnchor.constraint(equalTo: footer.centerYAnchor),
            saveButton.trailingAnchor.constraint(equalTo: cancel.leadingAnchor, constant: -16),
            saveButton.centerYAnchor.constraint(equalTo: footer.centerYAnchor),
        ])

        self.view = root
    }

    // MARK: - ASCredentialProviderViewController

    /// User picked Veil from the AutoFill sheet — list logins matching the
    /// service identifier (or everything when the system sends none).
    override func prepareCredentialList(for serviceIdentifiers: [ASCredentialServiceIdentifier]) {
        prepareCredentialList(for: serviceIdentifiers, requestParameters: nil)
    }

    /// The two-arg overload runs when a passkey request is in flight; nil
    /// parameters means passwords. Both funnel into the same list logic.
    override func prepareCredentialList(
        for serviceIdentifiers: [ASCredentialServiceIdentifier],
        requestParameters: ASPasskeyCredentialRequestParameters?,
    ) {
        vlog("prepareCredentialList ids=\(serviceIdentifiers.map { $0.identifier }) passkey=\(requestParameters != nil)")
        passkeyParams = requestParameters
        if let params = requestParameters {
            // Passkey assertion: candidates need credId/rpId/userHandle,
            // which the items list carries for human principals.
            serviceURL = "https://" + params.relyingPartyIdentifier
            status.text = "Choose a passkey for \(params.relyingPartyIdentifier)"
            VaultStore.items { [weak self] all in
                DispatchQueue.main.async {
                    guard let self else { return }
                    self.entries = all.filter { item in
                        guard item.kind == "passkey",
                              item.rpId == params.relyingPartyIdentifier,
                              let credB64 = item.credId,
                              let credID = Self.data(b64url: credB64), !credID.isEmpty
                        else { return false }
                        // Empty allow-list means any credential is acceptable.
                        if params.allowedCredentials.isEmpty { return true }
                        return params.allowedCredentials.contains(credID)
                    }
                    if self.entries.isEmpty {
                        self.status.text = "No passkeys for \(params.relyingPartyIdentifier)"
                        self.emptyLabel.text = self.status.text
                    }
                    self.emptyLabel.isHidden = !self.entries.isEmpty
                    self.table.reloadData()
                }
            }
            return
        }
        serviceURL = serviceIdentifiers.first.map(Self.url(for:)) ?? ""
        let host = Self.host(serviceURL)
        status.text = host.isEmpty ? "Choose a sign-in" : "Fill for \(host)"
        VaultStore.items { [weak self] all in
            DispatchQueue.main.async {
                guard let self else { return }
                self.entries = all.filter { item in
                    // ItemAPIKey is "api_key" — the password envelope.
                    guard ["api_key", "login"].contains(item.kind) else { return false }
                    guard !host.isEmpty else { return true }
                    return item.uris.contains { Self.host($0) == host }
                        || item.name.localizedCaseInsensitiveContains(host)
                }
                if self.entries.isEmpty {
                    self.status.text = host.isEmpty ? "No saved logins" : "No logins for \(host)"
                    self.emptyLabel.text = self.status.text
                }
                self.emptyLabel.isHidden = !self.entries.isEmpty
                self.table.reloadData()
            }
        }
    }

    /// The user tapped one of our inline suggestions — the identity store
    /// already knows which record; Face ID, then fill or sign.
    override func prepareInterfaceToProvideCredential(for credentialRequest: any ASCredentialRequest) {
        vlog("prepareInterfaceToProvideCredential")
        if let req = credentialRequest as? ASPasskeyCredentialRequest,
           let identity = req.credentialIdentity as? ASPasskeyCredentialIdentity {
            status.text = "Confirming \(identity.userName)…"
            deferPending(.passkeyAssert(
                rpID: identity.relyingPartyIdentifier,
                credID: identity.credentialID,
                userHandle: identity.userHandle,
                clientDataHash: req.clientDataHash,
            ))
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
        status.text = "Confirming \(identity.user)…"
        deferPending(.password(uuid: uuid))
    }

    /// Site wants a new passkey: origin mints the ES256 credential, the OS
    /// owns clientData — we return the attestation and save the identity.
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
        status.text = "Confirming passkey for \(identity.userName)…"
        deferPending(.passkeyRegister(request: req, userName: identity.userName))
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        flushPending()
    }

    /// LAContext.evaluatePolicy fails with .notInteractive while the hosted
    /// scene is still off-screen — every credential release defers to
    /// viewDidAppear (or immediately when the view is already up).
    private func deferPending(_ op: PendingOp) {
        pending = op
        if isViewLoaded && view.window != nil {
            flushPending()
        }
    }

    private func flushPending() {
        guard let op = pending else { return }
        pending = nil
        release(op)
    }

    /// Passwords settings → Veil gear. Credentials live in the vault; this
    /// surface only confirms identity.
    override func prepareInterfaceForExtensionConfiguration() {
        status.text = "Credentials are managed by the Veil app"
    }

    /// Never answer silently — every secret release is Face ID. The system
    /// calls back into prepareInterfaceToProvideCredential(for:).
    override func provideCredentialWithoutUserInteraction(for credentialRequest: any ASCredentialRequest) {
        vlog("provideCredentialWithoutUserInteraction -> userInteractionRequired")
        cancel(with: .userInteractionRequired)
    }

    /// Context-menu "AutoFill → Passwords" on iOS 18+ dispatches a
    /// text-insert request, not prepareCredentialList — without this
    /// override the OS shows "developer needs to update it". No service
    /// identifiers arrive here, so every login item is listed, two rows
    /// each (username, password); the pick completes with that one string.
    override func prepareInterfaceForUserChoosingTextToInsert() {
        vlog("prepareInterfaceForUserChoosingTextToInsert")
        textInsert = true
        status.text = "Choose what to fill"
        VaultStore.items { [weak self] all in
            DispatchQueue.main.async {
                guard let self else { return }
                self.entries = all.filter { ["api_key", "login"].contains($0.kind) }
                self.emptyLabel.isHidden = !self.entries.isEmpty
                self.table.reloadData()
            }
        }
    }

    // MARK: - Save / Generate (iOS 26.2)

    /// The OS captured a submitted sign-in. A password our own generate
    /// request produced lands as a pending account with no UI — Apple
    /// transmits nothing back to the form anyway. A brand-new login on
    /// form dismissal also saves quietly; an overwrite there, and every
    /// express save, asks first (fill.md: accept required).
    @available(iOS 26.2, *)
    override func performWithoutUserInteractionIfPossible(savePasswordRequest req: ASSavePasswordRequest) {
        vlog("saveIfPossible event=\(req.event.rawValue)")
        switch req.event {
        case .generatedPasswordFilled:
            saveCredential(req, requireNew: false)
        case .formDidDisappear:
            saveCredential(req, requireNew: true)
        default:
            cancel(with: .userInteractionRequired)
        }
    }

    /// The interactive save path — express saves, plus silent paths that
    /// answered userInteractionRequired. A tap on Save is the accept.
    @available(iOS 26.2, *)
    override func prepareInterface(for savePasswordRequest: ASSavePasswordRequest) {
        let req = savePasswordRequest
        vlog("prepareInterface save event=\(req.event.rawValue)")
        let url = Self.url(for: req.serviceIdentifier)
        status.text = "Save \(req.credential.user)\non \(Self.host(url))?"
        pendingSave = PendingSave(serviceIdentifier: req.serviceIdentifier, url: url,
                                  user: req.credential.user, password: req.credential.password,
                                  title: req.title)
        table.isHidden = true
        emptyLabel.isHidden = true
        saveButton.isHidden = false
        cancel.setTitle("Not Now", for: .normal)
    }

    /// Silent password mint — no UI, no network, just CSPRNG output shaped
    /// by the site's passwordrules. SupportsGeneratePasswordsCredentials-
    /// WithUI is false in the plist, so the OS renders the result itself
    /// and this is the only generate path that runs.
    @available(iOS 26.2, *)
    override func performWithoutUserInteraction(generatePasswordsRequest req: ASGeneratePasswordsRequest) {
        vlog("generateIfPossible")
        let rules = req.passwordFieldPasswordRules ?? req.passwordRulesFromQuirks
        let primary = PasswordGen.generate(rules: rules)
        var results = [ASGeneratedPassword(kind: primary.alphanumeric ? .alphanumeric : .strong,
                                           value: primary.value)]
        if !primary.alphanumeric {
            let alt = PasswordGen.generate(rules: "allowed: [a-zA-Z0-9]")
            results.append(ASGeneratedPassword(kind: .alphanumeric, value: alt.value))
        }
        extensionContext.completeGeneratePasswordRequest(results: results) { _ in }
    }

    /// Write a captured sign-in to origin. An item matching host+login is
    /// overwritten via PATCH — the uuid survives and no duplicate is made;
    /// `requireNew` upgrades that overwrite to the interactive sheet per
    /// Apple's form-dismissal contract. An empty login can never match,
    /// so it always creates.
    @available(iOS 26.2, *)
    private func saveCredential(_ req: ASSavePasswordRequest, requireNew: Bool) {
        let url = Self.url(for: req.serviceIdentifier)
        VaultStore.items { [weak self] all in
            guard let self else { return }
            let existing = Self.savedItem(all, url: url, user: req.credential.user)
            if requireNew, existing != nil {
                DispatchQueue.main.async { self.cancel(with: .userInteractionRequired) }
                return
            }
            self.persistSave(url: url, user: req.credential.user,
                             password: req.credential.password, title: req.title,
                             overwrite: existing?.name,
                             serviceIdentifier: req.serviceIdentifier)
        }
    }

    /// The item that already covers this host+login — an empty login can
    /// never match, so unknown users always create rather than overwrite.
    private static func savedItem(_ all: [HandoffItem], url: String, user: String) -> HandoffItem? {
        let urlHost = host(url)
        return all.first { item in
            !item.login.isEmpty && item.login == user
                && item.uris.contains { host($0) == urlHost }
        }
    }

    /// POST or PATCH the item, publish its credential identity so inline
    /// fill sees it immediately, then complete the OS request.
    @available(iOS 26.2, *)
    private func persistSave(url: String, user: String, password: String, title: String?,
                             overwrite: String?,
                             serviceIdentifier: ASCredentialServiceIdentifier) {
        let host = Self.host(url)
        let name = (title?.isEmpty == false ? title : nil) ?? host
        VaultStore.save(existing: overwrite, name: name.isEmpty ? user : name,
                        user: user, password: password, url: url) { [weak self] result in
            guard let self else { return }
            switch result {
            case .success(let uuid):
                let id = ASPasswordCredentialIdentity(serviceIdentifier: serviceIdentifier,
                                                      user: user, recordIdentifier: uuid)
                ASCredentialIdentityStore.shared.saveCredentialIdentities([id]) { _, _ in }
                self.vlog("save ok")
                DispatchQueue.main.async {
                    self.extensionContext.completeSavePasswordRequest { _ in }
                }
            case .failure(let err):
                self.vlog("save failed \(err.localizedDescription)")
                DispatchQueue.main.async { self.cancel(with: .failed) }
            }
        }
    }

    @objc private func saveTapped() {
        guard #available(iOS 26.2, *), let s = pendingSave else { return }
        pendingSave = nil
        status.text = "Saving…"
        VaultStore.items { [weak self] all in
            guard let self else { return }
            let existing = Self.savedItem(all, url: s.url, user: s.user)
            self.persistSave(url: s.url, user: s.user, password: s.password,
                             title: s.title, overwrite: existing?.name,
                             serviceIdentifier: s.serviceIdentifier)
        }
    }

    // MARK: - Fill

    /// What `deviceOwnerAuthentication` will actually ask for on this
    /// hardware — Face ID, Touch ID, Optic ID, or the passcode when no
    /// biometry is enrolled. `biometryType` reads the device capability,
    /// not which method the user prefers.
    private static var biometricName: String {
        switch LAContext().biometryType {
        case .faceID: return "Face ID"
        case .touchID: return "Touch ID"
        case .opticID: return "Optic ID"
        default: return "Passcode"
        }
    }

    private func release(_ op: PendingOp, retryNotForeground: Bool = true) {
        let bio = Self.biometricName
        let reason: String
        switch op {
        case .password:
            reason = "Veil needs \(bio) before this password fills"
        case .passkeyAssert:
            reason = "Veil needs \(bio) before this passkey signs you in"
        case .passkeyRegister:
            reason = "Veil needs \(bio) to create this passkey"
        case .textInsert(_, let password):
            reason = password
                ? "Veil needs \(bio) before this password fills"
                : "Veil needs \(bio) before this username fills"
        }
        let ctx = LAContext()
        ctx.evaluatePolicy(.deviceOwnerAuthentication,
                           localizedReason: reason) { [weak self] ok, err in
            guard let self else { return }
            if !ok, retryNotForeground,
               (err as? LAError)?.code == .notInteractive {
                vlog("LA notInteractive — retrying once")
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
                    self.release(op, retryNotForeground: false)
                }
                return
            }
            guard ok else {
                self.vlog("LA failed \((err as? LAError)?.code.rawValue ?? 0)")
                DispatchQueue.main.async { self.cancel(with: .userCanceled) }
                return
            }
            switch op {
            case .password(let uuid):
                self.completePassword(uuid: uuid)
            case .passkeyAssert(let rpID, let credID, let userHandle, let clientDataHash):
                self.completePasskeyAssert(rpID: rpID, credID: credID,
                                           userHandle: userHandle, clientDataHash: clientDataHash)
            case .passkeyRegister(let req, let userName):
                self.completePasskeyRegister(request: req, userName: userName)
            case .textInsert(let uuid, let password):
                self.completeTextInsert(uuid: uuid, password: password)
            }
        }
    }

    private func completePassword(uuid: String) {
        VaultStore.fill(uuid: uuid, url: serviceURL) { result in
            switch result {
            case .success(let c):
                let cred = ASPasswordCredential(user: c.user, password: c.password)
                DispatchQueue.main.async {
                    self.extensionContext.completeRequest(withSelectedCredential: cred)
                }
            case .failure(let err):
                self.vlog("fill failed \(err.localizedDescription)")
                DispatchQueue.main.async { self.cancel(with: .failed) }
            }
        }
    }

    /// Text-insert release — the credential fetch is identical to a fill;
    /// only the completion API differs (one string, not a credential).
    private func completeTextInsert(uuid: String, password: Bool) {
        VaultStore.fill(uuid: uuid, url: serviceURL) { result in
            switch result {
            case .success(let c):
                let text = password ? c.password : c.user
                DispatchQueue.main.async {
                    self.extensionContext.completeRequest(withTextToInsert: text) { _ in }
                }
            case .failure(let err):
                self.vlog("text insert failed \(err.localizedDescription)")
                DispatchQueue.main.async { self.cancel(with: .failed) }
            }
        }
    }

    /// Sign the OS's clientDataHash over origin — Face ID already passed,
    /// so this is the one call that releases a signature.
    private func completePasskeyAssert(rpID: String, credID: Data, userHandle: Data, clientDataHash: Data) {
        let publicKey: [String: Any] = [
            "clientDataHash": Self.b64url(clientDataHash),
            "rpId": rpID,
            "allowCredentials": [["type": "public-key", "id": Self.b64url(credID)]],
        ]
        vlog("passkeyGet rp=\(rpID) cdh=\(clientDataHash.count)B")
        VaultStore.passkeys(register: false, origin: "https://" + rpID, publicKey: publicKey) { result in
            switch result {
            case .success(let resp):
                guard let inner = resp["response"] as? [String: Any],
                      let authB64 = inner["authenticatorData"] as? String,
                      let sigB64 = inner["signature"] as? String,
                      let auth = Self.data(b64url: authB64),
                      let sig = Self.data(b64url: sigB64)
                else {
                    self.vlog("passkeyGet decode fail keys=\(resp.keys.sorted())")
                    DispatchQueue.main.async { self.cancel(with: .failed) }
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
            case .failure(let err):
                self.vlog("passkeyGet failed \(err.localizedDescription)")
                DispatchQueue.main.async { self.cancel(with: .failed) }
            }
        }
    }

    /// Mint a new ES256 credential on origin from the RP's publicKey
    /// request, hand the OS its attestation, and save the identity so
    /// future assertions can find the record.
    private func completePasskeyRegister(request req: ASPasskeyCredentialRequest, userName: String) {
        guard let identity = req.credentialIdentity as? ASPasskeyCredentialIdentity else {
            cancel(with: .failed)
            return
        }
        let rpID = identity.relyingPartyIdentifier
        var excluded: [[String: Any]] = []
        if #available(iOS 18.0, *) {
            for c in req.excludedCredentials ?? [] {
                excluded.append(["type": "public-key", "id": Self.b64url(c.credentialID)])
            }
        }
        let publicKey: [String: Any] = [
            "clientDataHash": Self.b64url(req.clientDataHash),
            "rp": ["id": rpID, "name": rpID],
            "user": [
                "id": Self.b64url(identity.userHandle),
                "name": userName,
                "displayName": userName,
            ],
            "pubKeyCredParams": [["type": "public-key", "alg": -7]],
            "excludeCredentials": excluded,
        ]
        vlog("reg rp=\(rpID) uh=\(identity.userHandle.count)B cdh=\(req.clientDataHash.count)B")
        VaultStore.passkeys(register: true, origin: "https://" + rpID, publicKey: publicKey) { result in
            switch result {
            case .success(let resp):
                guard let inner = resp["response"] as? [String: Any],
                      let attB64 = inner["attestationObject"] as? String,
                      let att = Self.data(b64url: attB64),
                      let rawID = (resp["rawId"] as? String) ?? (resp["id"] as? String),
                      let credID = Self.data(b64url: rawID)
                else {
                    self.vlog("passkeyRegister decode fail keys=\(resp.keys.sorted())")
                    DispatchQueue.main.async { self.cancel(with: .failed) }
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
                    userName: userName,
                    credentialID: credID,
                    userHandle: identity.userHandle,
                    recordIdentifier: rawID,
                )
                DispatchQueue.main.async {
                    ASCredentialIdentityStore.shared.saveCredentialIdentities([saved]) { _, _ in }
                    self.vlog("passkey registered rp=\(rpID)")
                    self.extensionContext.completeRegistrationRequest(using: registration)
                }
            case .failure(let err):
                self.vlog("passkeyRegister failed \(err.localizedDescription)")
                DispatchQueue.main.async { self.cancel(with: .failed) }
            }
        }
    }

    @objc private func cancelTapped() {
        cancel(with: .userCanceled)
    }

    private func cancel(with code: ASExtensionError.Code) {
        extensionContext.cancelRequest(withError: ASExtensionError(code))
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

    /// The origin speaks unpadded base64url (the WebAuthn encoding) —
    /// Data's own codec is the padded standard alphabet.
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

    private static func host(_ url: String) -> String {
        URL(string: url)?.host
            ?? url.replacingOccurrences(of: "https://", with: "")
                .replacingOccurrences(of: "http://", with: "")
                .components(separatedBy: "/").first
            ?? url
    }
}

extension CredentialProviderViewController: UITableViewDataSource, UITableViewDelegate {
    func tableView(_ tableView: UITableView, numberOfRowsInSection section: Int) -> Int {
        entries.count * (textInsert ? 2 : 1)
    }

    func tableView(_ tableView: UITableView, cellForRowAt indexPath: IndexPath) -> UITableViewCell {
        let cell = tableView.dequeueReusableCell(withIdentifier: "c", for: indexPath) as! HandoffCell
        let e = entries[indexPath.row / (textInsert ? 2 : 1)]
        if textInsert {
            cell.configure(name: indexPath.row % 2 == 0 ? e.name : "Password",
                           login: indexPath.row % 2 == 0
                               ? "Username: \(e.login)"
                               : (e.login.isEmpty ? e.name : e.login))
        } else {
            cell.configure(name: e.name, login: e.login)
        }
        return cell
    }

    func tableView(_ tableView: UITableView, didSelectRowAt indexPath: IndexPath) {
        tableView.deselectRow(at: indexPath, animated: false)
        let e = entries[indexPath.row / (textInsert ? 2 : 1)]
        if textInsert {
            status.text = "Confirming…"
            deferPending(.textInsert(uuid: e.uuid, password: indexPath.row % 2 == 1))
            return
        }
        // Passkey list pick: sign with the row's credential over the OS's
        // clientDataHash — same Face ID gate as an inline identity pick.
        if let params = passkeyParams, e.kind == "passkey",
           let credB64 = e.credId, let credID = Self.data(b64url: credB64),
           let handleB64 = e.userHandle, let handle = Self.data(b64url: handleB64) {
            status.text = "Confirming \(e.login.isEmpty ? e.name : e.login)…"
            deferPending(.passkeyAssert(
                rpID: params.relyingPartyIdentifier,
                credID: credID,
                userHandle: handle,
                clientDataHash: params.clientDataHash,
            ))
            return
        }
        guard !e.uuid.isEmpty else { return }
        status.text = "Confirming…"
        deferPending(.password(uuid: e.uuid))
    }
}

private final class HandoffCell: UITableViewCell {
    override init(style: UITableViewCell.CellStyle, reuseIdentifier: String?) {
        super.init(style: .subtitle, reuseIdentifier: reuseIdentifier)
        accessoryType = .disclosureIndicator
    }

    required init?(coder: NSCoder) { fatalError() }

    func configure(name: String, login: String) {
        var content = defaultContentConfiguration()
        content.text = name
        content.secondaryText = login
        content.secondaryTextProperties.color = .secondaryLabel
        content.image = UIImage(systemName: "key.fill")
        contentConfiguration = content
    }
}
