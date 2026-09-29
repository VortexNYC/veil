//
//  FillBridge.swift
//  Veil
//
//  Same framed-JSON client the Safari appex uses, minus the sandbox — the
//  menu-bar helper reaches the daemon's vault-home socket directly.
//

import Foundation

final class FillBridge {
    static let shared = FillBridge()

    private var sock: FileHandle?
    private let lock = NSLock()

    private func socketPath() -> String {
        if let override = ProcessInfo.processInfo.environment["VEIL_FILL_SOCK"], !override.isEmpty {
            return override
        }
        // Sandboxed: the daemon binds fill.sock inside this app's own
        // container — same pattern as the Safari appex socket.
        return FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("fill.sock").path
    }

    private func connect() throws {
        if sock != nil { return }
        let path = socketPath()
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else {
            throw NSError(domain: "nyc.veil.fill", code: 1, userInfo: [
                NSLocalizedDescriptionKey: "socket(): \(String(cString: strerror(errno)))",
            ])
        }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        addr.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        let cap = MemoryLayout.size(ofValue: addr.sun_path)
        guard path.utf8.count < cap else {
            close(fd)
            throw NSError(domain: "nyc.veil.fill", code: 2, userInfo: [
                NSLocalizedDescriptionKey: "fill socket path too long",
            ])
        }
        _ = withUnsafeMutablePointer(to: &addr.sun_path) { ptr in
            ptr.withMemoryRebound(to: CChar.self, capacity: cap) { buf in
                strlcpy(buf, path, cap)
            }
        }
        let bound = withUnsafePointer(to: &addr) { ptr in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                Darwin.connect(fd, sa, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard bound == 0 else {
            close(fd)
            throw NSError(domain: "nyc.veil.fill", code: 3, userInfo: [
                NSLocalizedDescriptionKey: "veil fill host not running at \(path) — run `veil fill install`",
            ])
        }
        // A wedged host (or the Touch ID sheet left up) must not block the
        // caller past this bound — reads/writes fail, we drop + reconnect.
        var tv = timeval(tv_sec: 30, tv_usec: 0)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
        setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
        var one: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &one, socklen_t(MemoryLayout<Int32>.size))
        sock = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
    }

    private func drop() {
        try? sock?.close()
        sock = nil
    }

    private func send(_ message: [String: Any]) throws {
        guard let sock else { throw NSError(domain: "nyc.veil.fill", code: 4) }
        let body = try JSONSerialization.data(withJSONObject: message)
        var len = UInt32(body.count).littleEndian
        try sock.write(contentsOf: Data(bytes: &len, count: 4))
        try sock.write(contentsOf: body)
    }

    private func receive() throws -> [String: Any] {
        guard let sock else { throw NSError(domain: "nyc.veil.fill", code: 4) }
        guard let hdr = try sock.read(upToCount: 4), hdr.count == 4 else {
            throw NSError(domain: "nyc.veil.fill", code: 5, userInfo: [
                NSLocalizedDescriptionKey: "fill host closed the socket",
            ])
        }
        let n = hdr.withUnsafeBytes { $0.load(as: UInt32.self) }.littleEndian
        guard n > 0, n <= 16 * 1024 * 1024 else {
            throw NSError(domain: "nyc.veil.fill", code: 6, userInfo: [
                NSLocalizedDescriptionKey: "fill host sent a bad frame",
            ])
        }
        guard let payload = try sock.read(upToCount: Int(n)), payload.count == n else {
            throw NSError(domain: "nyc.veil.fill", code: 7, userInfo: [
                NSLocalizedDescriptionKey: "fill host reply truncated",
            ])
        }
        guard let reply = try JSONSerialization.jsonObject(with: payload) as? [String: Any] else {
            throw NSError(domain: "nyc.veil.fill", code: 8, userInfo: [
                NSLocalizedDescriptionKey: "fill host reply was not a JSON object",
            ])
        }
        return reply
    }

    /// One JSON object out, one reply back. A dead socket reconnects and
    /// resends once — but only when the write failed: a request the host
    /// already received (e.g. a fill sitting at Touch ID) is never retried,
    /// or a read timeout would double the confirmation prompt.
    func roundTrip(_ message: [String: Any]) throws -> [String: Any] {
        lock.lock()
        defer { lock.unlock() }
        try connect()
        do {
            try send(message)
        } catch {
            drop()
            try connect()
            try send(message)
        }
        return try receive()
    }

    /// Items bound to this app (`app://<bundleID>` URIs — exact match, same
    //  rule as web hosts).
    func match(app bundleID: String) throws -> [[String: Any]] {
        let reply = try roundTrip(["action": "match", "url": "app://" + bundleID, "app": bundleID])
        return reply["entries"] as? [[String: Any]] ?? []
    }

    /// Items matching a web URL — the AutoFill appex's service identifiers.
    func match(url: String) throws -> [[String: Any]] {
        let reply = try roundTrip(["action": "match", "url": url])
        return reply["entries"] as? [[String: Any]] ?? []
    }

    /// Every fillable item's metadata + URIs — the identity-store sync feed.
    func list() throws -> [[String: Any]] {
        let reply = try roundTrip(["action": "list"])
        return reply["entries"] as? [[String: Any]] ?? []
    }

    /// Confirm-gated: the host runs its own Touch ID step before answering.
    func fill(app bundleID: String, uuid: String) throws -> (entries: [[String: Any]], error: String) {
        let reply = try roundTrip(["action": "fill", "url": "app://" + bundleID, "app": bundleID, "uuid": uuid])
        let entries = reply["entries"] as? [[String: Any]] ?? []
        return (entries, reply["error"] as? String ?? "")
    }

    /// Same fill for a web service — the appex path.
    func fill(url: String, uuid: String) throws -> (entries: [[String: Any]], error: String) {
        let reply = try roundTrip(["action": "fill", "url": url, "uuid": uuid])
        let entries = reply["entries"] as? [[String: Any]] ?? []
        return (entries, reply["error"] as? String ?? "")
    }
}
