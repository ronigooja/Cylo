import Foundation
import Security

public struct DeviceIdentity: Equatable, Sendable, BearerTokenProviding {
    public let bootstrapRequestID: String
    public let pairingRequestID: String
    public let deviceToken: String
    public func bearerToken() throws -> String { deviceToken }
}

public protocol DeviceIdentityStoring: BearerTokenProviding {
    func loadExisting() throws -> DeviceIdentity?
    func loadOrCreate() throws -> DeviceIdentity
    func storeIfAbsent(_ identity: DeviceIdentity) throws -> DeviceIdentity
    func replace(_ identity: DeviceIdentity) throws
    func reset() throws
}

public final class DeviceIdentityStore: DeviceIdentityStoring, @unchecked Sendable {
    private let service: String

    public init(serverURL: URL, bundleIdentifier: String = Bundle.main.bundleIdentifier ?? "com.cylo.client") {
        service = "\(bundleIdentifier).device.\(serverURL.scheme ?? "https")://\(serverURL.host ?? "server"):\(serverURL.port ?? 443)\(serverURL.path)"
    }

    /// Reads only. It never creates an identity or falls back to another store.
    public func loadExisting() throws -> DeviceIdentity? {
        guard let data = try read() else { return nil }
        guard let value = try? JSONDecoder().decode(DeviceIdentityRecord.self, from: data) else {
            throw CyloError.persistence("The saved device identity cannot be read.")
        }
        return value.identity
    }

    /// Used only for an explicit identity migration; owner login creates a fresh staged identity.
    public func loadOrCreate() throws -> DeviceIdentity {
        if let identity = try loadExisting() { return identity }
        return try storeIfAbsent(DeviceIdentityRecord.newIdentity())
    }

    /// Copies an identity only when Keychain is empty. Existing different identities are preserved.
    public func storeIfAbsent(_ identity: DeviceIdentity) throws -> DeviceIdentity {
        if let existing = try loadExisting() {
            guard existing == identity else { throw CyloError.credentialConflict }
            return existing
        }
        let data = try JSONEncoder().encode(DeviceIdentityRecord(identity))
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: "identity",
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        ]
        let status = SecItemAdd(query as CFDictionary, nil)
        if status == errSecDuplicateItem, let existing = try loadExisting() {
            guard existing == identity else { throw CyloError.credentialConflict }
            return existing
        }
        guard status == errSecSuccess else {
            if Self.isAccessDenied(status) { throw CyloError.credentialAccessDenied("Keychain") }
            throw CyloError.persistence("Could not save the device identity in Keychain (\(status)).")
        }
        return identity
    }

    /// Replaces a revoked or signed-out identity only after a successful explicit login.
    public func replace(_ identity: DeviceIdentity) throws {
        let data = try JSONEncoder().encode(DeviceIdentityRecord(identity))
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                                   kSecAttrService as String: service,
                                   kSecAttrAccount as String: "identity"]
        let status = SecItemUpdate(query as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        if status == errSecItemNotFound { _ = try storeIfAbsent(identity); return }
        guard status == errSecSuccess else {
            if Self.isAccessDenied(status) { throw CyloError.credentialAccessDenied("Keychain") }
            throw CyloError.persistence("Could not save the device identity in Keychain.")
        }
    }

    public static func newLoginIdentity() throws -> DeviceIdentity {
        try DeviceIdentityRecord.newIdentity()
    }

    public func reset() throws {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: "identity"
        ]
        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw CyloError.persistence("Could not remove the saved device identity (\(status)).")
        }
    }

    public func bearerToken() throws -> String {
        guard let identity = try loadExisting() else { throw CyloError.missingCredential }
        return identity.deviceToken
    }

    private func read() throws -> Data? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: "identity",
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne
        ]
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound { return nil }
        guard status == errSecSuccess else {
            if Self.isAccessDenied(status) { throw CyloError.credentialAccessDenied("Keychain") }
            throw CyloError.persistence("Could not read the saved device identity (\(status)).")
        }
        return result as? Data
    }

    private static func isAccessDenied(_ status: OSStatus) -> Bool {
        status == errSecAuthFailed || status == errSecInteractionNotAllowed || status == errSecUserCanceled
    }
}

struct DeviceIdentityRecord: Codable, Equatable {
    var bootstrapRequestID: String
    var pairingRequestID: String
    var deviceToken: String

    init(_ identity: DeviceIdentity) {
        bootstrapRequestID = identity.bootstrapRequestID
        pairingRequestID = identity.pairingRequestID
        deviceToken = identity.deviceToken
    }

    var identity: DeviceIdentity {
        DeviceIdentity(bootstrapRequestID: bootstrapRequestID, pairingRequestID: pairingRequestID, deviceToken: deviceToken)
    }

    static func newIdentity() throws -> DeviceIdentity {
        var random = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, random.count, &random) == errSecSuccess else {
            throw CyloError.persistence("Secure random device identity generation failed.")
        }
        return DeviceIdentity(
            bootstrapRequestID: UUID().uuidString.lowercased(),
            pairingRequestID: UUID().uuidString.lowercased(),
            deviceToken: Data(random).base64URLEncodedString()
        )
    }
}

private extension Data {
    func base64URLEncodedString() -> String {
        base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}
