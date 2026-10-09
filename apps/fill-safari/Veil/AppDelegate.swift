//
//  AppDelegate.swift
//  Veil
//
//  Menu-bar accessory — the Mac helper half of slice 32. No dock icon, no
//  auto window: a status item, the Cmd-\ universal fill hotkey, and the
//  Safari-extension setup path on demand.
//

import Cocoa
import SafariServices
import ServiceManagement

@main
class AppDelegate: NSObject, NSApplicationDelegate {

    private var statusItem: NSStatusItem!

    // No storyboard: NSApplicationDelegate.main() alone only creates the
    // delegate from NSMainNibFile/NSMainStoryboardFile — both are gone.
    // The delegate reference is weak; keep it alive for the app's lifetime.
    static func main() {
        let app = NSApplication.shared
        autoreleasepool {
            let delegate = AppDelegate()
            app.delegate = delegate
            app.run()
        }
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        // One application: Dock icon + menu bar + vault window + fill.
        // The app stays running when windows close — the helper is the
        // always-on part; the window is the face.
        NSApp.setActivationPolicy(.regular)
        // A windowless accessory gets reaped by Automatic Termination —
        // the helper must survive idle to keep its hotkey live.
        ProcessInfo.processInfo.automaticTerminationSupportEnabled = false
        installMainMenu()
        installStatusItem()
        UniversalFill.shared.install()
        IdentitySync.shared.start()
        BridgeDaemon.shared.ensure()
        // Screen lock drops the host's confirm reuse window — a
        // session-mode grant must not outlive the lock.
        DistributedNotificationCenter.default().addObserver(
            forName: NSNotification.Name("com.apple.screenIsLocked"),
            object: nil, queue: .main,
        ) { _ in
            FillBridge.shared.relock()
        }
        // Menu-bar helper should outlive the session — relaunch at login.
        try? SMAppService.mainApp.register()
        // A manual launch (double-click, Spotlight, `open`) activates the
        // app; the login-item relaunch does not. Opening the vault on an
        // active launch is what a tapped app icon should do.
        DispatchQueue.main.async {
            if NSApp.isActive {
                VaultWindowController.shared.open()
            }
        }
    }

    private func installStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        if let button = statusItem.button {
            // The app icon's small variant is the brand mark — the generic
            // SF Symbol read as a random icon nobody recognized.
            if let icon = NSImage(named: "AppIcon") ?? NSApp.applicationIconImage {
                let small = NSImage(size: NSSize(width: 18, height: 18))
                small.lockFocus()
                icon.draw(
                    in: NSRect(origin: .zero, size: small.size),
                    from: .zero, operation: .sourceOver, fraction: 1,
                )
                small.unlockFocus()
                button.image = small
            }
        }
        let menu = NSMenu()
        menu.autoenablesItems = false

        let fill = NSMenuItem(
            title: "Fill with Veil",
            action: #selector(fillNow),
            keyEquivalent: "\\",
        )
        fill.keyEquivalentModifierMask = .command
        fill.target = self
        menu.addItem(fill)

        menu.addItem(.separator())

        let setup = NSMenuItem(
            title: "Enable Safari Extension…",
            action: #selector(showSafariSetup),
            keyEquivalent: "",
        )
        setup.target = self
        menu.addItem(setup)

        let autofill = NSMenuItem(
            title: "Enable AutoFill…",
            action: #selector(showAutoFillSetup),
            keyEquivalent: "",
        )
        autofill.target = self
        menu.addItem(autofill)

        let vault = NSMenuItem(
            title: "Open Veil…",
            action: #selector(openVault),
            keyEquivalent: "",
        )
        vault.target = self
        menu.addItem(vault)

        #if DEBUG
        let probe = NSMenuItem(
            title: "Type Probe",
            action: #selector(typeProbe),
            keyEquivalent: "",
        )
        probe.target = self
        menu.addItem(probe)
        #endif

        menu.addItem(.separator())

