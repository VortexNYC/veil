//
//  VaultWindow.swift
//  Veil
//
//  The vault surface inside the app — a WKWebView on app.veil.nyc behind
//  the broker's own Touch ID confirm, the same path every fill uses.
//

import Cocoa
import WebKit

final class VaultWindowController: NSObject, NSWindowDelegate, WKNavigationDelegate {
    static let shared = VaultWindowController()
    private var window: NSWindow?
    private var web: WKWebView?
    private var unlocked = false
    private var unlockInFlight = false
    private weak var subtitleField: NSTextField?
    private weak var unlockButton: NSButton?

    /// Drop the webview's grip on the session: stop loads, scrub the
    /// injected bearer, detach delegates. Safe while the window closes.
    private func tearDownWeb() {
        web?.stopLoading()
        web?.evaluateJavaScript("try{sessionStorage.removeItem('veil.id_token');}catch(e){}", completionHandler: nil)
        web?.configuration.userContentController.removeAllUserScripts()
        web?.navigationDelegate = nil
        web = nil
    }

    /// Screen lock — the vault must not stay reachable past it.
    func lock() {
        unlocked = false
        unlockInFlight = false
        tearDownWeb()
        if let win = window {
            win.delegate = nil
            window = nil
            win.orderOut(nil)
        }
    }

    /// The window is already closing — never orderOut here (ordering out a
    /// mid-close window crashes the transform-animation teardown). Just
    /// drop our references.
    func windowWillClose(_ notification: Notification) {
        unlocked = false
        unlockInFlight = false
        tearDownWeb()
        window?.delegate = nil
        window = nil
    }

