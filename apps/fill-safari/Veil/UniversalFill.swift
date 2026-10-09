//
//  UniversalFill.swift
//  Veil
//
//  Cmd-\ in any Mac app — the 1Password-shaped gesture. Carbon hotkey
//  (consumes the keystroke, no Accessibility needed to *listen*), AX reads
//  the frontmost app's focused field (Accessibility *is* required for that),
//  the host matches items bound to `app://<bundleID>`, the existing
//  Touch ID confirm gates the secret, and CGEvent keystrokes type it back
//  into the field. The secret passes through the helper the same way it
//  passes through the browser extension — a fill client, never stored.
//

import Cocoa
import Carbon
import ApplicationServices

final class UniversalFill: NSObject {
    static let shared = UniversalFill()

    private var keyTap: CFMachPort?
    private var targetApp: NSRunningApplication?
    private var picker: FillPanel?
    private var generation = 0
    /// Field-focus watcher. AXObserver notifications do not reach a
    /// sandboxed process for other apps — this polls the front app's
    /// focused element at 300ms instead. Three AX reads a second, nothing
    /// else: the cheap path to the browser-extension behavior.
    private var focusTimer: Timer?
    private var lastFocus: (pid: pid_t, role: String, pos: CGPoint)?
    /// Suppresses the auto dropdown while a pick's eval is in flight —
    /// the Touch ID prompt pulls focus and would kill itself.
    private var fillInFlight = false

    /// Cmd-\ via a session event tap — consumes the keystroke before the
    /// target app sees it. Carbon hotkeys miss synthetic and quiesced
    /// events; the tap is the same path the picker's arrows use. Reading
    /// the focused field still needs Accessibility trust.
    func install() {
        let mask = CGEventMask(1 << CGEventType.keyDown.rawValue)
        keyTap = CGEvent.tapCreate(
            tap: .cgSessionEventTap, place: .headInsertEventTap,
            options: .defaultTap, eventsOfInterest: mask,
            callback: { _, type, event, _ -> Unmanaged<CGEvent>? in
                if type == .tapDisabledByTimeout || type == .tapDisabledByUserInput {
                    if let tap = UniversalFill.shared.keyTap {
                        CGEvent.tapEnable(tap: tap, enable: true)
                    }
                    return Unmanaged.passUnretained(event)
                }
                guard type == .keyDown else { return Unmanaged.passUnretained(event) }
                let code = event.getIntegerValueField(.keyboardEventKeycode)
                let flags = event.flags
                // 1Password parity: ⌘\ and ⌘⇧Space both open Quick Access.
                let backslash = code == 42
                    && flags.contains(.maskCommand)
                    && flags.intersection([.maskShift, .maskControl, .maskAlternate]).isEmpty
                let cmdShiftSpace = code == 49
                    && flags.contains(.maskCommand)
                    && flags.contains(.maskShift)
                    && flags.intersection([.maskControl, .maskAlternate]).isEmpty
                guard backslash || cmdShiftSpace else { return Unmanaged.passUnretained(event) }
                DispatchQueue.main.async { UniversalFill.shared.trigger() }
                return nil // the gesture is ours — the field never sees it
            },
            userInfo: nil,
        )
        if let keyTap {
            let src = CFMachPortCreateRunLoopSource(nil, keyTap, 0)
            CFRunLoopAddSource(CFRunLoopGetMain(), src, .commonModes)
            CGEvent.tapEnable(tap: keyTap, enable: true)
            NSLog("veil: key tap installed")
        } else {
            NSLog("veil: key tap refused — Input Monitoring not granted")
        }
        if !AXIsProcessTrusted() {
            NSLog("veil: ax untrusted — prompting")
            AXIsProcessTrustedWithOptions(
                [kAXTrustedCheckOptionPrompt.takeUnretainedValue(): true] as CFDictionary)
        }
        focusTimer = Timer.scheduledTimer(withTimeInterval: 0.3, repeats: true) { [weak self] _ in
            self?.pollFocus()
        }
        NSLog("veil: focus watch polling 300ms (trusted=%d)", AXIsProcessTrusted() ? 1 : 0)
    }

