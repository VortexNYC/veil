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
    private let lock = NSLock()

    private var socketPath: String {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".veil/fill.sock").path
    }

    /// A live socket means a daemon is already serving — usually one the
    /// user started by hand. Only spawn when nothing is there. The file
    /// lingers after a daemon dies, so connect-probe, not exists-check.
    /// The probe is a bare connect — not a roundTrip on FillBridge.shared,
    /// whose ping runs the host's PullReplica + ensureIndex and can block
    /// seconds behind a human-JWT refresh.
    private func alive() -> Bool {
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { return false }
        defer { close(fd) }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        addr.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        let cap = MemoryLayout.size(ofValue: addr.sun_path)
        guard socketPath.utf8.count < cap else { return false }
        _ = withUnsafeMutablePointer(to: &addr.sun_path) { ptr in
            ptr.withMemoryRebound(to: CChar.self, capacity: cap) { buf in
                strlcpy(buf, socketPath, cap)
            }
        }
        return withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                Darwin.connect(fd, sa, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        } == 0
    }

    func ensure() {
        // ensure() is on the vault unlock path and app start — serialize so
        // concurrent callers wait for the first spawn, not race a second.
        lock.lock()
        defer { lock.unlock() }
        syncBundledHostAndExtensions()
        for _ in 0..<10 {
            if alive() {
                return
            }
            usleep(100_000)
        }
        // ServeBridge unlinks fill.sock itself after taking fill.lock —
        // never remove it here or a live daemon loses its socket.
        // The launchd job is the preferred supervisor; kick it first.
        let plist = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/LaunchAgents/nyc.veil.fill.bridge.plist").path
        if FileManager.default.fileExists(atPath: plist) {
            // A spawn queued seconds ago (make macos-app, `veil fill
            // install`, a KeepAlive respawn) can take a while to exec and
            // bind the socket. `kickstart -k` on a spawn younger than
            // launchd's 10s minimum runtime just re-arms the throttle
            // ("spawn scheduled"), so first nudge without -k — a no-op on
            // a live job — and poll for the whole window.
            let nudge = Process()
            nudge.executableURL = URL(fileURLWithPath: "/bin/launchctl")
            nudge.arguments = ["kickstart", "gui/\(getuid())/nyc.veil.fill.bridge"]
            try? nudge.run()
            nudge.waitUntilExit()
            for _ in 0..<100 {
                if alive() {
                    return
                }
                usleep(100_000)
            }
            // 10s with no socket: wedged or crash-looping. Any live spawn
            // is now past minimum runtime, so a hard restart is safe.
            let kick = Process()
            kick.executableURL = URL(fileURLWithPath: "/bin/launchctl")
            kick.arguments = ["kickstart", "-k", "gui/\(getuid())/nyc.veil.fill.bridge"]
            try? kick.run()
            kick.waitUntilExit()
            for _ in 0..<50 {
                if alive() {
                    return
                }
                usleep(100_000)
            }
        }
        guard let bin = Bundle.main.url(forAuxiliaryExecutable: "veil-bin") else {
            NSLog("veil: no bundled veil-bin — bridge cannot start")
            return
        }
        let p = Process()
        p.executableURL = bin
        p.arguments = ["fill", "--bridge"]
        var env = ProcessInfo.processInfo.environment
        let veilHome = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".veil").path
        env["VEIL_HOME"] = veilHome
        env["VEIL_REPLICA_KEYSTORE"] = "mem"
        p.environment = env
        // Bridge stderr carries remint/spawn failures — keep it on disk.
        try? FileManager.default.createDirectory(atPath: veilHome, withIntermediateDirectories: true)
        let logPath = veilHome + "/fill-bridge.log"
        // createFile truncates an existing log — only create when missing.
        if !FileManager.default.fileExists(atPath: logPath) {
            FileManager.default.createFile(atPath: logPath, contents: nil)
        }
        let out = FileHandle(forWritingAtPath: logPath) ?? FileHandle.nullDevice
        _ = try? out.seekToEnd()
        p.standardOutput = out
        p.standardError = out
        do {
            try p.run()
            proc = p
            // The child owns the log fd now — close the parent's copy.
            if out !== FileHandle.nullDevice { try? out.close() }
            NSLog("veil: bridge spawned pid=%d", p.processIdentifier)
        } catch {
            NSLog("veil: bridge spawn failed %@", error.localizedDescription)
        }
        for _ in 0..<10 {
            if alive() {
                return
            }
            usleep(100_000)
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
                    // Keep ~/.local/bin/veil (CLI + veil mcp stdio) in lockstep
                    // with Veil.app and ~/.veil/native-host.
                    let localBin = FileManager.default.homeDirectoryForCurrentUser
                        .appendingPathComponent(".local/bin/veil")
                    if FileManager.default.fileExists(atPath: localBin.path) {
                        let localTmp = FileManager.default.homeDirectoryForCurrentUser
                            .appendingPathComponent(".local/bin/veil.new")
                        try? FileManager.default.removeItem(at: localTmp)
                        if (try? FileManager.default.copyItem(at: bin, to: localTmp)) != nil {
                            try? FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: localTmp.path)
                            _ = try? FileManager.default.replaceItemAt(localBin, withItemAt: localTmp)
                        }
                    }
                    // Sync unpacked Chrome/Firefox extensions and restart the
                    // launchd bridge so the new binary serves fill.sock immediately.
                    let syncProc = Process()
                    syncProc.executableURL = bin
                    syncProc.arguments = ["update", "--sync-only"]
                    try? syncProc.run()
                    syncProc.waitUntilExit()
                    let killOld = Process()
                    killOld.executableURL = URL(fileURLWithPath: "/usr/bin/killall")
                    killOld.arguments = ["native-host"]
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
