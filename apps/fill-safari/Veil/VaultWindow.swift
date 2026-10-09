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
    /// Repeat opens on an unlocked app just show the window.
    func open() {
        if unlocked, let window {
            NSApp.activate(ignoringOtherApps: true)
            window.makeKeyAndOrderFront(nil)
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

    private func loadVault() {
        guard let window else { return }
        DispatchQueue.global().async {
            let tok = self.token()
            DispatchQueue.main.async {
                let cfg = WKWebViewConfiguration()
                if !tok.isEmpty, let script = self.injectToken(tok) {
                    cfg.userContentController.addUserScript(script)
                }
                let view = WKWebView(frame: window.contentView!.bounds, configuration: cfg)
                view.autoresizingMask = [.width, .height]
                window.contentView = view
                self.web = view
                view.load(URLRequest(url: URL(string: "https://app.veil.nyc")!))
            }
        }
    }
}
