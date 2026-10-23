//
//  BridgeDaemon.swift
//  Veil
//
//  The Go broker bundled at Contents/MacOS/veil-bin — the app launches it
//  on startup when the fill socket is not already served, so the install
//  is one app and nothing has to be started by hand.
//

import Foundation

final class BridgeDaemon {
    static let shared = BridgeDaemon()
    private var proc: Process?

    private var socketPath: String {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".veil/fill.sock").path
    }

    /// A live socket means a daemon is already serving — usually one the
    /// user started by hand. Only spawn when nothing is there. The file
    /// lingers after a daemon dies, so connect-probe, not exists-check.
    private func alive() -> Bool {
        FileManager.default.fileExists(atPath: socketPath)
            && (try? FillBridge.shared.roundTrip(["action": "ping"], timeout: 2)) != nil
    }

    func ensure() {
        syncBundledHostAndExtensions()
        for _ in 0..<10 {
            if alive() {
                return
            }
            usleep(100_000)
        }
        // Stale socket from a dead daemon — the file masks respawn.
        try? FileManager.default.removeItem(atPath: socketPath)
        guard let bin = Bundle.main.url(forAuxiliaryExecutable: "veil-bin") else {
            NSLog("veil: no bundled veil-bin — bridge cannot start")
            return
        }
        let p = Process()
        p.executableURL = bin
        p.arguments = ["fill", "--bridge"]
        var env = ProcessInfo.processInfo.environment
        env["VEIL_HOME"] = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".veil").path
        env["VEIL_REPLICA_KEYSTORE"] = "mem"
        p.environment = env
        p.standardOutput = FileHandle.nullDevice
        p.standardError = FileHandle.nullDevice
        do {
            try p.run()
            proc = p
            NSLog("veil: bridge spawned pid=%d", p.processIdentifier)
        } catch {
            NSLog("veil: bridge spawn failed %@", error.localizedDescription)
        }
    }

    /// When Veil.app updates (via Sparkle or DMG install), sync the bundled
    /// `veil-bin` to `~/.veil/native-host` and refresh unpacked Chrome/Firefox
    /// extension directories if `~/.veil/fill.json` exists.
    private func syncBundledHostAndExtensions() {
        guard let bin = Bundle.main.url(forAuxiliaryExecutable: "veil-bin") else {
            return
        }
        let veilDir = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".veil")
        let fillJson = veilDir.appendingPathComponent("fill.json").path
        guard FileManager.default.fileExists(atPath: fillJson) else {
            return
        }
        let dst = veilDir.appendingPathComponent("native-host")
        if let srcAttr = try? FileManager.default.attributesOfItem(atPath: bin.path),
           let srcDate = srcAttr[.modificationDate] as? Date {
            let dstAttr = try? FileManager.default.attributesOfItem(atPath: dst.path)
            let dstDate = dstAttr?[.modificationDate] as? Date
            if dstDate == nil || srcDate != dstDate {
                let tmp = veilDir.appendingPathComponent("native-host.new")
                try? FileManager.default.removeItem(at: tmp)
                if (try? FileManager.default.copyItem(at: bin, to: tmp)) != nil {
                    try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: tmp.path)
                    _ = try? FileManager.default.replaceItemAt(dst, withItemAt: tmp)
                    // Sync unpacked Chrome/Firefox extensions and restart the
                    // launchd bridge so the new binary serves fill.sock immediately.
                    let syncProc = Process()
                    syncProc.executableURL = bin
                    syncProc.arguments = ["update", "--sync-only"]
                    try? syncProc.run()
                    syncProc.waitUntilExit()
                    let killOld = Process()
                    killOld.executableURL = URL(fileURLWithPath: "/usr/bin/killall")
                    killOld.arguments = ["veil-bin", "native-host"]
                    try? killOld.run()
                    killOld.waitUntilExit()
                    let kick = Process()
                    kick.executableURL = URL(fileURLWithPath: "/bin/launchctl")
                    kick.arguments = ["kickstart", "-k", "gui/\(getuid())/nyc.veil.fill.bridge"]
                    try? kick.run()
                    kick.waitUntilExit()
                }
            }
        }
    }
}
