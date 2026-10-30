import Foundation
import Security

/// A canonical 256-bit instance credential. Never include this value in diagnostics.
public struct InstanceKey: Equatable, Sendable, BearerTokenProviding {
    private let value: String
    public init(_ value: String) throws {
        guard value.utf8.count == 43,
              value.utf8.allSatisfy({ (65...90).contains($0) || (97...122).contains($0) || (48...57).contains($0) || $0 == 45 || $0 == 95 }),
              let bytes = Data(base64Encoded: value.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/") + "="),
              bytes.count == 32,
              bytes.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "") == value else {
            throw ConnectionInputError.invalidKey
        }
        self.value = value
    }
    public func bearerToken() throws -> String { value }
}

public enum ConnectionInputError: Error, LocalizedError {
    case invalidKey, invalidLink, missingQRCode, ambiguousQRCode
    public var errorDescription: String? {
        switch self {
        case .invalidKey: "Enter the 43-character instance key from your server."
        case .invalidLink: "This installation link is invalid."
        case .missingQRCode: "No installation QR code was found in this image."
        case .ambiguousQRCode: "Choose an image containing one installation QR code."
        }
    }
}

public enum ServerAddress {
    public static func normalize(_ address: String) throws -> URL {
        let text = address.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.contains("\\"),
              var parts = URLComponents(string: text),
              let scheme = parts.scheme?.lowercased(), ["https", "http"].contains(scheme),
              let host = parts.host, !host.isEmpty,
              parts.user == nil, parts.password == nil, parts.query == nil, parts.fragment == nil,
              !parts.percentEncodedPath.lowercased().contains("%2f"),
              !parts.percentEncodedPath.lowercased().contains("%5c"),
              !parts.path.split(separator: "/").contains(where: { $0 == "." || $0 == ".." }),
              parts.port.map({ (1...65535).contains($0) }) ?? true else { throw ZoeError.invalidServerURL }
        parts.scheme = scheme
        parts.host = host.lowercased()
        if (scheme == "https" && parts.port == 443) || (scheme == "http" && parts.port == 80) { parts.port = nil }
        while parts.path.hasSuffix("/") { parts.path.removeLast() }
        guard let url = parts.url else { throw ZoeError.invalidServerURL }
        return url
    }
}

/// Validated installation details. The caller decides whether the user authorized connection.
public struct InstallationConnection: Equatable, Sendable {
    public let serverURL: URL
    public let key: InstanceKey
    public init(serverAddress: String, key: String) throws {
        serverURL = try ServerAddress.normalize(serverAddress)
        self.key = try InstanceKey(key)
    }
    public init(link: URL) throws {
        guard let parts = URLComponents(url: link, resolvingAgainstBaseURL: false),
              parts.scheme?.lowercased() == "zoe", parts.host == "connect",
              parts.path.isEmpty, parts.user == nil, parts.password == nil, parts.port == nil,
              let query = parts.queryItems, query.count == 1,
              query[0].name == "server", let server = query[0].value,
              let fragment = parts.fragment, fragment.hasPrefix("key="),
              !fragment.contains("&"), !fragment.contains("%") else { throw ConnectionInputError.invalidLink }
        try self.init(serverAddress: server, key: String(fragment.dropFirst(4)))
    }

    /// Shared camera/image policy: never select one credential from multiple QR payloads.
    public static func decodeQRPayloads(_ payloads: [String]) throws -> InstallationConnection {
        let unique = Set(payloads)
        guard !unique.isEmpty else { throw ConnectionInputError.missingQRCode }
        guard unique.count == 1, let payload = unique.first else { throw ConnectionInputError.ambiguousQRCode }
        guard let link = URL(string: payload) else { throw ConnectionInputError.invalidLink }
        return try InstallationConnection(link: link)
    }
}

/// One Keychain item per normalized server address; no file-backed credential alternative.
public protocol InstanceKeyStoring: BearerTokenProviding {
    func load() throws -> InstanceKey?
    /// Atomically replace the credential; a thrown write must preserve the previous value.
    func replace(_ key: InstanceKey) throws
    func reset() throws
}

public final class InstanceKeyStore: InstanceKeyStoring, @unchecked Sendable {
    let service: String
    public init(serverURL: URL, bundleIdentifier: String = Bundle.main.bundleIdentifier ?? "com.zoe.client") throws {
        let serverURL = try ServerAddress.normalize(serverURL.absoluteString)
        service = "\(bundleIdentifier).instance-key.\(serverURL.absoluteString)"
    }
    private var query: [String: Any] {
        [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: "instance-key"]
    }
    /// Process-wide read cache so repeated requests do not hit Keychain (and its prompts) each time.
    private static let cache = KeyCache()
    private final class KeyCache: @unchecked Sendable {
        private let lock = NSLock()
        private var values: [String: InstanceKey] = [:]
        func get(_ service: String) -> InstanceKey? { lock.lock(); defer { lock.unlock() }; return values[service] }
        func set(_ service: String, _ key: InstanceKey?) { lock.lock(); defer { lock.unlock() }; values[service] = key }
    }

    public func load() throws -> InstanceKey? {
        if let cached = Self.cache.get(service) { return cached }
        let loaded = try loadFromKeychain()
        if let loaded { Self.cache.set(service, loaded) }
        return loaded
    }
    private func loadFromKeychain() throws -> InstanceKey? {
        var request = query
        request[kSecReturnData as String] = true
        request[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(request as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        try check(status)
        guard let data = result as? Data, let text = String(data: data, encoding: .utf8) else { throw ZoeError.persistence("The saved instance key cannot be read.") }
        return try InstanceKey(text)
    }
    public func replace(_ key: InstanceKey) throws {
        let data = Data(try key.bearerToken().utf8)
        let status = SecItemUpdate(query as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        if status == errSecItemNotFound {
            var request = query
            request[kSecValueData as String] = data
            request[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            try check(SecItemAdd(request as CFDictionary, nil))
        } else { try check(status) }
        Self.cache.set(service, key)
    }
    public func reset() throws {
        let status = SecItemDelete(query as CFDictionary)
        if status != errSecItemNotFound { try check(status) }
        Self.cache.set(service, nil)
    }
    public func bearerToken() throws -> String {
        guard let key = try load() else { throw ZoeError.missingCredential }
        return try key.bearerToken()
    }
    private func check(_ status: OSStatus) throws {
        guard status != errSecSuccess else { return }
        if [errSecAuthFailed, errSecInteractionNotAllowed, errSecUserCanceled].contains(status) { throw ZoeError.credentialAccessDenied("Keychain") }
        throw ZoeError.persistence("The instance key could not be accessed in Keychain.")
    }
}