        let quit = NSMenuItem(
            title: "Quit Veil",
            action: #selector(quit),
            keyEquivalent: "q",
        )
        quit.target = self
        menu.addItem(quit)

        statusItem.menu = menu
    }

    // The standard app menu set — the process is created in code so the
    // menu bar is ours to build. Edit exists because the vault window's
    // WKWebView fields need copy/paste through the responder chain.
    private func installMainMenu() {
        let main = NSMenu()

        let appItem = NSMenuItem()
        let appMenu = NSMenu(title: "Veil")
        let items: [(String, Selector?, String)] = [
            ("About Veil", #selector(NSApplication.orderFrontStandardAboutPanel(_:)), ""),
            ("", nil, ""),
            ("Fill with Veil", #selector(fillNow), "\\"),
            ("Open Veil", #selector(openVault), "o"),
            ("", nil, ""),
            ("Hide Veil", #selector(NSApplication.hide(_:)), "h"),
            ("Quit Veil", #selector(quit), "q"),
        ]
        for (title, sel, key) in items {
            if title.isEmpty {
                appMenu.addItem(.separator())
                continue
            }
            let item = appMenu.addItem(withTitle: title, action: sel, keyEquivalent: key)
            item.target = sel == #selector(fillNow) || sel == #selector(openVault) || sel == #selector(quit) ? self : NSApp
        }
        appItem.submenu = appMenu
        main.addItem(appItem)

        let editItem = NSMenuItem()
        editItem.title = "Edit"
        let editMenu = NSMenu(title: "Edit")
        editMenu.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
        editMenu.addItem(withTitle: "Redo", action: Selector(("redo:")), keyEquivalent: "Z")
        editMenu.addItem(.separator())
        editMenu.addItem(withTitle: "Cut", action: Selector(("cut:")), keyEquivalent: "x")
        editMenu.addItem(withTitle: "Copy", action: Selector(("copy:")), keyEquivalent: "c")
        editMenu.addItem(withTitle: "Paste", action: Selector(("paste:")), keyEquivalent: "v")
        editMenu.addItem(withTitle: "Select All", action: Selector(("selectAll:")), keyEquivalent: "a")
        editItem.submenu = editMenu
        main.addItem(editItem)

        let winItem = NSMenuItem()
        winItem.title = "Window"
        let winMenu = NSMenu(title: "Window")
        winMenu.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        let open = winMenu.addItem(withTitle: "Open Veil", action: #selector(openVault), keyEquivalent: "")
        open.target = self
        winItem.submenu = winMenu
        main.addItem(winItem)
        NSApp.windowsMenu = winMenu

        NSApp.mainMenu = main
    }

    @objc private func fillNow() {
        UniversalFill.shared.trigger()
    }

    @objc private func showSafariSetup() {
        SFSafariApplication.showPreferencesForExtension(
            withIdentifier: "nyc.veil.fill.Extension",
        ) { _ in }
    }

    @objc private func showAutoFillSetup() {
        // System Settings → Passwords hosts the provider toggle on macOS.
        if let url = URL(string: "x-apple.systempreferences:com.apple.Passwords-Settings.extension") {
            NSWorkspace.shared.open(url)
        }
    }

    #if DEBUG
    @objc private func typeProbe() {
        UniversalFill.shared.typeProbe()
    }
    #endif

    @objc private func openVault() {
        VaultWindowController.shared.open()
    }

    @objc private func quit() {
        NSApp.terminate(nil)
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        return false
    }

    // Clicking the app while it runs — Dock, Launchpad, Finder — opens the
    // vault. The accessory has no dock icon of its own, so reopen is the
    // only "launch it again" signal we get.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag {
            VaultWindowController.shared.open()
        }
        return true
    }

    // MARK: - otpauth:// handler

    // Registered in Info.plist so `open otpauth://…` (QR readers, links in
    // non-browser apps) lands here instead of erroring. Same contract as
    // the extension: the human picks the item, the host confirms, nothing
    // enrolls on sight alone.
    func application(_ application: NSApplication, open urls: [URL]) {
        for url in urls {
            handleOTPAuth(url)
        }
    }

    private struct TOTPCandidate {
        let uuid: String
        let name: String
        let login: String
        let uri: String
    }

    private func handleOTPAuth(_ url: URL) {
        guard url.scheme?.lowercased() == "otpauth",
              let comps = URLComponents(url: url, resolvingAgainstBaseURL: false),
              comps.host?.lowercased() == "totp" else {
            return
        }
        let label = comps.path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        let issuer = comps.queryItems?.first(where: { $0.name.lowercased() == "issuer" })?.value
            ?? label.components(separatedBy: ":").first
            ?? ""
        DispatchQueue.global().async {
            var cands: [TOTPCandidate] = []
            if let entries = try? FillBridge.shared.list() {
                let needle = issuer.lowercased()
                for e in entries {
                    guard e["kind"] as? String == "login",
                          !(e["hasTotp"] as? Bool ?? false),
                          let uuid = e["uuid"] as? String else {
                        continue
                    }
                    let name = e["name"] as? String ?? ""
                    let login = e["login"] as? String ?? ""
                    // enrollTarget requires the scope URL to host-match one
                    // of the item's URIs — a login with none can never take a
                    // seed, so don't offer it.
                    guard let uri = (e["uris"] as? [String])?.first(where: { !$0.isEmpty }), !uri.isEmpty else {
                        continue
                    }
                    let hay = (name + " " + login + " " + uri).lowercased()
                    if needle.isEmpty || hay.contains(needle) {
                        cands.append(TOTPCandidate(uuid: uuid, name: name, login: login, uri: uri))
                    }
                }
            }
            DispatchQueue.main.async {
                self.offerTOTP(issuer: issuer, otpauth: url.absoluteString, candidates: cands)
            }
        }
    }

    private func offerTOTP(issuer: String, otpauth: String, candidates: [TOTPCandidate]) {
        NSApp.activate(ignoringOtherApps: true)
        let alert = NSAlert()
        alert.icon = NSImage(systemSymbolName: "lock.shield", accessibilityDescription: "Veil")
        if candidates.isEmpty {
            alert.messageText = "No sign-in for \(issuer.isEmpty ? "this authenticator" : issuer)"
            alert.informativeText = "Save the login first, then enroll the authenticator."
            alert.addButton(withTitle: "OK")
            alert.runModal()
            return
        }
        alert.messageText = "Save this authenticator to Veil?"
        alert.informativeText = issuer.isEmpty ? "Pick the sign-in it belongs to." : "Issuer: \(issuer)"
        let picks = Array(candidates.prefix(3))
        for c in picks {
            alert.addButton(withTitle: c.login.isEmpty ? c.name : "\(c.name) — \(c.login)")
        }
        alert.addButton(withTitle: "Cancel")
        let res = alert.runModal()
        let idx = res.rawValue - NSApplication.ModalResponse.alertFirstButtonReturn.rawValue
        guard idx >= 0 && idx < picks.count else {
            return
        }
        let pick = picks[idx]
        DispatchQueue.global().async {
            var reply: [String: Any]
            do {
                reply = try FillBridge.shared.enrollTotp(url: pick.uri, otpauth: otpauth, uuid: pick.uuid)
            } catch {
                reply = ["error": error.localizedDescription]
            }
            // "canceled" is the human's own Touch ID dismiss — nothing to say.
            if (reply["hasTotp"] as? Bool) != true, let why = reply["error"] as? String, why != "canceled" {
                DispatchQueue.main.async { self.failTOTP(why) }
            }
        }
    }

    private func failTOTP(_ why: String) {
        NSApp.activate(ignoringOtherApps: true)
        let alert = NSAlert()
        alert.messageText = "Could not save the authenticator"
        alert.informativeText = why == "need_login" ? "Sign in to Veil first." : "Veil said: \(why)"
        alert.addButton(withTitle: "OK")
        alert.runModal()
    }
}
