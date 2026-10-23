//
//  SafariWebExtensionHandler.swift
//  Veil Extension
//
//  Bridges Safari's per-message native messaging to the Veil fill host.
//  Appexes are always sandboxed and cannot spawn the host, so `veil fill
//  install` runs vault-home/native-host --bridge as a LaunchAgent; this
//  extension reaches it over a unix socket the daemon binds inside this
//  appex's own container. Frames are uint32 little-endian length + JSON,
//  identical on every browser. Each request uses its own short-lived
//  socket on a background queue so a Touch ID prompt in one tab never
//  blocks `match` or `ping` in another tab.
//

import SafariServices
import os.log

private enum HostSocket {
    private static func socketPath() -> String {
        if let override = ProcessInfo.processInfo.environment["VEIL_FILL_SOCK"], !override.isEmpty {
            return override
        }
        return FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("fill.sock").path
    }

    static func roundTrip(_ message: [String: Any], timeout: Int = 95) throws -> [String: Any] {
        let path = socketPath()
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else {
            throw NSError(domain: "nyc.veil.fill", code: 2, userInfo: [
                NSLocalizedDescriptionKey: "socket(): \(String(cString: strerror(errno)))",
            ])
        }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        addr.sun_len = UInt8(MemoryLayout<sockaddr_un>.size)
        let cap = MemoryLayout.size(ofValue: addr.sun_path)
        guard path.utf8.count < cap else {
            close(fd)
            throw NSError(domain: "nyc.veil.fill", code: 3, userInfo: [
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
            let err = String(cString: strerror(errno))
            close(fd)
            throw NSError(domain: "nyc.veil.fill", code: 4, userInfo: [
                NSLocalizedDescriptionKey: "veil fill host not running at \(path) — run `veil fill install` (\(err))",
            ])
        }
        var tv = timeval(tv_sec: timeout, tv_usec: 0)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
        setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
        var one: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &one, socklen_t(MemoryLayout<Int32>.size))
        let sock = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
        defer { try? sock.close() }

        let body = try JSONSerialization.data(withJSONObject: message)
        var len = UInt32(body.count).littleEndian
        try sock.write(contentsOf: Data(bytes: &len, count: 4))
        try sock.write(contentsOf: body)
        guard let hdr = try sock.read(upToCount: 4), hdr.count == 4 else {
            throw NSError(domain: "nyc.veil.fill", code: 6, userInfo: [
                NSLocalizedDescriptionKey: "fill host closed the socket",
            ])
        }
        let n = hdr.withUnsafeBytes { $0.load(as: UInt32.self) }.littleEndian
        guard n > 0, n <= 16 * 1024 * 1024 else {
            throw NSError(domain: "nyc.veil.fill", code: 7, userInfo: [
                NSLocalizedDescriptionKey: "fill host sent a bad frame",
            ])
        }
        var payload = Data()
        payload.reserveCapacity(Int(n))
        while payload.count < Int(n) {
            guard let chunk = try sock.read(upToCount: Int(n) - payload.count), !chunk.isEmpty else {
                throw NSError(domain: "nyc.veil.fill", code: 8, userInfo: [
                    NSLocalizedDescriptionKey: "fill host reply truncated",
                ])
            }
            payload.append(chunk)
        }
        guard let reply = try JSONSerialization.jsonObject(with: payload) as? [String: Any] else {
            throw NSError(domain: "nyc.veil.fill", code: 9, userInfo: [
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

        guard let dict = message as? [String: Any] else {
            let response = NSExtensionItem()
            response.userInfo = [SFExtensionMessageKey: ["success": "false", "error": "bad message"]]
            context.completeRequest(returningItems: [response], completionHandler: nil)
            return
        }

        DispatchQueue.global(qos: .userInitiated).async {
            let response = NSExtensionItem()
            do {
                let reply = try HostSocket.roundTrip(dict)
                os_log(.default, "veil fill reply ok")
                response.userInfo = [SFExtensionMessageKey: reply]
            } catch {
                os_log(.error, "veil fill host: %{public}@", error.localizedDescription)
                response.userInfo = [SFExtensionMessageKey: ["success": "false", "error": error.localizedDescription]]
            }
            context.completeRequest(returningItems: [response], completionHandler: nil)
        }
    }

}
