import Cocoa

// Minimal native-app fill fixture: a username field + a secure field,
// the same surface any non-browser login dialog presents over AX.
final class App: NSObject, NSApplicationDelegate {
    var window: NSWindow!

    func applicationDidFinishLaunching(_ note: Notification) {
        let win = NSWindow(
            contentRect: NSRect(x: 400, y: 300, width: 380, height: 220),
            styleMask: [.titled, .closable],
            backing: .buffered,
            defer: false)
        win.title = "TestLogin"
        win.level = .floating
        let content = NSView(frame: NSRect(x: 0, y: 0, width: 380, height: 220))

        let user = NSTextField(frame: NSRect(x: 40, y: 130, width: 300, height: 28))
        user.placeholderString = "Username"
        user.setAccessibilityIdentifier("username")

        let pass = NSSecureTextField(frame: NSRect(x: 40, y: 80, width: 300, height: 28))
        pass.placeholderString = "Password"
        pass.setAccessibilityIdentifier("password")

        content.addSubview(user)
        content.addSubview(pass)
        win.contentView = content
        win.makeKeyAndOrderFront(nil)
        window = win
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ app: NSApplication) -> Bool { true }
}

let app = NSApplication.shared
app.setActivationPolicy(.regular)
let delegate = App()
app.delegate = delegate
app.activate(ignoringOtherApps: true)
app.run()
