//
//  VaultWindow.swift
//  Veil
//
//  The vault surface inside the app — a WKWebView on app.veil.nyc behind
//  the broker's own Touch ID confirm, the same path every fill uses.
//

import Cocoa
import LocalAuthentication
import WebKit

final class VaultWindowController: NSObject, NSWindowDelegate {
    static let shared = VaultWindowController()
    private var window: NSWindow?
    private var web: WKWebView?
    private var unlocked = false

    func lock() {
        unlocked = false
        web?.evaluateJavaScript("try{sessionStorage.removeItem('veil.id_token');}catch(e){}", completionHandler: nil)
        web?.configuration.userContentController.removeAllUserScripts()
        web = nil
        window?.orderOut(nil)
        window = nil
    }

    func windowWillClose(_ notification: Notification) {
        lock()
    }

    /// Gated entry — the broker's confirm first, then the vault loads.
    /// Repeat opens on an unlocked app just show the window. `path` deep-
    /// links into the SPA ("/requests" from a notification tap).
    func open(path: String = "") {
        if !path.isEmpty { pendingPath = path }
        if unlocked, let window {
            NSApp.activate(ignoringOtherApps: true)
            window.makeKeyAndOrderFront(nil)
            DispatchQueue.global().async {
                let tok = self.token()
                guard !tok.isEmpty else { return }
                DispatchQueue.main.async {
                    if let web = self.web, let script = self.injectToken(tok) {
                        web.configuration.userContentController.removeAllUserScripts()
                        web.configuration.userContentController.addUserScript(script)
                        web.evaluateJavaScript(
                            "try{sessionStorage.setItem('veil.id_token',\"\(tok)\");}catch(e){}",
                            completionHandler: nil
                        )
                        if !self.pendingPath.isEmpty {
                            web.load(URLRequest(url: URL(string: self.vaultOrigin + self.pendingPath)!))
                            self.pendingPath = ""
                        }
                    }
                }
            }
            return
        }
        showSplash()
        promptUnlock()
    }