    /// Front app's focused element, ~3x a second. Only transitions do work:
    /// the same field under the same app is free. Credential fields get the
    /// anchored dropdown; everything else closes it.
    private func pollFocus() {
        guard AXIsProcessTrusted(), !fillInFlight else { return }
        guard let front = NSWorkspace.shared.frontmostApplication,
              let bundleID = front.bundleIdentifier,
              bundleID != Bundle.main.bundleIdentifier
        else { if picker?.mode == .anchored { picker?.dismiss(); picker = nil }; lastFocus = nil; return }

        let app = AXUIElementCreateApplication(front.processIdentifier)
        var ref: CFTypeRef?
        let err = AXUIElementCopyAttributeValue(app, kAXFocusedUIElementAttribute as CFString, &ref)
        guard let el = ref else {
            if err == .apiDisabled {
                // Electron/Chromium apps keep their AX tree off until a
                // client asks — flip it once per app switch.
                AXUIElementSetAttributeValue(
                    app, "AXEnhancedUserInterface" as CFString,
                    kCFBooleanTrue)
                AXUIElementSetAttributeValue(
                    app, "AXManualAccessibility" as CFString, kCFBooleanTrue)
            } else if err != .success, err != .noValue {
                NSLog("veil: focus read err=%d in %@", err.rawValue, bundleID)
            }
            if picker?.mode == .anchored { picker?.dismiss(); picker = nil }
            lastFocus = nil
            return
        }
        let field = el as! AXUIElement
        var roleRef: CFTypeRef?
        var posRef: CFTypeRef?
        AXUIElementCopyAttributeValue(field, kAXRoleAttribute as CFString, &roleRef)
        AXUIElementCopyAttributeValue(field, kAXPositionAttribute as CFString, &posRef)
        let role = roleRef as? String ?? ""
        var pos = CGPoint.zero
        if let p = posRef { AXValueGetValue(p as! AXValue, .cgPoint, &pos) }

        let cur = (pid: front.processIdentifier, role: role, pos: pos)
        if let last = lastFocus, last == cur { return }
        lastFocus = cur

        let credentialish = role == "AXSecureTextField" || role == "AXTextField"
        NSLog("veil: focusEvent role=%@ in %@", role, bundleID)
        guard credentialish else {
            if picker?.mode == .anchored { picker?.dismiss(); picker = nil }
            return
        }
        targetApp = front
        let gen = { self.generation += 1; return self.generation }()
        DispatchQueue.global().async { [self] in
            let entries: [[String: Any]]
            do {
                entries = try FillBridge.shared.match(app: bundleID)
            } catch {
                NSLog("veil: focus match err %@", error.localizedDescription)
                return
            }
            NSLog("veil: focus match=%d", entries.count)
            DispatchQueue.main.async { [self] in
                guard gen == generation, !fillInFlight else { return }
                picker?.dismiss()
                picker = nil
                if entries.isEmpty { return }
                let panel = FillPanel(entries: entries, at: field, mode: .anchored) { [weak self] uuid in
                    self?.fillAndType(uuid: uuid, passwordOnly: role == "AXSecureTextField", panel: nil)
                }
                picker = panel
                panel.show()
            }
        }
    }

