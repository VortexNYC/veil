//
//  SafariWebExtensionHandler.swift
//  Veil Extension
//
//  Bridges Safari's per-message native messaging to the Veil fill host.
//  The host is the same Go process Chrome/Firefox native messaging spawns:
//  vault-home/native-host, configured by the fill.json beside it. Frames are
//  uint32 little-endian length + JSON, identical on every browser.
//

import SafariServices
import os.log

/// A live `native-host` process: one stdin pipe for requests, one stdout
/// pipe for framed replies. The JSON protocol is strictly request/response,
/// so a single lock serializes callers.
private final class HostPipe {
    static let shared = HostPipe()

    private var process: Process?
    private var toHost: FileHandle?
    private var fromHost: FileHandle?
    private let lock = NSLock()

    /// Resolution order matches how the host is installed: a copy embedded in
    /// this extension's bundle (distribution builds), then the path
    /// `veil fill install` wrote — vault-home/native-host, defaulting to
    /// ~/.veil/native-host.
    private func hostURL() -> URL? {
        if let bundled = Bundle.main.url(forResource: "native-host", withExtension: nil) {
            return bundled
        }
        if let override = ProcessInfo.processInfo.environment["VEIL_FILL_HOST"], !override.isEmpty {
            return URL(fileURLWithPath: override)
        }
        let home = FileManager.default.homeDirectoryForCurrentUser
        let installed = home.appendingPathComponent(".veil/native-host")
        if FileManager.default.isExecutableFile(atPath: installed.path) {
            return installed
        }
        return nil
    }

    /// Starts the host if needed. argv0's basename ("native-host") is what the
    /// binary keys on to select fill mode; no arguments are passed.
    private func ensureRunning() throws {
        if let p = process, p.isRunning { return }
        guard let url = hostURL() else {
            throw NSError(domain: "nyc.veil.fill", code: 1, userInfo: [
                NSLocalizedDescriptionKey: "veil fill host not installed — run `veil fill install`",
            ])
        }
        let p = Process()
        let stdin = Pipe()
        let stdout = Pipe()
        p.executableURL = url
        p.standardInput = stdin
        p.standardOutput = stdout
        // stderr stays on the unified log via the launch environment.
        try p.run()
        process = p
        toHost = stdin.fileHandleForWriting
        fromHost = stdout.fileHandleForReading
    }

    private func kill() {
        process?.terminate()
        process = nil
        toHost = nil
        fromHost = nil
    }

    /// Sends one JSON object, waits for one framed reply. A broken pipe
    /// respawns the host once before surfacing the error.
    func roundTrip(_ message: [String: Any]) throws -> [String: Any] {
        lock.lock()
        defer { lock.unlock() }
        do {
            return try exchange(message)
        } catch {
            kill()
            try ensureRunning()
            return try exchange(message)
        }
    }

    private func exchange(_ message: [String: Any]) throws -> [String: Any] {
        try ensureRunning()
        guard let toHost, let fromHost else {
            throw NSError(domain: "nyc.veil.fill", code: 2)
        }
        let body = try JSONSerialization.data(withJSONObject: message)
        var len = UInt32(body.count).littleEndian
        toHost.write(Data(bytes: &len, count: 4))
        toHost.write(body)
        guard let hdr = try fromHost.read(upToCount: 4), hdr.count == 4 else {
            throw NSError(domain: "nyc.veil.fill", code: 3, userInfo: [
                NSLocalizedDescriptionKey: "fill host closed the pipe",
            ])
        }
        let n = hdr.withUnsafeBytes { $0.load(as: UInt32.self) }.littleEndian
        guard n > 0, n <= 16 * 1024 * 1024 else {
            throw NSError(domain: "nyc.veil.fill", code: 4, userInfo: [
                NSLocalizedDescriptionKey: "fill host sent a bad frame",
            ])
        }
        guard let payload = try fromHost.read(upToCount: Int(n)), payload.count == n else {
            throw NSError(domain: "nyc.veil.fill", code: 5, userInfo: [
                NSLocalizedDescriptionKey: "fill host reply truncated",
            ])
        }
        guard let reply = try JSONSerialization.jsonObject(with: payload) as? [String: Any] else {
            throw NSError(domain: "nyc.veil.fill", code: 6, userInfo: [
                NSLocalizedDescriptionKey: "fill host reply was not a JSON object",
            ])
        }
        return reply
    }
}

class SafariWebExtensionHandler: NSObject, NSExtensionRequestHandling {

    func beginRequest(with context: NSExtensionContext) {
        let request = context.inputItems.first as? NSExtensionItem

        let message: Any?
        if #available(iOS 15.0, macOS 11.0, *) {
            message = request?.userInfo?[SFExtensionMessageKey]
        } else {
            message = request?.userInfo?["message"]
        }

        let response = NSExtensionItem()
        defer { context.completeRequest(returningItems: [response], completionHandler: nil) }

        guard let dict = message as? [String: Any] else {
            response.userInfo = [SFExtensionMessageKey: ["success": "false", "error": "bad message"]]
            return
        }

        do {
            let reply = try HostPipe.shared.roundTrip(dict)
            response.userInfo = [SFExtensionMessageKey: reply]
        } catch {
            os_log(.error, "veil fill host: %{public}@", error.localizedDescription)
            response.userInfo = [SFExtensionMessageKey: ["success": "false", "error": error.localizedDescription]]
        }
    }

}
