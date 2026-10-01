//
//  CredentialProviderViewController.swift
//  Veil AutoFill (iOS)
//
//  The system credential-provider surface: when a field's AutoFill sheet
//  offers Veil (or the user taps one of our synced inline suggestions),
//  this view controller lists matching logins and completes the request.
//  Mirrors the macOS appex contract: Face ID before a secret leaves, no
//  silent releases, deny/down/unknown cancels — fail closed.
//  Passkeys are out of scope for this slice (macOS slice 32d covers them).
//

import UIKit
import AuthenticationServices
import LocalAuthentication

final class CredentialProviderViewController: ASCredentialProviderViewController {

    private var entries: [HandoffItem] = []
    private var serviceURL = ""
    private var pendingUUID: String?

    private let table = UITableView(frame: .zero, style: .plain)
    private let status = UILabel()
    private let emptyLabel = UILabel()

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
        footer.text = "Face ID required to fill"
        footer.font = .systemFont(ofSize: 12)
        footer.textColor = .secondaryLabel
        footer.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(footer)

        let cancel = UIButton(type: .system)
        cancel.setTitle("Cancel", for: .normal)
        cancel.addTarget(self, action: #selector(cancelTapped), for: .touchUpInside)
        cancel.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(cancel)

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
        ])

        self.view = root
    }

    // MARK: - ASCredentialProviderViewController

    /// User picked Veil from the AutoFill sheet — list logins matching the
    /// service identifier (or everything when the system sends none).
    override func prepareCredentialList(for serviceIdentifiers: [ASCredentialServiceIdentifier]) {
        vlog("prepareCredentialList ids=\(serviceIdentifiers.map { $0.identifier })")
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
    /// already knows which record; Face ID, then fill it.
    override func prepareInterfaceToProvideCredential(for credentialRequest: any ASCredentialRequest) {
        vlog("prepareInterfaceToProvideCredential")
        guard let req = credentialRequest as? ASPasswordCredentialRequest,
              let identity = req.credentialIdentity as? ASPasswordCredentialIdentity,
              let uuid = identity.recordIdentifier, !uuid.isEmpty
        else {
            cancel(with: .credentialIdentityNotFound)
            return
        }
        serviceURL = Self.url(for: identity.serviceIdentifier)
        status.text = "Confirming \(identity.user)…"
        // LAContext.evaluatePolicy fails with .notInteractive while the
        // hosted scene is still off-screen — defer to viewDidAppear.
        pendingUUID = uuid
        if isViewLoaded && view.window != nil {
            flushPending()
        }
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        flushPending()
    }

    private func flushPending() {
        guard let uuid = pendingUUID else { return }
        pendingUUID = nil
        release(uuid: uuid)
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

    // MARK: - Fill

    private func release(uuid: String, retryNotForeground: Bool = true) {
        let ctx = LAContext()
        ctx.evaluatePolicy(.deviceOwnerAuthentication,
                           localizedReason: "Veil needs Face ID before this password fills") { [weak self] ok, err in
            guard let self else { return }
            if !ok, retryNotForeground,
               (err as? LAError)?.code == .notInteractive {
                vlog("LA notInteractive — retrying once")
                DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
                    self.release(uuid: uuid, retryNotForeground: false)
                }
                return
            }
            guard ok else {
                self.vlog("LA failed \((err as? LAError)?.code.rawValue ?? 0)")
                DispatchQueue.main.async { self.cancel(with: .userCanceled) }
                return
            }
            VaultStore.fill(uuid: uuid, url: self.serviceURL) { result in
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
        entries.count
    }

    func tableView(_ tableView: UITableView, cellForRowAt indexPath: IndexPath) -> UITableViewCell {
        let cell = tableView.dequeueReusableCell(withIdentifier: "c", for: indexPath) as! HandoffCell
        let e = entries[indexPath.row]
        cell.configure(name: e.name, login: e.login)
        return cell
    }

    func tableView(_ tableView: UITableView, didSelectRowAt indexPath: IndexPath) {
        tableView.deselectRow(at: indexPath, animated: false)
        let uuid = entries[indexPath.row].uuid
        guard !uuid.isEmpty else { return }
        status.text = "Confirming…"
        release(uuid: uuid)
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
