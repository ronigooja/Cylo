import Foundation
import XCTest
@testable import CyloKit
@testable import CyloUI

final class CredentialStorageTests: XCTestCase {
    func testPrivateFileStoreBearerReadDoesNotCreateIdentity() throws {
        let root = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: root) }
        let store = PrivateFileDeviceIdentityStore(serverURL: server("https://one.example"), bundleIdentifier: "com.cylo.test", applicationSupportDirectory: root)

        XCTAssertThrowsError(try store.bearerToken()) { error in
            guard let cyloError = error as? CyloError, case .missingCredential = cyloError else {
                return XCTFail("Expected a missing-identity error")
            }
        }
        XCTAssertTrue(identityFiles(under: root).isEmpty)
    }

    func testPrivateFileIdentityIsOriginScopedAndHasPrivatePermissions() throws {
        let root = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: root) }
        let first = PrivateFileDeviceIdentityStore(serverURL: server("https://one.example"), bundleIdentifier: "com.cylo.test", applicationSupportDirectory: root)
        let second = PrivateFileDeviceIdentityStore(serverURL: server("https://two.example"), bundleIdentifier: "com.cylo.test", applicationSupportDirectory: root)

        let firstIdentity = try first.loadOrCreate()
        let secondIdentity = try second.loadOrCreate()

        XCTAssertNotEqual(firstIdentity.deviceToken, secondIdentity.deviceToken)
        XCTAssertEqual(try first.loadExisting(), firstIdentity)
        XCTAssertEqual(try second.loadExisting(), secondIdentity)
        let files = identityFiles(under: root)
        XCTAssertEqual(files.count, 2)
        var checkedDirectories = Set<String>()
        for file in files {
            let fileMode = try fileManager.attributesOfItem(atPath: file.path)[.posixPermissions] as? NSNumber
            XCTAssertEqual(fileMode?.intValue, 0o600)
            var directory = file.deletingLastPathComponent()
            for _ in 0..<3 {
                if checkedDirectories.insert(directory.path).inserted {
                    let directoryMode = try fileManager.attributesOfItem(atPath: directory.path)[.posixPermissions] as? NSNumber
                    XCTAssertEqual(directoryMode?.intValue, 0o700)
                }
                directory = directory.deletingLastPathComponent()
            }
        }
    }

    func testPrivateFileStoreWritesIdentityAtomicallyAndReloadsSameRecord() throws {
        let root = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: root) }
        let store = PrivateFileDeviceIdentityStore(serverURL: server("http://cylo.local:8787/base"), bundleIdentifier: "com.cylo.test", applicationSupportDirectory: root)
        let identity = DeviceIdentity(bootstrapRequestID: "bootstrap-test-id", pairingRequestID: "pairing-test-id", deviceToken: "test-device-token")

        XCTAssertEqual(try store.storeIfAbsent(identity), identity)
        XCTAssertEqual(try store.loadExisting(), identity)
        XCTAssertEqual(identityFiles(under: root).count, 1)
        let allFiles = fileManager.enumerator(at: root, includingPropertiesForKeys: nil)?.compactMap { $0 as? URL } ?? []
        XCTAssertFalse(allFiles.contains { $0.pathExtension == "tmp" })
    }

    func testPrivateFileStoreNeverOverwritesDifferentIdentityDuringMigration() throws {
        let root = temporaryDirectory()
        defer { try? FileManager.default.removeItem(at: root) }
        let store = PrivateFileDeviceIdentityStore(serverURL: server("https://one.example"), bundleIdentifier: "com.cylo.test", applicationSupportDirectory: root)
        let saved = DeviceIdentity(bootstrapRequestID: "saved-bootstrap", pairingRequestID: "saved-pairing", deviceToken: "saved-token")
        let conflicting = DeviceIdentity(bootstrapRequestID: "other-bootstrap", pairingRequestID: "other-pairing", deviceToken: "other-token")
        _ = try store.storeIfAbsent(saved)

        XCTAssertThrowsError(try store.storeIfAbsent(conflicting)) { error in
            guard let cyloError = error as? CyloError, case .credentialConflict = cyloError else {
                return XCTFail("Expected a credential conflict")
            }
        }
        XCTAssertEqual(try store.loadExisting(), saved)
    }

    func testCredentialStoragePreferenceIsSeparatePerServerOrigin() {
        let firstKey = CredentialStorageChoice.defaultsKey(serverURL: server("https://one.example"), bundleIdentifier: "com.cylo.test")
        let secondKey = CredentialStorageChoice.defaultsKey(serverURL: server("https://two.example"), bundleIdentifier: "com.cylo.test")
        XCTAssertNotEqual(firstKey, secondKey)
    }

    private var fileManager: FileManager { .default }

    private func temporaryDirectory() -> URL {
        fileManager.temporaryDirectory.appendingPathComponent("cylo-credential-test-\(UUID().uuidString)", isDirectory: true)
    }

    private func server(_ value: String) -> URL { URL(string: value)! }

    private func identityFiles(under root: URL) -> [URL] {
        guard let enumerator = fileManager.enumerator(at: root, includingPropertiesForKeys: nil) else { return [] }
        return enumerator.compactMap { $0 as? URL }.filter { $0.lastPathComponent == "identity.json" }
    }
}

extension CredentialStorageTests {
    func testKeychainBearerReadDoesNotCreateAnIdentity() throws {
        let bundleIdentifier = "com.cylo.credential-test.\(UUID().uuidString)"
        let store = DeviceIdentityStore(serverURL: server("https://keychain-no-create.example"), bundleIdentifier: bundleIdentifier)

        XCTAssertThrowsError(try store.bearerToken()) { error in
            guard let cyloError = error as? CyloError, case .missingCredential = cyloError else {
                return XCTFail("Expected a missing-identity error")
            }
        }
        XCTAssertNil(try store.loadExisting())
    }

    @MainActor
    func testFixedServerIgnoresSavedAddressAndDefaultsToKeychain() {
        let suite = "com.cylo.credential-test.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }
        defaults.set("https://old-server.example", forKey: "cylo.serverAddress")
        let model = CyloAppModel(defaults: defaults)

        XCTAssertEqual(model.serverAddress, "http://debian.local:8787")
        XCTAssertEqual(defaults.string(forKey: "cylo.serverAddress"), model.serverAddress)
        XCTAssertEqual(model.credentialStorageChoice, .keychain)
        let store = CredentialStorageChoice.defaultsKey(serverURL: URL(string: model.serverAddress)!)
        XCTAssertEqual(defaults.string(forKey: store), "keychain")
    }

    @MainActor
    func testFixedServerPreservesExplicitStorageChoice() {
        let suite = "com.cylo.credential-test.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }
        let key = CredentialStorageChoice.defaultsKey(serverURL: URL(string: CyloAppModel.fixedServerAddress)!)
        defaults.set("privateFile", forKey: key)

        let model = CyloAppModel(defaults: defaults)

        XCTAssertEqual(model.credentialStorageChoice, .privateFile)
        XCTAssertEqual(defaults.string(forKey: key), "privateFile")
    }
}
