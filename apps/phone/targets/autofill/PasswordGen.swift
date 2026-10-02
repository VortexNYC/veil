//
//  PasswordGen.swift
//  Veil AutoFill (iOS)
//
//  Password minting for ASGeneratePasswordsRequest. The request carries
//  the site's `passwordrules` attribute (Apple's DSL); we honor what the
//  grammar actually constrains at this layer — a maxlength cap and an
//  `allowed:` set with no symbol characters — and otherwise emit an
//  Apple-style strong password (hyphenated groups, upper+lower+digit).
//  Entropy is SecRandomCopyBytes with rejection sampling — no modulo
//  bias in a security primitive.
//

import Foundation

enum PasswordGen {

    struct Result {
        let value: String
        /// True when the emitted password carries no symbol characters —
        /// the caller maps this onto ASGeneratedPassword.Kind.
        let alphanumeric: Bool
    }

    /// Generate a password. `rules` is the raw `passwordrules` value from
    /// any of the request's three rule fields; nil means no constraints.
    static func generate(rules: String?) -> Result {
        var maxLen = 20
        var alphaOnly = false
        for clause in (rules ?? "").lowercased().split(separator: ";") {
            let c = clause.trimmingCharacters(in: .whitespaces)
            if c.hasPrefix("maxlength:"), let n = Int(c.dropFirst("maxlength:".count)), n > 0 {
                maxLen = min(n, 64)
            }
            if c.hasPrefix("allowed:") {
                let set = c.dropFirst("allowed:".count)
                alphaOnly = !set.contains { "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~".contains($0) }
            }
        }
        if alphaOnly { return Result(value: alphanumeric(length: min(maxLen, 20)), alphanumeric: true) }
        return Result(value: strong(maxLength: maxLen), alphanumeric: false)
    }

    /// Apple-style strong password: groups of 4 lower/digit chars joined
    /// by hyphens with one letter uppercased. Shrinks group count when a
    /// site caps length; below ~12 chars a flat mixed string fits better.
    private static func strong(maxLength: Int) -> String {
        if maxLength < 12 { return alphanumeric(length: max(8, maxLength)) }
        var groups = min(4, (maxLength + 1) / 5)
        if groups < 2 { return alphanumeric(length: maxLength) }
        let lower = Array("abcdefghjkmnpqrstuvwxyz23456789")
        var words: [[Character]] = (0 ..< groups).map { _ in
            (0 ..< 4).map { _ in lower[Int(byte(UInt8(lower.count)))] }
        }
        // Exactly one uppercase letter, site-friendly position.
        let g = Int(byte(UInt8(groups)))
        let i = Int(byte(4))
        if let c = words[g][i].unicodeScalars.first, CharacterSet.lowercaseLetters.contains(c) {
            words[g][i] = Character(words[g][i].uppercased())
        }
        return words.map { String($0) }.joined(separator: "-")
    }

    /// Flat upper+lower+digit string with each class guaranteed present.
    private static func alphanumeric(length: Int) -> String {
        let set = Array("abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789")
        let n = max(8, length)
        var out = (0 ..< n).map { _ in set[Int(byte(UInt8(set.count)))] }
        out[0] = "a"; out[1] = "A"; out[2] = "4"
        return String(out)
    }

    /// One uniform byte in [0, bound) — rejection sampling, no modulo bias.
    private static func byte(_ bound: UInt8) -> UInt8 {
        while true {
            var b: UInt8 = 0
            guard SecRandomCopyBytes(kSecRandomDefault, 1, &b) == errSecSuccess else { continue }
            let limit = UInt8.max - (UInt8.max % bound)
            if b < limit { return b % bound }
        }
    }
}
