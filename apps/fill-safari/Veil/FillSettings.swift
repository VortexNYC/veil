//
//  FillSettings.swift
//  Veil
//
//  The posture surface: one pane for the same fill.json the Go host and
//  veil CLI read — policy preset, remote approval, icon egress, and which
//  global chords the event tap listens for. Writes go straight to
//  ~/.veil/fill.json; the daemon picks them up next launch, the tap reads
//  chords live.

import Cocoa
import Combine
import SwiftUI

/// The user's fill.json — a plain dictionary round-trip. Only keys we own
/// get touched; everything else passes through.
final class FillSettingsModel: ObservableObject {
    @Published var policy = "standard"
    @Published var remoteApprove = "off"
    @Published var icons = true
    @Published var chordBackslash = true
    @Published var chordCmdShiftSpace = true
    @Published var chordCtrlOptSpace = true

    private var raw: [String: Any] = [:]

    static var path: String {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".veil/fill.json").path
    }

    func load() {
        guard let data = FileManager.default.contents(atPath: Self.path),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
        raw = obj
        policy = obj["policy"] as? String ?? "standard"
        remoteApprove = obj["remote_approve"] as? String ?? "off"
        icons = (obj["icons"] as? Bool) ?? true
        let chords = obj["chords"] as? [String] ?? ["cmd+\\", "cmd+shift+space", "ctrl+opt+space"]
        chordBackslash = chords.contains("cmd+\\")
        chordCmdShiftSpace = chords.contains("cmd+shift+space")
        chordCtrlOptSpace = chords.contains("ctrl+opt+space")
    }

    func save() {
        raw["policy"] = policy
        raw["remote_approve"] = remoteApprove
        raw["icons"] = icons
        var chords: [String] = []
        if chordBackslash { chords.append("cmd+\\") }
        if chordCmdShiftSpace { chords.append("cmd+shift+space") }
        if chordCtrlOptSpace { chords.append("ctrl+opt+space") }
        raw["chords"] = chords
        guard let data = try? JSONSerialization.data(withJSONObject: raw, options: [.prettyPrinted]) else { return }
        let dir = (Self.path as NSString).deletingLastPathComponent
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        try? data.write(to: URL(fileURLWithPath: Self.path))
        NotificationCenter.default.post(name: .veilSettingsChanged, object: nil)
    }
}

extension Notification.Name {
    static let veilSettingsChanged = Notification.Name("veilSettingsChanged")
}

struct FillSettingsView: View {
    @ObservedObject var model = FillSettingsModel()

    var body: some View {
        Form {
            Picker("Posture", selection: $model.policy) {
                Text("Standard").tag("standard")
                Text("Strict — confirm every release").tag("strict")
                Text("Relaxed — one unlock, 5 minutes").tag("relaxed")
            }
            Picker("Approve on another device", selection: $model.remoteApprove) {
                Text("Off").tag("off")
                Text("Tagged items only").tag("tagged")
                Text("Every fill").tag("all")
            }
            Toggle("Fetch site icons", isOn: $model.icons)
            Divider()
            Text("Global shortcuts").font(.headline)
            Toggle("⌘\\", isOn: $model.chordBackslash)
            Toggle("⌘⇧Space", isOn: $model.chordCmdShiftSpace)
            Toggle("⌃⌥Space (Veil)", isOn: $model.chordCtrlOptSpace)
        }
        .padding(20)
        .frame(width: 340)
        .onAppear { model.load() }
        .onChange(of: model.policy) { _ in model.save() }
        .onChange(of: model.remoteApprove) { _ in model.save() }
        .onChange(of: model.icons) { _ in model.save() }
        .onChange(of: model.chordBackslash) { _ in model.save() }
        .onChange(of: model.chordCmdShiftSpace) { _ in model.save() }
        .onChange(of: model.chordCtrlOptSpace) { _ in model.save() }
    }
}

final class SettingsWindowController: NSObject {
    static let shared = SettingsWindowController()
    private var window: NSWindow?

    func open() {
        if window == nil {
            let host = NSHostingController(rootView: FillSettingsView())
            let w = NSWindow(contentViewController: host)
            w.title = "Veil Settings"
            w.styleMask = [.titled, .closable]
            w.center()
            window = w
        }
        window?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }
}
