import XCTest
import Foundation
@testable import ZoeKit

final class InstanceKeyStoreTests: XCTestCase {
    func testKeychainScopeNormalizesEquivalentAddressesAndSeparatesPaths() throws {
        let bundle = "com.zoe.tests.\(UUID().uuidString)"
        let canonical = try InstanceKeyStore(serverURL: URL(string: "https://zoe.example/base")!, bundleIdentifier: bundle)
        let equivalent = try InstanceKeyStore(serverURL: URL(string: "HTTPS://ZOE.example:443/base/")!, bundleIdentifier: bundle)
        let different = try InstanceKeyStore(serverURL: URL(string: "https://zoe.example/other")!, bundleIdentifier: bundle)
        XCTAssertEqual(canonical.service, equivalent.service)
        XCTAssertNotEqual(canonical.service, different.service)
        XCTAssertNotEqual(canonical.service, try InstanceKeyStore(serverURL: URL(string: "http://zoe.example/base")!, bundleIdentifier: bundle).service)
        XCTAssertNotEqual(canonical.service, try InstanceKeyStore(serverURL: URL(string: "https://zoe.example/base")!, bundleIdentifier: bundle + ".other").service)
    }

    func testLoadingEmptyKeychainDoesNotCreateAnInstanceCredential() throws {
        let store = try InstanceKeyStore(serverURL: URL(string: "https://zoe.example")!, bundleIdentifier: "com.zoe.tests.\(UUID().uuidString)")
        XCTAssertNil(try store.load())
        XCTAssertThrowsError(try store.bearerToken())
        XCTAssertNil(try store.load())
    }
}
