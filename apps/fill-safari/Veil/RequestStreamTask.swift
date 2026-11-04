//
//  RequestStreamTask.swift
//  Veil
//
//  Subscribes to GET /v1/requests/stream so Mac notifications are not
//  stuck on a 30s poll while dogfooding agent fill.

import Foundation

final class RequestStreamTask: NSObject, URLSessionDataDelegate {
    private let origin: String
    private let tokenPath: String
    private let onTick: () -> Void
    private var session: URLSession!
    private var task: URLSessionDataTask?
    private var buffer = Data()

    init(origin: String, tokenPath: String, onTick: @escaping () -> Void) {
        self.origin = origin
        self.tokenPath = tokenPath
        self.onTick = onTick
        super.init()
        session = URLSession(configuration: .default, delegate: self, delegateQueue: nil)
    }

    func start() {
        task?.cancel()
        buffer.removeAll()
        guard let tok = try? String(contentsOfFile: tokenPath)
            .trimmingCharacters(in: .whitespacesAndNewlines),
            !tok.isEmpty,
            let url = URL(string: origin + "/v1/requests/stream") else { return }
        var req = URLRequest(url: url)
        req.setValue("Bearer " + tok, forHTTPHeaderField: "Authorization")
        req.timeoutInterval = 0
        task = session.dataTask(with: req)
        task?.resume()
    }

    func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        buffer.append(data)
        while let range = buffer.range(of: Data("\n\n".utf8)) {
            let chunk = buffer.subdata(in: buffer.startIndex..<range.lowerBound)
            buffer.removeSubrange(buffer.startIndex..<range.upperBound)
            if let line = String(data: chunk, encoding: .utf8),
               line.contains("data:") {
                DispatchQueue.main.async { [onTick] in onTick() }
            }
        }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        DispatchQueue.global().asyncAfter(deadline: .now() + 3) { [weak self] in
            self?.start()
        }
    }
}