    /// Gated entry — the broker's confirm first, then the vault loads.
    /// Repeat opens on an unlocked app just show the window. `path` deep-
    /// links into the SPA ("/requests" from a notification tap).
    func open(path: String = "") {
        VaultScheme.syncInBackground()
        if !path.isEmpty { pendingPath = path }
        // No window means nothing passed the gate — a stale `unlocked`
        // would skip the splash and strand the vault.
        if window == nil { unlocked = false }
        if unlocked, let window {
            NSApp.activate(ignoringOtherApps: true)
            window.makeKeyAndOrderFront(nil)
            DispatchQueue.global().async {
                let tok = self.token()
                DispatchQueue.main.async {
                    guard !tok.isEmpty else {
                        // Refresh minted nothing — the session is gone and
                        // the stale webview would sit signed-out. Relock.
                        self.relockToSplash()
                        return
                    }
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

    /// The gate is the bridge's own "token" action — it runs the Touch ID
    /// confirm and refreshes/returns the human JWT in one round-trip, so a
    /// second LAContext here would double-prompt.
    @objc private func promptUnlock() {
        guard !unlockInFlight, !unlocked else { return }
        unlockInFlight = true
        unlockButton?.isEnabled = false
        subtitleField?.stringValue = "Waiting for Touch ID…"
        NSApp.activate(ignoringOtherApps: true)
        window?.makeKeyAndOrderFront(nil)
        DispatchQueue.global(qos: .userInitiated).async {
            BridgeDaemon.shared.ensure()
            let reply = try? FillBridge.shared.roundTrip(["action": "token"], timeout: 95)
            DispatchQueue.main.async {
                self.unlockInFlight = false
                self.unlockButton?.isEnabled = true
                if let tok = reply?["token"] as? String, !tok.isEmpty {
                    // The user may have closed the splash while Touch ID
                    // was up — do not mark unlocked with no window.
                    guard self.window != nil else { return }
                    self.unlocked = true
                    self.loadVault(token: tok)
                    return
                }
                if let err = reply?["error"] as? String, err == "need_login" {
                    self.subtitleField?.stringValue = "Sign in to Veil in your browser, then click Unlock."
                    DispatchQueue.global(qos: .userInitiated).async {
                        if let loginReply = try? FillBridge.shared.roundTrip(["action": "login"], timeout: 15),
                           let authURL = loginReply["auth_url"] as? String,
                           let url = URL(string: authURL) {
                            DispatchQueue.main.async { NSWorkspace.shared.open(url) }
                        }
                    }
                    return
                }
                // Denied or unreachable — stay on the splash; a webview
                // without a token would only bounce to login anyway.
                self.subtitleField?.stringValue = "Authenticate with Touch ID to view your vault."
            }
        }
    }

    /// The session died under a loaded vault — back to the gated splash on
    /// the same window, then prompt again.
    private func relockToSplash() {
        unlocked = false
        unlockInFlight = false
        tearDownWeb()
        showSplash()
        promptUnlock()
    }

    private func showSplash() {
        // Splash already up (e.g. open() re-entered while unlockInFlight) —
        // just front it; rebuilding would drop the live button/subtitle.
        if let window, web == nil {
            NSApp.activate(ignoringOtherApps: true)
            window.makeKeyAndOrderFront(nil)
            return
        }
        let win: NSWindow
        if let window {
            // Relock on the live window — swap the vault webview for the
            // splash content rather than churn the window itself.
            win = window
        } else {
            let frame = NSRect(x: 0, y: 0, width: 1200, height: 820)
            let w = NSWindow(
                contentRect: frame,
                styleMask: [.titled, .closable, .miniaturizable, .resizable],
                backing: .buffered,
                defer: false,
            )
            w.title = "Veil"
            w.minSize = NSSize(width: 800, height: 560)
            // A restored window would return with the vault already loaded —
            // past the gate. The window must only ever exist through open().
            w.isRestorable = false
            // ARC owns it — release-on-close frees the window out from under
            // our delegate callbacks.
            w.isReleasedWhenClosed = false
            w.delegate = self
            w.center()
            window = w
            win = w
        }

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
        subtitleField = subtitle
        unlockButton = btn
        web = nil
        NSApp.activate(ignoringOtherApps: true)
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

    private func token() -> String { (try? FillBridge.shared.token()) ?? "" }

    private var pendingPath = ""
    /// The vault is the React app bundled at Contents/Resources/vault and
    /// served to the webview over the veil-app scheme — one install, no
    /// remote fetch for the UI shell. The API still lives at origin.
    private var vaultOrigin = "veil-app://vault"

    /// Called only with a token the bridge just returned — never re-mints
    /// (a second "token" call would re-run the Touch ID gate).
    private func loadVault(token tok: String) {
        guard let window, !tok.isEmpty else { return }
        let cfg = WKWebViewConfiguration()
        cfg.setURLSchemeHandler(VaultScheme(), forURLScheme: "veil-app")
        if let script = injectToken(tok) {
            cfg.userContentController.addUserScript(script)
        }
        let view = WKWebView(frame: window.contentView!.bounds, configuration: cfg)
        view.autoresizingMask = [.width, .height]
        view.navigationDelegate = self
        window.contentView = view
        web = view
        view.load(URLRequest(url: URL(string: vaultOrigin + pendingPath)!))
    }

    /// The SPA bounces to its login/lock route (or out to the identity
    /// hosts) when the injected session dies — that belongs to the native
    /// gate, not a webview trip to Kratos. Cancel and relock instead.
    /// veil-app:///mac-settings opens the native Fill settings window, and
    /// login.veil.nyc/settings belongs in the system browser — Ory Elements
    /// standalone screens, not the embedded vault.
    func webView(_ webView: WKWebView,
                 decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        let url = navigationAction.request.url
        if url?.scheme == "veil-app" && url?.path == "/mac-settings" {
            decisionHandler(.cancel)
            DispatchQueue.main.async { SettingsWindowController.shared.open() }
            return
        }
        if url?.host == "login.veil.nyc", let url, url.path.hasPrefix("/settings") {
            decisionHandler(.cancel)
            NSWorkspace.shared.open(url)
            return
        }
        let bounce = (url?.scheme == "veil-app" && (url?.path == "/login" || url?.path == "/lock"))
            || url?.host == "accounts.veil.nyc"
            || url?.host == "login.veil.nyc"
            || url?.host == "id.veil.nyc"
        if bounce {
            decisionHandler(.cancel)
            DispatchQueue.main.async { self.relockToSplash() }
            return
        }
        decisionHandler(.allow)
    }

    /// veil-app://vault/<path> serves synchronously from disk (preferring a
    /// verified live bundle synced in the background from https://app.veil.nyc,
    /// falling back to Contents/Resources/vault). Answering WKURLSchemeTask
    /// synchronously on the caller's thread avoids WebKit dropping ES module
    /// script loads on custom schemes.
    private final class VaultScheme: NSObject, WKURLSchemeHandler {
        private let mime: [String: String] = [
            "html": "text/html", "js": "text/javascript", "mjs": "text/javascript",
            "css": "text/css", "json": "application/json", "svg": "image/svg+xml",
            "png": "image/png", "jpg": "image/jpeg", "webp": "image/webp",
            "woff": "font/woff", "woff2": "font/woff2", "ttf": "font/ttf",
            "ico": "image/x-icon", "map": "application/json", "txt": "text/plain",
            "wasm": "application/wasm",
        ]

        static let liveRoot: URL = {
            let base = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first!
            return base.appendingPathComponent("nyc.veil.fill/vault-live-v2", isDirectory: true)
        }()

        private static func activeRoot() -> URL {
            let bundled = Bundle.main.resourceURL!.appendingPathComponent("vault")
            let liveIndex = liveRoot.appendingPathComponent("index.html")
            guard let html = try? String(contentsOf: liveIndex, encoding: .utf8), !html.isEmpty else {
                return bundled
            }
            // Only use liveRoot if every /assets/... file referenced by its
            // index.html is present on disk — never serve a partial bundle.
            let pattern = #"/assets/[A-Za-z0-9._-]+"#
            if let regex = try? NSRegularExpression(pattern: pattern) {
                let ns = html as NSString
                let matches = regex.matches(in: html, range: NSRange(location: 0, length: ns.length))
                for m in matches {
                    let rel = String(ns.substring(with: m.range).dropFirst())
                    let asset = liveRoot.appendingPathComponent(rel)
                    if !FileManager.default.fileExists(atPath: asset.path) {
                        return bundled
                    }
                }
            }
            return liveRoot
        }

        /// Pulls index.html + all referenced /assets/* from https://app.veil.nyc
        /// in the background and swaps index.html last so activeRoot() only
        /// switches once the complete bundle is on disk.
        static func syncInBackground() {
            DispatchQueue.global(qos: .utility).async {
                guard let idxURL = URL(string: "https://app.veil.nyc/") else { return }
                let req = URLRequest(url: idxURL, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: 6)
                let sem = DispatchSemaphore(value: 0)
                var indexData: Data?
                URLSession.shared.dataTask(with: req) { data, res, err in
                    if err == nil, (res as? HTTPURLResponse)?.statusCode == 200 {
                        indexData = data
                    }
                    sem.signal()
                }.resume()
                _ = sem.wait(timeout: .now() + 7)
                guard let indexData, let html = String(data: indexData, encoding: .utf8), !html.isEmpty else {
                    return
                }
                let pattern = #"/assets/[A-Za-z0-9._-]+"#
                guard let regex = try? NSRegularExpression(pattern: pattern) else { return }
                let ns = html as NSString
                let matches = regex.matches(in: html, range: NSRange(location: 0, length: ns.length))
                for m in matches {
                    let assetPath = ns.substring(with: m.range)
                    let rel = String(assetPath.dropFirst())
                    let dest = liveRoot.appendingPathComponent(rel)
                    if FileManager.default.fileExists(atPath: dest.path) {
                        continue
                    }
                    guard let aURL = URL(string: "https://app.veil.nyc" + assetPath) else { return }
                    let aSem = DispatchSemaphore(value: 0)
                    var aData: Data?
                    URLSession.shared.dataTask(with: URLRequest(url: aURL, timeoutInterval: 8)) { d, r, e in
                        if e == nil, (r as? HTTPURLResponse)?.statusCode == 200 {
                            aData = d
                        }
                        aSem.signal()
                    }.resume()
                    _ = aSem.wait(timeout: .now() + 9)
                    guard let aData, !aData.isEmpty else { return }
                    try? FileManager.default.createDirectory(
                        at: dest.deletingLastPathComponent(),
                        withIntermediateDirectories: true,
                    )
                    try? aData.write(to: dest, options: .atomic)
                }
                try? FileManager.default.createDirectory(at: liveRoot, withIntermediateDirectories: true)
                try? indexData.write(to: liveRoot.appendingPathComponent("index.html"), options: .atomic)
            }
        }

        func webView(_ webView: WKWebView, start task: WKURLSchemeTask) {
            guard let url = task.request.url, url.scheme == "veil-app" else {
                task.didFailWithError(URLError(.unsupportedURL)); return
            }
            let root = Self.activeRoot()
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
