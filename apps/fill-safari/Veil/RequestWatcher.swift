//
//  RequestWatcher.swift
//  Veil
//
//  The approve-on-another-device reach: polls the origin's requests
//  surface and raises a macOS notification for each new open ask, so a
//  fill filed by another device surfaces here instead of needing the
//  vault open. Clicking the banner opens the vault where the approve
//  lives. Human-token HTTP only — the agent path never sees this.

import Cocoa
import UserNotifications

final class RequestWatcher {
    static let shared = RequestWatcher()
    private var timer: Timer?
    private var seen: Set<String> = []

    private var origin: String {
        guard let d = try? Data(contentsOf: URL(fileURLWithPath: FillSettingsModel.path)),
              let o = try? JSONSerialization.jsonObject(with: d) as? [String: Any],
              let u = o["origin"] as? String else { return "https://veil.nyc" }
        return u
    }

    private var tokenPath: String {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/vortex/pwm-human.jwt").path
    }

    func start() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in }
        timer = Timer.scheduledTimer(withTimeInterval: 30, repeats: true) { [weak self] _ in
            self?.poll()
        }
        timer?.fire()
    }

    private func poll() {
        guard let tok = try? String(contentsOfFile: tokenPath)
            .trimmingCharacters(in: .whitespacesAndNewlines), !tok.isEmpty else { return }
        var req = URLRequest(url: URL(string: origin + "/v1/requests")!)
        req.setValue("Bearer " + tok, forHTTPHeaderField: "Authorization")
        req.timeoutInterval = 10
        URLSession.shared.dataTask(with: req) { [weak self] data, resp, _ in
            guard let self, let data,
                  (resp as? HTTPURLResponse)?.statusCode == 200,
                  let out = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let reqs = out["requests"] as? [[String: Any]] else { return }
            var fresh: [[String: Any]] = []
            var ids = Set<String>()
            for r in reqs where (r["status"] as? String) == "open" {
                let id = r["id"] as? String ?? ""
                ids.insert(id)
                if !self.seen.contains(id) { fresh.append(r) }
            }
            DispatchQueue.main.async {
                self.seen = ids
                for r in fresh { self.notify(r) }
            }
        }.resume()
    }

    private func notify(_ r: [String: Any]) {
        let device = r["agent_id"] as? String ?? "a device"
        let item = r["item_id"] as? String ?? "an item"
        let c = UNMutableNotificationContent()
        c.title = "Veil wants to fill"
        c.body = "\(device) is asking to fill \(item) — open Veil to approve."
        c.sound = .default
        let req = UNNotificationRequest(identifier: r["id"] as? String ?? UUID().uuidString,
                                        content: c, trigger: nil)
        UNUserNotificationCenter.current().add(req)
    }
}
