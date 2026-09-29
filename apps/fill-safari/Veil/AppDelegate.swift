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
        NSApp.setActivationPolicy(.accessory)
        // A windowless accessory gets reaped by Automatic Termination —
        // the helper must survive idle to keep its hotkey live.
        ProcessInfo.processInfo.automaticTerminationSupportEnabled = false
        installStatusItem()
        UniversalFill.shared.install()
        IdentitySync.shared.start()
        // Menu-bar helper should outlive the session — relaunch at login.
        try? SMAppService.mainApp.register()
    }

    private func installStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        if let button = statusItem.button {
            button.image = NSImage(
                systemSymbolName: "lock.shield",
                accessibilityDescription: "Veil",
            )
        }
        let menu = NSMenu()
        menu.autoenablesItems = false

        let fill = NSMenuItem(
            title: "Fill",
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
        if let url = URL(string: "https://app.veil.nyc") {
            NSWorkspace.shared.open(url)
        }
    }

    @objc private func quit() {
        NSApp.terminate(nil)
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        return false
    }
}