    @objc private func promptUnlock() {
        NSApp.activate(ignoringOtherApps: true)
        window?.makeKeyAndOrderFront(nil)
        let ctx = LAContext()
        var canErr: NSError?
        if ctx.canEvaluatePolicy(.deviceOwnerAuthentication, error: &canErr) {
            ctx.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: "unlock Veil") { ok, evalErr in
                if ok {
                    DispatchQueue.main.async {
                        self.unlocked = true
                        self.loadVault()
                    }
                    return
                }
                if let laErr = evalErr as? LAError, laErr.code == .notInteractive {
                    self.unlockViaBridge()
                }
            }
            return
        }
        unlockViaBridge()
    }

    private func unlockViaBridge() {
        DispatchQueue.global().async {
            let reply = try? FillBridge.shared.roundTrip(["action": "unlock"], timeout: 95)
            let ok = reply?["ok"] as? Bool ?? false
            if ok {
                DispatchQueue.main.async {
                    self.unlocked = true
                    self.loadVault()
                }
            }
        }
    }

    private func showSplash() {
        if let window {
            NSApp.activate(ignoringOtherApps: true)
            window.makeKeyAndOrderFront(nil)
            return
        }
        let frame = NSRect(x: 0, y: 0, width: 1200, height: 820)
        let win = NSWindow(
            contentRect: frame,
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false,
        )
        win.title = "Veil"
        win.minSize = NSSize(width: 800, height: 560)
        // A restored window would return with the vault already loaded —
        // past the gate. The window must only ever exist through open().
        win.isRestorable = false
        win.delegate = self

        let splash = NSView()
        let lockIcon = NSImageView()
        lockIcon.image = NSImage(
            systemSymbolName: "lock.fill",
            accessibilityDescription: "Locked"
        )?.withSymbolConfiguration(.init(pointSize: 36, weight: .medium))
        lockIcon.contentTintColor = .secondaryLabelColor

        let title = NSTextField(labelWithString: "Veil is locked")
        title.font = .systemFont(ofSize: 18, weight: .semibold)
        title.textColor = .labelColor

        let subtitle = NSTextField(labelWithString: "Authenticate with Touch ID to view your vault.")
        subtitle.font = .systemFont(ofSize: 13)
        subtitle.textColor = .secondaryLabelColor

        let btn = NSButton(title: "Unlock with Touch ID", target: self, action: #selector(promptUnlock))
        btn.bezelStyle = .rounded
        btn.controlSize = .large
        btn.keyEquivalent = "\r"

        let stack = NSStackView(views: [lockIcon, title, subtitle, btn])
        stack.orientation = .vertical
        stack.alignment = .centerX
        stack.spacing = 12
        stack.setCustomSpacing(20, after: subtitle)
        stack.translatesAutoresizingMaskIntoConstraints = false
        splash.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.centerXAnchor.constraint(equalTo: splash.centerXAnchor),
            stack.centerYAnchor.constraint(equalTo: splash.centerYAnchor),
        ])
        win.contentView = splash
        window = win
        web = nil
        NSApp.activate(ignoringOtherApps: true)
        win.center()
        win.makeKeyAndOrderFront(nil)
    }

    /// The SPA authenticates on a sessionStorage bearer — inject the
    /// broker's human token before page JS runs and the vault lands
    /// signed in. One auth event (the gate), not a second Ory login.
    private func injectToken(_ token: String) -> WKUserScript? {
        let src = "try{sessionStorage.setItem('veil.id_token',\"\(token)\");}catch(e){}"
        return WKUserScript(
            source: src,
            injectionTime: .atDocumentStart,
            forMainFrameOnly: true,
        )
    }

    private func token() -> String {
        do {
            let reply = try FillBridge.shared.roundTrip(["action": "token"], timeout: 30)
            return reply["token"] as? String ?? ""
        } catch {
            return ""
        }
    }

    private var pendingPath = ""
    /// The vault is the React app bundled at Contents/Resources/vault and
    /// served to the webview over the veil-app scheme — one install, no
    /// remote fetch for the UI shell. The API still lives at origin.
    private var vaultOrigin = "veil-app://vault"

    private func loadVault() {
        guard let window else { return }
        DispatchQueue.global().async {
            let tok = self.token()
            DispatchQueue.main.async {
                let cfg = WKWebViewConfiguration()
                cfg.setURLSchemeHandler(VaultScheme(), forURLScheme: "veil-app")
                if !tok.isEmpty, let script = self.injectToken(tok) {
                    cfg.userContentController.addUserScript(script)
                }
                let view = WKWebView(frame: window.contentView!.bounds, configuration: cfg)
                view.autoresizingMask = [.width, .height]
                window.contentView = view
                self.web = view
                view.load(URLRequest(url: URL(string: self.vaultOrigin + self.pendingPath)!))
            }
        }
    }

    /// veil-app://vault/<path> → Contents/Resources/vault/<path>; unknown
    /// paths fall back to index.html so TanStack routes resolve.
    private final class VaultScheme: NSObject, WKURLSchemeHandler {
        private let mime: [String: String] = [
            "html": "text/html", "js": "text/javascript", "mjs": "text/javascript",
            "css": "text/css", "json": "application/json", "svg": "image/svg+xml",
            "png": "image/png", "jpg": "image/jpeg", "webp": "image/webp",
            "woff": "font/woff", "woff2": "font/woff2", "ttf": "font/ttf",
            "ico": "image/x-icon", "map": "application/json", "txt": "text/plain",
            "wasm": "application/wasm",
        ]

        func webView(_ webView: WKWebView, start task: WKURLSchemeTask) {
            guard let url = task.request.url, url.scheme == "veil-app" else {
                task.didFailWithError(URLError(.unsupportedURL)); return
            }
            let root = Bundle.main.resourceURL!.appendingPathComponent("vault")
            var path = url.path.isEmpty ? "/index.html" : url.path
            var file = root.appendingPathComponent(String(path.dropFirst()))
            var isDir: ObjCBool = false
            if !FileManager.default.fileExists(atPath: file.path, isDirectory: &isDir) || isDir.boolValue {
                file = root.appendingPathComponent("index.html")
                path = "/index.html"
            }
            guard let data = try? Data(contentsOf: file) else {
                task.didFailWithError(URLError(.fileDoesNotExist)); return
            }
            let ext = (path as NSString).pathExtension.lowercased()
            let res = URLResponse(url: url, mimeType: mime[ext] ?? "application/octet-stream",
                                  expectedContentLength: data.count, textEncodingName: nil)
            task.didReceive(res)
            task.didReceive(data)
            task.didFinish()
        }

        func webView(_ webView: WKWebView, stop task: WKURLSchemeTask) {}
    }
}
