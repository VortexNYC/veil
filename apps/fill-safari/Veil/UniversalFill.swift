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
                guard type == .keyDown,
                      event.getIntegerValueField(.keyboardEventKeycode) == 42,
                      event.flags.contains(.maskCommand),
                      event.flags.intersection([.maskShift, .maskControl, .maskAlternate]).isEmpty
                else { return Unmanaged.passUnretained(event) }
                DispatchQueue.main.async { UniversalFill.shared.trigger() }
                return nil // Cmd-\ is ours — the field never sees it
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
                let panel = FillPanel(entries: entries, at: field) { [weak self] uuid in
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
        // The app regains focus before typing — the picker menu borrowed it.
        front.activate()
        DispatchQueue.global().async {
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
    private let panel: NSPanel
    private let table = NSTableView()
    private let allEntries: [[String: Any]]
    private var shown: [[String: Any]]
    private let search = NSSearchField()
    private let onPick: (String) -> Void
    private var globalMonitor: Any?

    init(entries: [[String: Any]], at field: AXUIElement?, onPick: @escaping (String) -> Void) {
        self.allEntries = entries
        self.shown = entries
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
        // Keyable without activating — the search field takes real typing
        // while the target app stays front.
        panel.becomesKeyOnlyIfNeeded = false
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

        // Vibrancy card: search up top, rounded rows, a thin key-hint
        // footer — the Quick Access shape.
        let searchH: CGFloat = 34
        let listH = min(CGFloat(entries.count) * 46 + 8, 276)
        let hintH: CGFloat = 24
        let height = searchH + listH + hintH
        let card = NSVisualEffectView(frame: NSRect(x: 0, y: 0, width: 340, height: height))
        card.material = .popover
        card.blendingMode = .behindWindow
        card.state = .active
        card.wantsLayer = true
        card.layer?.cornerRadius = 12
        card.layer?.masksToBounds = true
        card.layer?.borderWidth = 1
        card.layer?.borderColor = NSColor.separatorColor.cgColor

        search.placeholderString = "Search items"
        search.font = .systemFont(ofSize: 14)
        search.focusRingType = .none
        search.isBezeled = true
        search.bezelStyle = .roundedBezel
        search.delegate = self
        search.sendsSearchStringImmediately = true
        search.sendsWholeSearchString = false
        search.frame = NSRect(x: 10, y: height - searchH - 2, width: 320, height: searchH - 4)
        card.addSubview(search)

        scroll.frame = NSRect(x: 4, y: hintH, width: 332, height: listH)
        card.addSubview(scroll)

        let hint = NSTextField(labelWithString: "↑↓ pick    ⏎ fill    esc")
        hint.font = .systemFont(ofSize: 10, weight: .regular)
        hint.textColor = .tertiaryLabelColor
        hint.alignment = .center
        hint.frame = NSRect(x: 0, y: 3, width: 340, height: 16)
        card.addSubview(hint)

        panel.contentView = card
        panel.setContentSize(NSSize(width: 340, height: height))

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
        // makeKey takes the keyboard for the search field without
        // activating Veil — the target app stays front.
        panel.makeKeyAndOrderFront(nil)
        panel.makeFirstResponder(search)
        NSLog("veil: picker shown frame=%@", NSStringFromRect(panel.frame))

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