    func trigger() {
        NSLog("veil: trigger")
        guard let front = NSWorkspace.shared.frontmostApplication,
              front.bundleIdentifier != Bundle.main.bundleIdentifier,
              let bundleID = front.bundleIdentifier
        else {
            NSLog("veil: no front app")
            return
        }
        // A re-trigger supersedes whatever was up: drop the panel and bump
        // the generation so a fill still awaiting Touch ID types nothing.
        generation += 1
        picker?.dismiss()
        picker = nil
        targetApp = front
        NSLog("veil: front=%@", bundleID)

        // Input Monitoring can be granted after launch — retry the tap.
        if keyTap == nil { install() }

        // AX reads need Accessibility trust — prompt once, then bail until
        // the user grants; without it we cannot tell a password field from
        // a plain one and blind-typing secrets is not acceptable.
        guard AXIsProcessTrusted() else {
            NSLog("veil: ax not trusted, prompting")
            AXIsProcessTrustedWithOptions(
                [kAXTrustedCheckOptionPrompt.takeUnretainedValue(): true] as CFDictionary,
            )
            return
        }

        let app = AXUIElementCreateApplication(front.processIdentifier)
        var ref: CFTypeRef?
        AXUIElementCopyAttributeValue(
            app, kAXFocusedUIElementAttribute as CFString, &ref,
        )
        let field = ref as! AXUIElement?
        var roleRef: CFTypeRef?
        var role = ""
        if let field {
            AXUIElementCopyAttributeValue(
                field, kAXRoleAttribute as CFString, &roleRef,
            )
            role = roleRef as? String ?? ""
        }
        let passwordOnly = role == "AXSecureTextField"
        let gen = generation
        let appName = front.localizedName ?? bundleID

        // The socket round-trip blocks — never on main.
        DispatchQueue.global().async { [self] in
            let entries: [[String: Any]]
            do {
                entries = try FillBridge.shared.match(app: bundleID)
            } catch {
                NSLog("veil: match error %@", error.localizedDescription)
                DispatchQueue.main.async { self.showNote("Veil fill host unreachable — run `veil fill install`") }
                return
            }
            DispatchQueue.main.async { [self] in
                guard gen == generation else { return }
                NSLog("veil: matches=%d", entries.count)

                if entries.isEmpty {
                    showNote("No Veil items bound to \(appName)")
                    return
                }
                // One match is still offered, not silently filled — the row
                // stays up naming the credential while Touch ID gates it.
                let panel = FillPanel(entries: entries, at: field, mode: .palette) { [weak self] uuid in
                    self?.fillAndType(uuid: uuid, passwordOnly: passwordOnly, panel: nil)
                }
                picker = panel
                panel.show()
                if entries.count == 1, let uuid = entries[0]["uuid"] as? String {
                    fillAndType(uuid: uuid, passwordOnly: passwordOnly, panel: panel)
                }
            }
        }
    }

    /// `panel` non-nil is the single-match auto-offer: typing only lands if
    /// that same panel is still the live, visible picker — Escape or a
    /// re-trigger during the Touch ID prompt drops the result.
    private func fillAndType(uuid: String, passwordOnly: Bool, panel: FillPanel?) {
        guard let front = targetApp,
              let bundleID = front.bundleIdentifier
        else { return }
        let gen = generation
        fillInFlight = true
        // The app regains focus before typing — the picker menu borrowed it.
        front.activate()
        DispatchQueue.global().async {
            defer { DispatchQueue.main.async { self.fillInFlight = false } }
            do {
                let (entries, _) = try FillBridge.shared.fill(app: bundleID, uuid: uuid)
                guard let first = entries.first else { return }
                let login = first["login"] as? String ?? ""
                let password = first["password"] as? String ?? ""
                DispatchQueue.main.async {
                    guard gen == self.generation else { return }
                    if let panel {
                        guard self.picker === panel, panel.isVisible else { return }
                        panel.dismiss()
                        self.picker = nil
                    }
                    // Let the app settle after activation — typing into a
                    // half-raised window eats the first keystrokes. The type
                    // loop sleeps per char — keep it off main.
                    DispatchQueue.global().asyncAfter(deadline: .now() + 0.15) {
                        if passwordOnly {
                            self.type(password, into: front.processIdentifier)
                        } else {
                            if !login.isEmpty {
                                self.type(login, into: front.processIdentifier)
                                self.tab(into: front.processIdentifier)
                            }
                            self.type(password, into: front.processIdentifier)
                        }
                    }
                }
            } catch {
                // Fail closed — nothing typed, nothing shown.
            }
        }
    }

    #if DEBUG
    /// Debug-only probe — types a canned string through the same CGEvent
    /// path fill uses, no secrets involved. Proves postToPid delivery.
    func typeProbe() {
        guard let front = targetApp ?? NSWorkspace.shared.frontmostApplication,
              front.bundleIdentifier != Bundle.main.bundleIdentifier
        else { return }
        type("veil-typing-probe", into: front.processIdentifier)
    }
    #endif

