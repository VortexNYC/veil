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
        if alive() {
            return
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
}
