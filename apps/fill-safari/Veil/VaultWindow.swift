//
//  VaultWindow.swift
//  Veil
//
//  The vault surface inside the app — a WKWebView on app.veil.nyc behind
//  the broker's own Touch ID confirm, the same path every fill uses.
//

import Cocoa
import WebKit

final class VaultWindowController: NSObject {
    static let shared = VaultWindowController()
    private var window: NSWindow?
    private var web: WKWebView?
    private var unlocked = false

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
        DispatchQueue.global().async {
            let ok = self.unlock()
            DispatchQueue.main.async {
                if ok {
                    self.unlocked = true
                    self.loadVault()
                } else {
                    // close() releases the window mid-animation and AppKit
                    // crashes in _NSWindowTransformAnimation — hide it
                    // instead; a denied vault simply never exists.
                    self.window?.orderOut(nil)
                    self.window = nil
                }
            }
        }
    }

    /// The broker's own biometric confirm — same eval as a fill, through
    /// the bridge. LA called from this app's own context is not
    /// interactive enough to present (-1004); the daemon's is.
    private func unlock() -> Bool {
        do {
            let reply = try FillBridge.shared.roundTrip(["action": "unlock"], timeout: 95)
            return reply["ok"] as? Bool ?? false
        } catch {
            NSLog("veil: vault gate err %@", error.localizedDescription)
            return false
        }
    }

    private func showSplash() {
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
        win.delegate = nil

        let splash = NSView()
        let label = NSTextField(labelWithString: "Veil — confirm it's you to continue")
        label.textColor = .secondaryLabelColor
        splash.addSubview(label)
        label.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            label.centerXAnchor.constraint(equalTo: splash.centerXAnchor),
            label.centerYAnchor.constraint(equalTo: splash.centerYAnchor),
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