    /// Arbitrary text via Unicode key events — no keycode table, so
    /// passwords with any characters type correctly.
    private func type(_ text: String, into pid: pid_t) {
        for ch in text {
            if let down = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true) {
                var utf16 = Array(String(ch).utf16)
                down.keyboardSetUnicodeString(stringLength: utf16.count, unicodeString: &utf16)
                down.postToPid(pid)
            }
            if let up = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false) {
                up.postToPid(pid)
            }
            usleep(2000)
        }
    }

    private func tab(into pid: pid_t) {
        if let down = CGEvent(keyboardEventSource: nil, virtualKey: UInt16(kVK_Tab), keyDown: true) {
            down.postToPid(pid)
        }
        if let up = CGEvent(keyboardEventSource: nil, virtualKey: UInt16(kVK_Tab), keyDown: false) {
            up.postToPid(pid)
        }
        usleep(30000)
    }

    /// Quiet single-shot notice — a fading panel at the cursor, not a
    /// dialog. Silence with context, like the browser path's no-match case.
    private func showNote(_ text: String) {
        let note = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 300, height: 34),
            styleMask: [.nonactivatingPanel, .borderless],
            backing: .buffered, defer: false,
        )
        note.level = .floating
        note.isOpaque = false
        note.hasShadow = true
        note.backgroundColor = NSColor.windowBackgroundColor.withAlphaComponent(0.96)
        let label = NSTextField(labelWithString: text)
        label.textColor = .secondaryLabelColor
        label.alignment = .center
        label.frame = NSRect(x: 0, y: 7, width: 300, height: 20)
        note.contentView = label
        let at = NSEvent.mouseLocation
        note.setFrameOrigin(NSPoint(x: at.x - 150, y: at.y - 44))
        note.orderFront(nil)
        DispatchQueue.main.asyncAfter(deadline: .now() + 2.5) {
            note.orderOut(nil)
        }
    }
}

/// Floating match picker — the 1Password Quick Access shape. Centered on
/// the active screen with a real search field; when a credential field is
/// focused in the target app the card anchors under it instead. The panel
/// is keyable but never activates the app — typing goes to the search,
/// arrows/Enter/Esc are forwarded from the field's command selector.
/// Mouse rows still pick on click.
final class FillPanel: NSObject, NSTableViewDataSource, NSTableViewDelegate, NSSearchFieldDelegate {
    /// palette — Cmd-\ quick access: centered card, real search, keyable.
    /// anchored — field-focus dropdown: slim card under the field, never
    /// takes key (typing belongs to the field), nav via a session tap.
    enum Mode { case anchored, palette }

    let mode: Mode
    private let panel: NSPanel
    private let table = NSTableView()
    private let allEntries: [[String: Any]]
    private var shown: [[String: Any]]
    private let search = NSSearchField()
    private let onPick: (String) -> Void
    private var globalMonitor: Any?
    private var eventTap: CFMachPort?
    private var tapSource: CFRunLoopSource?

