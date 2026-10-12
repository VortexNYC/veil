//
//  ErrorReport.swift — $exception capture to PostHog for the Mac app.
//  Same shape as the extension/phone reporters: one POST to the public
//  capture host, no SDK, no Keychain access, no item data. distinct_id
//  is a stable anonymous install id in UserDefaults. Uncaught Obj-C
//  exceptions and explicit report() calls land in error tracking.
//

import Foundation

enum ErrorReport {
    private static let host = "https://us.i.posthog.com/capture/"
    private static let key = "phc_v5HaYTE3N9siA53wtYmkFHSm28mBATJJsdDjeaobHxC8"
    private static let installId: String = {
        if let id = UserDefaults.standard.string(forKey: "veilInstallId") {
            return id
        }
        let id = "mac-" + UUID().uuidString
        UserDefaults.standard.set(id, forKey: "veilInstallId")
        return id
    }()

    // Module-level: the handler C-interop forbids a capturing closure.
    private static let uncaughtExceptionHandler: @convention(c) (NSException) -> Void = { exception in
        report(
            type: "objc_exception",
            message: exception.reason ?? exception.name.rawValue,
            stack: exception.callStackSymbols.prefix(12).joined(separator: "\n")
        )
    }

    static func arm() {
        _ = installId // ensure id exists before first report
        NSSetUncaughtExceptionHandler(uncaughtExceptionHandler)
    }

    static func report(type: String = "error", message: String, stack: String = "", extra: [String: String] = [:]) {
        var props: [String: Any] = [
            "distinct_id": installId,
            "$lib": "veil-mac",
            "$exception_type": type,
            "$exception_message": message,
            "surface": "mac",
        ]
        if !stack.isEmpty { props["$exception_stack_trace_raw"] = String(stack.prefix(1200)) }
        extra.forEach { props[$0.key] = $0.value }
        guard let body = try? JSONSerialization.data(withJSONObject: [
            "api_key": key,
            "event": "$exception",
            "properties": props,
        ]) else { return }
        var req = URLRequest(url: URL(string: host)!)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = body
        req.timeoutInterval = 3
        URLSession.shared.dataTask(with: req) { _, _, _ in }.resume()
    }
}
