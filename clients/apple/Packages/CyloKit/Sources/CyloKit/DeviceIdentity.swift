import Foundation
import Security

public struct DeviceIdentity: Equatable, Sendable {
    public let bootstrapRequestID: String
    public let pairingRequestID: String
    public let deviceToken: String
}

public final class DeviceIdentityStore: @unchecked Sendable {
    private let service: String

    public init(serverURL: URL, bundleIdentifier: String = Bundle.main.bundleIdentifier ?? "com.cylo.client") {
        service = "\(bundleIdentifier).device.\(serverURL.scheme ?? "https")://\(serverURL.host ?? "server"):\(serverURL.port ?? 443)\(serverURL.path)"
    }

    /// The device bearer is committed to Keychain before it can be sent over the network.
    public func loadOrCreate() throws -> DeviceIdentity {
        if let data = try read() {
            guard let value = try? JSONDecoder().decode(DeviceIdentityRecord.self, from: data) else {
                throw CyloError.persistence("The saved device identity cannot be read.")
            }
            return DeviceIdentity(bootstrapRequestID: value.bootstrapRequestID, pairingRequestID: value.pairingRequestID, deviceToken: value.deviceToken)
        }

        var random = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, random.count, &random) == errSecSuccess else {
            throw CyloError.persistence("Secure random device identity generation failed.")
        }
        let token = Data(random).base64URLEncodedString()
        let record = DeviceIdentityRecord(
            bootstrapRequestID: UUID().uuidString.lowercased(),
            pairingRequestID: UUID().uuidString.lowercased(),
            deviceToken: token
        )
        let data = try JSONEncoder().encode(record)
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: "identity",
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        ]
        let status = SecItemAdd(query as CFDictionary, nil)
        guard status == errSecSuccess else {
            throw CyloError.persistence("Could not save the device identity in Keychain (\(status)).")
        }
        return DeviceIdentity(bootstrapRequestID: record.bootstrapRequestID, pairingRequestID: record.pairingRequestID, deviceToken: token)
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
            throw CyloError.persistence("Could not read the saved device identity (\(status)).")
        }
        return result as? Data
    }
}

private struct DeviceIdentityRecord: Codable {
    var bootstrapRequestID: String
    var pairingRequestID: String
    var deviceToken: String
}

private extension Data {
    func base64URLEncodedString() -> String {
        base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}
