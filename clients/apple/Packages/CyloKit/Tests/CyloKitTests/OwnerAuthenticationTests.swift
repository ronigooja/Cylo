import XCTest
import Foundation
@testable import CyloKit
@testable import CyloUI

private final class OwnerAuthURLProtocol: URLProtocol {
    static var handle: ((URLRequest) throws -> (Int, Data))?
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        do {
            let (status, body) = try Self.handle!(request)
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil)!, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: body)
            client?.urlProtocolDidFinishLoading(self)
        } catch { client?.urlProtocol(self, didFailWithError: error) }
    }
    override func stopLoading() {}
}

final class OwnerAuthenticationTests: XCTestCase {
    private func session() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [OwnerAuthURLProtocol.self]
        return URLSession(configuration: configuration)
    }

    func testFreshCredentialsHaveIndependentSecureTokens() throws {
        let first = try DeviceIdentityStore.newLoginIdentity()
        let second = try DeviceIdentityStore.newLoginIdentity()
        XCTAssertNotEqual(first, second)
        XCTAssertEqual(first.deviceToken.count, 43)
        XCTAssertTrue(first.deviceToken.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_") })
    }

    func testSetupRetryKeepsNonceAndTokenAndSendsNoSavedBearer() async throws {
        let candidate = try DeviceIdentityStore.newLoginIdentity()
        let password = UUID().uuidString
        let code = UUID().uuidString
        var bodies: [[String: String]] = []
        OwnerAuthURLProtocol.handle = { request in
            XCTAssertEqual(request.url?.path, "/v1/auth/setup")
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))
            let body = try JSONDecoder().decode([String: String].self, from: request.httpBody!)
            bodies.append(body)
            XCTAssertEqual(body["device_token"], candidate.deviceToken)
            XCTAssertEqual(body["password"], password)
            XCTAssertEqual(body["code"], code)
            if bodies.count == 1 { throw URLError(.networkConnectionLost) }
            return (200, Data(#"{"device":{"id":"device-result","workspace_id":"workspace","name":"Mac","status":"approved","version":1,"created_at":"2026-10-25T00:00:00Z"}}"#.utf8))
        }
        let api = try APIClient(serverURL: URL(string: "http://debian.local:8787")!, credentials: try DeviceIdentityStore.newLoginIdentity(), session: session())
        do {
            _ = try await api.authenticate(password: password, code: code, deviceName: "Mac", identity: candidate)
            XCTFail("First response must be lost")
        } catch is URLError {}
        let registration = try await api.authenticate(password: password, code: code, deviceName: "Mac", identity: candidate)
        XCTAssertEqual(registration.device.id, "device-result")
        XCTAssertEqual(bodies.count, 2)
        XCTAssertEqual(bodies[0], bodies[1])
    }

    func testLoginStatusSessionAndLogoutContract() async throws {
        let identity = try DeviceIdentityStore.newLoginIdentity()
        var paths: [String] = []
        OwnerAuthURLProtocol.handle = { request in
            let path = request.url!.path
            paths.append(path)
            switch path {
            case "/v1/auth/status":
                XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))
                return (200, Data(#"{"setup_required":false}"#.utf8))
            case "/v1/auth/login":
                let body = try JSONSerialization.jsonObject(with: request.httpBody!) as! [String: Any]
                XCTAssertNil(body["code"])
                XCTAssertEqual(body["device_token"] as? String, identity.deviceToken)
                XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))
                return (200, Data(#"{"device":{"id":"device-result","workspace_id":"workspace","name":"Mac","status":"approved","version":1,"created_at":"2026-10-25T00:00:00Z","last_seen_at":"2026-10-25T00:00:00Z"}}"#.utf8))
            case "/v1/auth/session":
                XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer " + identity.deviceToken)
                return (200, Data(#"{"device_id":"device-result"}"#.utf8))
            default:
                XCTAssertEqual(path, "/v1/auth/logout")
                XCTAssertEqual(request.httpMethod, "POST")
                XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer " + identity.deviceToken)
                return (200, Data(#"{"logged_out":true}"#.utf8))
            }
        }
        let api = try APIClient(serverURL: URL(string: "http://debian.local:8787")!, credentials: identity, session: session())
        let status = try await api.authStatus()
        XCTAssertFalse(status.setupRequired)
        _ = try await api.authenticate(password: UUID().uuidString, code: nil, deviceName: "Mac", identity: identity)
        let validated = try await api.authSession()
        XCTAssertEqual(validated.deviceId, "device-result")
        try await api.logout()
        XCTAssertEqual(paths, ["/v1/auth/status", "/v1/auth/login", "/v1/auth/session", "/v1/auth/logout"])
    }

    func testStagingIsSeparateAndCredentialReplacementDoesNotChangeCapturedBearer() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = URL(string: "http://debian.local:8787")!
        let active = PrivateFileDeviceIdentityStore(serverURL: url, bundleIdentifier: "owner-tests", applicationSupportDirectory: directory)
        let staging = PrivateFileDeviceIdentityStore(serverURL: url, bundleIdentifier: "owner-tests.pending", applicationSupportDirectory: directory)
        let old = try DeviceIdentityStore.newLoginIdentity()
        let candidate = try DeviceIdentityStore.newLoginIdentity()
        try active.replace(old)
        try staging.replace(candidate)
        XCTAssertEqual(try active.loadExisting(), old)
        let captured = try XCTUnwrap(active.loadExisting())
        let api = try APIClient(serverURL: url, credentials: captured, session: session())
        try active.replace(candidate)
        try staging.reset()
        XCTAssertNil(try staging.loadExisting())
        XCTAssertEqual(try active.loadExisting(), candidate)
        OwnerAuthURLProtocol.handle = { request in
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer " + old.deviceToken)
            return (401, Data(#"{"error":{"code":"unauthorized","message":"expired"}}"#.utf8))
        }
        do { _ = try await api.authSession(); XCTFail("Old credential must not change") }
        catch { XCTAssertEqual(OwnerAuthFailure.classify(error), .revoked) }
        try active.reset()
        XCTAssertNil(try active.loadExisting())
    }

    @MainActor
    func testRevocationHidesAuthoritativeRecordsAndPreservesUnsentDrafts() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        XCTAssertNil(model.authentication.deviceID)
        model.setAuthenticationForTesting(.authenticated(deviceID: "old-device"))
        model.setSnapshotForTesting(Snapshot(serverVersion: "1", eventCursor: "1", projects: [], tasks: [], approvals: [], devices: [], runs: []))
        model.updateTaskDraft("Unsent conversation")
        model.updateTaskDraft("Unsent task message", for: "task")
        model.snapshotFetchOverrideForTesting = { throw CyloError.http(status: 401, code: "unauthorized", message: "expired", details: nil) }
        do { try await model.refreshSnapshotForTesting(); XCTFail("Revocation must fail refresh") } catch {}
        XCTAssertEqual(model.authentication, .signedOut(setupRequired: false))
        XCTAssertNil(model.snapshot)
        XCTAssertTrue(model.taskEntries.isEmpty)
        XCTAssertEqual(model.newTaskDraft, "Unsent conversation")
        XCTAssertEqual(model.taskDrafts["task"], "Unsent task message")
    }

    func testPasswordPolicyAndTypedFailures() {
        XCTAssertFalse(OwnerPasswordPolicy.acceptsSetup("short"))
        XCTAssertFalse(OwnerPasswordPolicy.acceptsSetup(String(repeating: "字", count: 4)))
        XCTAssertTrue(OwnerPasswordPolicy.acceptsSetup(String(repeating: "字", count: 12)))
        XCTAssertFalse(OwnerPasswordPolicy.acceptsSetup(String(repeating: "字", count: 400)))
        for (status, code, expected) in [(401, "invalid_credentials", OwnerAuthFailure.incorrectPassword), (429, "auth_rate_limited", .rateLimited), (409, "setup_required", .setupRequired), (409, "device_revoked", .revoked)] {
            XCTAssertEqual(OwnerAuthFailure.classify(CyloError.http(status: status, code: code, message: "", details: nil)), expected)
        }
    }
}
