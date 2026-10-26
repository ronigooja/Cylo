import XCTest
import Foundation
@testable import CyloKit

final class InstanceKeyStoreTests: XCTestCase {
    func testKeychainScopeNormalizesEquivalentAddressesAndSeparatesPaths() throws {
        let bundle = "com.cylo.tests.\(UUID().uuidString)"
        let canonical = try InstanceKeyStore(serverURL: URL(string: "https://cylo.example/base")!, bundleIdentifier: bundle)
        let equivalent = try InstanceKeyStore(serverURL: URL(string: "HTTPS://CYLO.example:443/base/")!, bundleIdentifier: bundle)
        let different = try InstanceKeyStore(serverURL: URL(string: "https://cylo.example/other")!, bundleIdentifier: bundle)
        XCTAssertEqual(canonical.service, equivalent.service)
        XCTAssertNotEqual(canonical.service, different.service)
        XCTAssertNotEqual(canonical.service, try InstanceKeyStore(serverURL: URL(string: "http://cylo.example/base")!, bundleIdentifier: bundle).service)
        XCTAssertNotEqual(canonical.service, try InstanceKeyStore(serverURL: URL(string: "https://cylo.example/base")!, bundleIdentifier: bundle + ".other").service)
    }

    func testLoadingEmptyKeychainDoesNotCreateAnInstanceCredential() throws {
        let store = try InstanceKeyStore(serverURL: URL(string: "https://cylo.example")!, bundleIdentifier: "com.cylo.tests.\(UUID().uuidString)")
        XCTAssertNil(try store.load())
        XCTAssertThrowsError(try store.bearerToken())
        XCTAssertNil(try store.load())
    }
}
