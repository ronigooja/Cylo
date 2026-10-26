import XCTest
import Foundation
import CoreImage
import ImageIO
import UniformTypeIdentifiers
@testable import CyloKit
@testable import CyloUI

private final class InstanceURLProtocol: URLProtocol {
    static var handle: ((URLRequest) throws -> (Int, Data))?
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        do {
            let (status, data) = try XCTUnwrap(Self.handle)(request)
            let response = try XCTUnwrap(HTTPURLResponse(url: XCTUnwrap(request.url), statusCode: status, httpVersion: nil, headerFields: nil))
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch { client?.urlProtocol(self, didFailWithError: error) }
    }
    override func stopLoading() {}
}

private final class MemoryInstanceKeyStore: InstanceKeyStoring, @unchecked Sendable {
    var key: InstanceKey?
    var failWrites = false
    func load() throws -> InstanceKey? { key }
    func replace(_ key: InstanceKey) throws {
        if failWrites { throw CyloError.persistence("Simulated Keychain denial") }
        self.key = key
    }
    func reset() throws { key = nil }
    func bearerToken() throws -> String {
        guard let key else { throw CyloError.missingCredential }
        return try key.bearerToken()
    }
}

final class InstanceConnectionTests: XCTestCase {
    private func key(_ byte: UInt8 = 1) -> String {
        Data(repeating: byte, count: 32).base64EncodedString()
            .replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
    }
    private func link(server: String = "https://cylo.example/base", key: String) -> URL {
        var parts = URLComponents()
        parts.scheme = "cylo"
        parts.host = "connect"
        parts.queryItems = [URLQueryItem(name: "server", value: server)]
        parts.fragment = "key=\(key)"
        return parts.url!
    }
    private func session() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [InstanceURLProtocol.self]
        return URLSession(configuration: configuration)
    }
    private var snapshotData: Data {
        Data(#"{"server_version":"candidate","event_cursor":"0","workspace_id":"default","subject_id":"owner","projects":[],"tasks":[],"approvals":[],"runs":[]}"#.utf8)
    }
    override func tearDown() {
        InstanceURLProtocol.handle = nil
        super.tearDown()
    }

    func testCanonicalKeyRejectsPaddingWhitespaceAndUnusedBits() throws {
        let canonical = key()
        XCTAssertNoThrow(try InstanceKey(canonical))
        for invalid in ["", String(canonical.dropLast()), canonical + "=", " " + canonical, canonical + "\n", String(repeating: "+", count: 43), String(canonical.dropLast()) + "B"] {
            XCTAssertThrowsError(try InstanceKey(invalid))
        }
    }

    func testServerNormalizationAndUnsafeAddresses() throws {
        XCTAssertEqual(try ServerAddress.normalize(" HTTPS://Cylo.Example:443/base/// ").absoluteString, "https://cylo.example/base")
        XCTAssertEqual(try ServerAddress.normalize("http://cylo.local:80/").absoluteString, "http://cylo.local")
        XCTAssertEqual(try ServerAddress.normalize("http://[::1]:8787/").port, 8787)
        for invalid in ["cylo.example", "file:///tmp/server", "https://user:secret@cylo.example", "https://cylo.example?key=hidden", "https://cylo.example#hidden", "https://cylo.example:0", "https://cylo.example:65536", "https://cylo.example/a/../b", "https://cylo.example/a%2fb", "https://cylo.example/a%5cb", "https://cylo.example/\\host"] {
            XCTAssertThrowsError(try ServerAddress.normalize(invalid), invalid)
        }
    }

    func testStrictInstallationLinkAndURLRoundTrip() throws {
        let imported = try InstallationConnection(link: link(server: "https://CYLO.example:443/private/", key: key()))
        XCTAssertEqual(imported.serverURL.absoluteString, "https://cylo.example/private")
        XCTAssertEqual(try imported.key.bearerToken(), key())
        let valid = link(key: key()).absoluteString
        let invalid = [
            valid.replacingOccurrences(of: "cylo://", with: "https://"),
            valid.replacingOccurrences(of: "connect?", with: "connect/other?"),
            valid.replacingOccurrences(of: "connect?", with: "other?"),
            valid + "&extra=value",
            valid.replacingOccurrences(of: "#key=", with: "&key="),
            valid.replacingOccurrences(of: "?server=", with: "?extra=1&server="),
            valid.replacingOccurrences(of: "#key=", with: "&server=https%3A%2F%2Fevil.example#key="),
            valid.replacingOccurrences(of: "#key=", with: "#other="),
            "cylo://user@connect?server=https%3A%2F%2Fcylo.example#key=\(key())",
            "cylo://connect?server=https%3A%2F%2Fcylo.example%23hidden#key=\(key())"
        ]
        for raw in invalid { XCTAssertThrowsError(try InstallationConnection(link: XCTUnwrap(URL(string: raw)))) }
    }

    func testSnapshotWithoutDevicesUsesBearerAndIndependentInstallationHeader() async throws {
        let installation = UUID().uuidString.lowercased()
        let credential = try InstanceKey(key())
        let body = snapshotData
        InstanceURLProtocol.handle = { request in
            XCTAssertEqual(request.url?.path, "/base/v1/snapshot")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer \(try credential.bearerToken())")
            XCTAssertEqual(request.value(forHTTPHeaderField: "X-Cylo-Installation-ID"), installation)
            XCTAssertNil(request.url?.query)
            XCTAssertNil(request.url?.fragment)
            return (200, body)
        }
        let api = try APIClient(serverURL: URL(string: "https://cylo.example/base/")!, credentials: credential, session: session(), installationID: installation)
        let snapshot = try await api.snapshot()
        XCTAssertEqual(snapshot.workspaceId, "default")
        XCTAssertEqual(snapshot.subjectId, "owner")
    }

    func testMissingOrDifferentOwnerContractCannotValidateConnection() async throws {
        for body in [snapshotData.replacingOccurrences(of: "default", with: "another"), snapshotData.replacingOccurrences(of: "owner", with: "another"), Data(#"{"server_version":"old","event_cursor":"0","projects":[],"tasks":[],"approvals":[],"devices":[],"runs":[]}"#.utf8)] {
            InstanceURLProtocol.handle = { _ in (200, body) }
            let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: InstanceKey(key()), session: session())
            do { _ = try await api.snapshot(); XCTFail("Incompatible authority must fail validation") }
            catch { XCTAssertTrue(error is CyloError) }
        }
    }

    @MainActor
    func testFreshInstallHasNoAddressAndImportNeedsExplicitConnect() async throws {
        let defaults = UserDefaults(suiteName: UUID().uuidString)!
        defaults.set("http://obsolete.example", forKey: "cylo.serverAddress")
        let model = CyloAppModel(defaults: defaults)
        XCTAssertEqual(model.serverAddress, "")
        await model.start()
        XCTAssertEqual(model.authentication, .disconnected)
        model.receiveInstallationLink(link(key: key()))
        XCTAssertNotNil(model.pendingConnectionImport)
        XCTAssertEqual(model.serverAddress, "")
        XCTAssertNil(defaults.string(forKey: "cylo.instance.serverAddress"))
        XCTAssertFalse(defaults.dictionaryRepresentation().values.contains { String(describing: $0).contains(key()) })
        model.receiveInstallationLink(URL(string: "cylo://connect?server=invalid#key=invalid")!)
        XCTAssertNil(model.pendingConnectionImport)
        XCTAssertNotNil(model.connectionImportError)
    }

    @MainActor
    func testRejectedReplacementKeepsWorkingConnectionAndDraft() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let store = MemoryInstanceKeyStore()
        let oldKey = try InstanceKey(key())
        try store.replace(oldKey)
        model.keyStoreOverrideForTesting = { _ in store }
        model.serverAddress = "https://cylo.example"
        model.setAuthenticationForTesting(.connected)
        model.updateTaskDraft("Keep this draft")
        model.connectionAPIOverrideForTesting = { url, key, installation in
            try APIClient(serverURL: url, credentials: key, session: self.session(), installationID: installation)
        }
        InstanceURLProtocol.handle = { _ in (401, Data(#"{"error":{"code":"unauthorized","message":"Rejected"}}"#.utf8)) }
        let accepted = await model.connect(serverAddress: model.serverAddress, key: key(2))
        XCTAssertFalse(accepted)
        XCTAssertEqual(model.authentication, .connected)
        XCTAssertEqual(model.newTaskDraft, "Keep this draft")
        XCTAssertEqual(try store.load(), oldKey)
        XCTAssertFalse(model.isBusy)
        XCTAssertFalse(model.notice?.contains(key(2)) ?? true)
    }

    @MainActor
    func testConfirmedReplacementPersistsKeyAndClearsLocalQueueAndDrafts() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let store = MemoryInstanceKeyStore()
        try store.replace(InstanceKey(key()))
        model.keyStoreOverrideForTesting = { _ in store }
        model.connectionAPIOverrideForTesting = { url, key, installation in
            try APIClient(serverURL: url, credentials: key, session: self.session(), installationID: installation)
        }
        model.suppressConnectionFollowupsForTesting = true
        model.serverAddress = "https://cylo.example"
        model.setAuthenticationForTesting(.connected)
        model.updateTaskDraft("Discard this draft")
        let body = snapshotData
        InstanceURLProtocol.handle = { _ in (200, body) }
        let accepted = await model.connect(serverAddress: model.serverAddress, key: key(2))
        XCTAssertTrue(accepted)
        XCTAssertEqual(model.authentication, .connected)
        XCTAssertEqual(try store.load(), try InstanceKey(key(2)))
        XCTAssertEqual(model.newTaskDraft, "")
        let before = model.connectionEpoch
        await model.clearCredential()
        XCTAssertNil(try store.load())
        XCTAssertEqual(model.serverAddress, "")
        XCTAssertEqual(model.authentication, .disconnected)
        XCTAssertGreaterThan(model.connectionEpoch, before)
    }

    @MainActor
    func testDifferentServerRequiresExplicitClearAndKeychainFailureStaysDisconnected() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.serverAddress = "https://cylo.example"
        model.setAuthenticationForTesting(.connected)
        let switched = await model.connect(serverAddress: "https://another.example", key: key())
        XCTAssertFalse(switched)
        XCTAssertEqual(model.serverAddress, "https://cylo.example")
        XCTAssertEqual(model.authentication, .connected)
        let store = MemoryInstanceKeyStore()
        model.keyStoreOverrideForTesting = { _ in store }
        await model.clearCredential()
        store.failWrites = true
        model.connectionAPIOverrideForTesting = { url, key, installation in
            try APIClient(serverURL: url, credentials: key, session: self.session(), installationID: installation)
        }
        let body = snapshotData
        InstanceURLProtocol.handle = { _ in (200, body) }
        let accepted = await model.connect(serverAddress: "https://another.example", key: key())
        XCTAssertFalse(accepted)
        XCTAssertEqual(model.authentication, .disconnected)
        XCTAssertFalse(model.isBusy)
        XCTAssertNil(try store.load())
    }

    @MainActor
    func testSavedServerRemainsLockedAfterKeyRejectionOrUnavailableConnection() async throws {
        for state in [InstanceConnectionState.disconnected, .unavailable] {
            let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
            model.serverAddress = "https://cylo.example"
            model.setAuthenticationForTesting(state)
            let store = MemoryInstanceKeyStore()
            try store.replace(InstanceKey(key()))
            model.keyStoreOverrideForTesting = { _ in store }
            model.connectionAPIOverrideForTesting = { _, _, _ in
                XCTFail("A different server must be refused before any request")
                throw CyloError.invalidServerURL
            }
            let accepted = await model.connect(serverAddress: "https://another.example", key: key(2))
            XCTAssertFalse(accepted)
            XCTAssertEqual(model.serverAddress, "https://cylo.example")
            XCTAssertEqual(model.authentication, state)
            XCTAssertEqual(try store.load(), try InstanceKey(key()))
            await model.clearCredential()
            XCTAssertEqual(model.serverAddress, "")
            XCTAssertNil(try store.load())
        }
    }

    func testQRImageDecodesActualInstallationLink() throws {
        let filter = try XCTUnwrap(CIFilter(name: "CIQRCodeGenerator"))
        let expected = link(key: key())
        filter.setValue(Data(expected.absoluteString.utf8), forKey: "inputMessage")
        let code = try XCTUnwrap(filter.outputImage).transformed(by: CGAffineTransform(scaleX: 8, y: 8))
        let bounds = code.extent.insetBy(dx: -32, dy: -32)
        let image = code.composited(over: CIImage(color: .white).cropped(to: bounds))
        let cgImage = try XCTUnwrap(CIContext().createCGImage(image, from: bounds))
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathExtension("png")
        defer { try? FileManager.default.removeItem(at: url) }
        let output = try XCTUnwrap(CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil))
        CGImageDestinationAddImage(output, cgImage, nil)
        XCTAssertTrue(CGImageDestinationFinalize(output))
        let imported = try InstallationQRImage.decode(url)
        XCTAssertEqual(imported, try InstallationConnection(link: expected))
    }
}

private extension Data {
    func replacingOccurrences(of target: String, with replacement: String) -> Data {
        Data(String(decoding: self, as: UTF8.self).replacingOccurrences(of: target, with: replacement).utf8)
    }
}
