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

    /// User picked Veil from the field's AutoFill menu — list every login we
    /// can offer for this service (or all of them when the system sends no
    /// identifier). Passkey requests are out of scope for this slice.
    override func prepareCredentialList(
        for serviceIdentifiers: [ASCredentialServiceIdentifier],
        requestParameters: ASPasskeyCredentialRequestParameters?,
    ) {
        vlog("prepareCredentialList ids=\(serviceIdentifiers.map { $0.identifier })")
        if requestParameters != nil {
            cancel(with: .failed)
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
        guard row >= 0, row < entries.count,
              let uuid = entries[row]["uuid"] as? String, !uuid.isEmpty
        else { return }
        status.stringValue = "Veil — confirming…"
        fill(uuid: uuid)
    }

    @objc private func cancelRequest() {
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