    init(entries: [[String: Any]], at field: AXUIElement?, mode: Mode,
         onPick: @escaping (String) -> Void) {
        self.allEntries = entries
        self.shown = entries
        self.mode = mode
        self.onPick = onPick
        panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 320, height: 100),
            styleMask: [.nonactivatingPanel, .titled, .fullSizeContentView],
            backing: .buffered, defer: false,
        )
        super.init()
        panel.titleVisibility = .hidden
        panel.titlebarAppearsTransparent = true
        panel.isMovableByWindowBackground = false
        panel.level = .floating
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = true
        // Palette is keyable without activating — the search field takes
        // real typing while the target app stays front. Anchored never
        // becomes key — the field owns the keyboard.
        panel.becomesKeyOnlyIfNeeded = (mode == .anchored)
        panel.isFloatingPanel = true

        let col = NSTableColumn(identifier: NSUserInterfaceItemIdentifier("c"))
        table.addTableColumn(col)
        table.headerView = nil
        table.rowHeight = 44
        table.intercellSpacing = NSSize(width: 0, height: 2)
        table.backgroundColor = .clear
        table.style = .plain
        table.selectionHighlightStyle = .regular
        table.dataSource = self
        table.delegate = self
        table.target = self
        table.doubleAction = #selector(confirm)
        table.action = #selector(rowClicked(_:))

        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.scrollerStyle = .overlay

        // Vibrancy card — palette carries a search bar; anchored is just
        // the list and the hint line under the field.
        let searchH: CGFloat = mode == .palette ? 34 : 0
        let width: CGFloat = mode == .palette ? 340 : 300
        let listH = min(CGFloat(entries.count) * 46 + 8, mode == .palette ? 276 : 240)
        let hintH: CGFloat = 24
        let height = searchH + listH + hintH
        let card = NSVisualEffectView(frame: NSRect(x: 0, y: 0, width: width, height: height))
        card.material = .popover
        card.blendingMode = .behindWindow
        card.state = .active
        card.wantsLayer = true
        card.layer?.cornerRadius = 12
        card.layer?.masksToBounds = true
        card.layer?.borderWidth = 1
        card.layer?.borderColor = NSColor.separatorColor.cgColor

        if mode == .palette {
            search.placeholderString = "Search items"
            search.font = .systemFont(ofSize: 14)
            search.focusRingType = .none
            search.isBezeled = true
            search.bezelStyle = .roundedBezel
            search.delegate = self
            search.sendsSearchStringImmediately = true
            search.sendsWholeSearchString = false
            search.frame = NSRect(x: 10, y: height - searchH - 2, width: width - 20, height: searchH - 4)
            card.addSubview(search)
        }

        scroll.frame = NSRect(x: 4, y: hintH, width: width - 8, height: listH)
        card.addSubview(scroll)

        let hint = NSTextField(labelWithString: "↑↓ pick    ⏎ fill    esc")
        hint.font = .systemFont(ofSize: 10, weight: .regular)
        hint.textColor = .tertiaryLabelColor
        hint.alignment = .center
        hint.frame = NSRect(x: 0, y: 3, width: width, height: 16)
        card.addSubview(hint)

        panel.contentView = card
        panel.setContentSize(NSSize(width: width, height: height))

        // Anchor under the focused field when one's on screen — the UI is
        // tied to the field like the browser extensions. Otherwise center
        // the card on the display that owns the frontmost app.
        var origin: NSPoint?
        if let field {
            var posRef: CFTypeRef?
            var sizeRef: CFTypeRef?
            AXUIElementCopyAttributeValue(field, kAXPositionAttribute as CFString, &posRef)
            AXUIElementCopyAttributeValue(field, kAXSizeAttribute as CFString, &sizeRef)
            var pos = CGPoint.zero
            var size = CGSize.zero
            if let v = posRef { AXValueGetValue(v as! AXValue, .cgPoint, &pos) }
            if let v = sizeRef { AXValueGetValue(v as! AXValue, .cgSize, &size) }
            // Skip window-sized elements — a full-window textArea is not a
            // credential field, and pinning under it just looks random.
            let credentialish = size.width > 0 && size.width < 900 && size.height < 120
            if pos != .zero, credentialish {
                // AX y is top-down in Quartz space; AppKit frame origins are
                // bottom-up against the primary display's height.
                let ph = NSScreen.screens.first?.frame.height ?? 0
                origin = NSPoint(
                    x: pos.x + 4, y: ph - (pos.y + size.height + 2) - height,
                )
            }
        }
        if origin == nil {
            let screen = NSScreen.screens.first {
                $0.visibleFrame.contains(NSEvent.mouseLocation)
            } ?? NSScreen.main ?? NSScreen.screens.first
            if let screen {
                origin = NSPoint(
                    x: screen.visibleFrame.midX - 170,
                    y: screen.visibleFrame.midY + height * 0.25,
                )
            }
        }
        if let origin { panel.setFrameOrigin(origin) }
    }

    var isVisible: Bool { panel.isVisible }

    func show() {
        table.reloadData()
        table.selectRowIndexes(IndexSet(integer: 0), byExtendingSelection: false)
        if mode == .palette {
            // makeKey takes the keyboard for the search field without
            // activating Veil — the target app stays front.
            panel.makeKeyAndOrderFront(nil)
            panel.makeFirstResponder(search)
        } else {
            // Anchored never takes key — arrows/Enter/Esc ride a session
            // tap while the panel's up so the field keeps the keyboard.
            panel.orderFront(nil)
            let mask = CGEventMask(1 << CGEventType.keyDown.rawValue)
            let me = Unmanaged.passUnretained(self).toOpaque()
            eventTap = CGEvent.tapCreate(
                tap: .cgSessionEventTap, place: .headInsertEventTap,
                options: .defaultTap, eventsOfInterest: mask,
                callback: { _, type, event, ctx -> Unmanaged<CGEvent>? in
                    if type == .tapDisabledByTimeout || type == .tapDisabledByUserInput {
                        if let ctx {
                            let p = Unmanaged<FillPanel>.fromOpaque(ctx).takeUnretainedValue()
                            if let tap = p.eventTap { CGEvent.tapEnable(tap: tap, enable: true) }
                        }
                        return Unmanaged.passUnretained(event)
                    }
                    guard type == .keyDown, let ctx else { return Unmanaged.passUnretained(event) }
                    let p = Unmanaged<FillPanel>.fromOpaque(ctx).takeUnretainedValue()
                    return p.handleKey(event) ? Unmanaged.passUnretained(event) : nil
                },
                userInfo: me,
            )
            if let eventTap {
                tapSource = CFMachPortCreateRunLoopSource(nil, eventTap, 0)
                CFRunLoopAddSource(CFRunLoopGetMain(), tapSource, .commonModes)
                CGEvent.tapEnable(tap: eventTap, enable: true)
            }
        }
        NSLog("veil: picker shown mode=%@ frame=%@", mode == .palette ? "palette" : "anchored",
              NSStringFromRect(panel.frame))

        globalMonitor = NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown]) {
            [weak self] _ in
            guard let self, self.panel.isVisible else { return }
            // Clicks inside the panel are row picks — only outside closes it.
            if !self.panel.frame.contains(NSEvent.mouseLocation) {
                self.dismiss()
            }
        }
    }

    /// Filter as the search text changes — name, login, and URI all match.
    func controlTextDidChange(_ obj: Notification) {
        let q = search.stringValue.trimmingCharacters(in: .whitespaces).lowercased()
        shown = q.isEmpty ? allEntries : allEntries.filter { e in
            let name = (e["name"] as? String ?? "").lowercased()
            let login = (e["login"] as? String ?? "").lowercased()
            let uris = (e["uris"] as? [String] ?? []).joined(separator: " ").lowercased()
            return name.contains(q) || login.contains(q) || uris.contains(q)
        }
        table.reloadData()
        if !shown.isEmpty {
            table.selectRowIndexes(IndexSet(integer: 0), byExtendingSelection: false)
        }
    }

    /// Anchored mode's nav keys — eaten only while the panel is visible.
    /// true = the event proceeds to the field, false = the picker took it.
    private func handleKey(_ ev: CGEvent) -> Bool {
        guard panel.isVisible else { return true }
        switch ev.getIntegerValueField(.keyboardEventKeycode) {
        case 125: move(1)               // down
        case 126: move(-1)              // up
        case 36, 76: confirm()          // return / keypad enter
        case 53: dismiss()              // esc
        default: return true
        }
        return false
    }

    /// Arrows/Enter/Esc typed into the search field forward to the table —
    /// everything else stays normal editing keys.
    func control(_ control: NSControl, textView: NSTextView, doCommandBy sel: Selector) -> Bool {
        switch sel {
        case #selector(NSResponder.moveDown(_:)): move(1); return true
        case #selector(NSResponder.moveUp(_:)): move(-1); return true
        case #selector(NSResponder.insertNewline(_:)): confirm(); return true
        case #selector(NSResponder.cancelOperation(_:)): dismiss(); return true
        default: return false
        }
    }

    private func move(_ delta: Int) {
        let next = max(0, min(shown.count - 1, table.selectedRow + delta))
        table.selectRowIndexes(IndexSet(integer: next), byExtendingSelection: false)
        table.scrollRowToVisible(next)
    }

    @objc private func rowClicked(_ sender: Any?) {
        confirm()
    }

    @objc private func confirm() {
        let row = table.selectedRow
        guard row >= 0, row < shown.count,
              let uuid = shown[row]["uuid"] as? String, !uuid.isEmpty
        else { return }
        let pick = onPick
        dismiss()
        pick(uuid)
    }

    func dismiss() {
        if let tapSource {
            CFRunLoopRemoveSource(CFRunLoopGetMain(), tapSource, .commonModes)
            self.tapSource = nil
        }
        if let eventTap {
            CGEvent.tapEnable(tap: eventTap, enable: false)
            CFMachPortInvalidate(eventTap)
            self.eventTap = nil
        }
        if let globalMonitor { NSEvent.removeMonitor(globalMonitor) }
        globalMonitor = nil
        panel.orderOut(nil)
    }

    deinit {
        dismiss()
    }

    func numberOfRows(in tableView: NSTableView) -> Int { shown.count }

    /// Rounded accent fill under the picked row — the default selection
    /// is a full-bleed rectangle, which reads as a bug on a floating card.
    private final class RoundRow: NSTableRowView {
        override func drawSelection(in dirtyRect: NSRect) {
            let r = bounds.insetBy(dx: 6, dy: 1.5)
            let path = NSBezierPath(roundedRect: r, xRadius: 8, yRadius: 8)
            NSColor.controlAccentColor.withAlphaComponent(0.18).setFill()
            path.fill()
            NSColor.controlAccentColor.withAlphaComponent(0.5).setStroke()
            path.lineWidth = 1
            path.stroke()
        }
    }

    func tableView(_ tableView: NSTableView, rowViewForRow row: Int) -> NSTableRowView? {
        RoundRow()
    }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let e = shown[row]
        let name = e["name"] as? String ?? "item"
        let login = e["login"] as? String ?? ""
        let id = NSUserInterfaceItemIdentifier("cell")
        let cell = tableView.makeView(withIdentifier: id, owner: nil) as? NSTableCellView ?? {
            let v = NSTableCellView()
            let icon = NSImageView()
            icon.translatesAutoresizingMaskIntoConstraints = false
            icon.symbolConfiguration = NSImage.SymbolConfiguration(pointSize: 13, weight: .medium)
            icon.contentTintColor = .secondaryLabelColor
            let title = NSTextField(labelWithString: "")
            title.translatesAutoresizingMaskIntoConstraints = false
            title.font = .systemFont(ofSize: 13, weight: .medium)
            title.lineBreakMode = .byTruncatingTail
            let sub = NSTextField(labelWithString: "")
            sub.translatesAutoresizingMaskIntoConstraints = false
            sub.font = .systemFont(ofSize: 11, weight: .regular)
            sub.textColor = .secondaryLabelColor
            sub.lineBreakMode = .byTruncatingTail
            sub.identifier = NSUserInterfaceItemIdentifier("sub")
            v.addSubview(icon)
            v.addSubview(title)
            v.addSubview(sub)
            v.imageView = icon
            v.textField = title
            NSLayoutConstraint.activate([
                icon.leadingAnchor.constraint(equalTo: v.leadingAnchor, constant: 16),
                icon.centerYAnchor.constraint(equalTo: v.centerYAnchor),
                icon.widthAnchor.constraint(equalToConstant: 16),
                title.leadingAnchor.constraint(equalTo: icon.trailingAnchor, constant: 10),
                title.bottomAnchor.constraint(equalTo: v.centerYAnchor, constant: -1),
                title.trailingAnchor.constraint(lessThanOrEqualTo: v.trailingAnchor, constant: -12),
                sub.leadingAnchor.constraint(equalTo: title.leadingAnchor),
                sub.topAnchor.constraint(equalTo: v.centerYAnchor, constant: 1),
                sub.trailingAnchor.constraint(lessThanOrEqualTo: v.trailingAnchor, constant: -12),
            ])
            v.identifier = id
            return v
        }()
        cell.imageView?.image = NSImage(
            systemSymbolName: "key.fill",
            accessibilityDescription: nil,
        )
        cell.textField?.stringValue = name
        if let sub = cell.subviews.first(where: { $0.identifier?.rawValue == "sub" }) as? NSTextField {
            sub.stringValue = login
        }
        return cell
    }
}
