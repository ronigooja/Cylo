import XCTest
@testable import CyloKit
@testable import CyloUI
import Foundation

private final class ExportURLProtocol: URLProtocol {
    static var responseHandler: ((URLRequest) -> (Int, [String: String], Data))?
    static var capturedRequest: URLRequest?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        Self.capturedRequest = request
        let (status, headers, body) = Self.responseHandler?(request) ?? (500, [:], Data())
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1", headerFields: headers)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

private struct StaticExportCredential: BearerTokenProviding {
    func bearerToken() throws -> String { UUID().uuidString }
}

private actor AsyncTestGate {
    private var continuation: CheckedContinuation<Void, Never>?
    private var startWaiter: CheckedContinuation<Void, Never>?
    private var hasStarted = false

    func pause() async {
        await withCheckedContinuation { continuation in
            self.continuation = continuation
            hasStarted = true
            startWaiter?.resume()
            startWaiter = nil
        }
    }

    func waitUntilStarted() async {
        guard !hasStarted else { return }
        await withCheckedContinuation { startWaiter = $0 }
    }

    func resume() {
        continuation?.resume()
        continuation = nil
    }
}

final class ClientContractTests: XCTestCase {
    private func exportSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ExportURLProtocol.self]
        return URLSession(configuration: configuration)
    }

    private func exportBody(scope: String = "personal", schemaVersion: Int = 1) -> Data {
        Data(#"{"manifest":{"format":"cylo.authority-export","schema_version":\#(schemaVersion),"export_id":"export-1","created_at":"2026-10-21T12:00:00Z","workspace_id":"workspace-1","subject_id":"device-1","scope":"\#(scope)","authority":{"snapshot_event_cursor":"17"},"coverage":{}},"projects":[],"tasks":[],"task_dependencies":[],"task_entries":[],"approvals":[],"runs":[],"evidence":[],"memory":[]}"#.utf8)
    }

    private func memoryClaim(id: String, content: String) -> MemoryClaim {
        MemoryClaim(id: id, workspaceId: "workspace-1", scope: MemoryScope(kind: "personal", id: nil, principalId: "subject-1"), subject: "preference", key: "format", content: content, authorityKind: "user_confirmed", status: "active", revision: 1, validFrom: nil, validTo: nil, supersedes: nil, sources: [], deletedAt: nil)
    }

    func testScopedExportUsesAuthenticatedScopedRequestAndPrivateTemporaryFile() async throws {
        ExportURLProtocol.capturedRequest = nil
        ExportURLProtocol.responseHandler = { _ in
            (200, ["Content-Type": "application/vnd.cylo.export+json; charset=utf-8", "Content-Disposition": "attachment; filename=\"cylo-export-exp-1.json\""], self.exportBody())
        }
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let client = try APIClient(serverURL: URL(string: "https://cylo.example/base")!, credentials: StaticExportCredential(), session: exportSession())

        let artifact = try await client.downloadExport(scope: "personal", temporaryDirectory: directory)

        let request = try XCTUnwrap(ExportURLProtocol.capturedRequest)
        XCTAssertEqual(request.httpMethod, "GET")
        XCTAssertEqual(URLComponents(url: try XCTUnwrap(request.url), resolvingAgainstBaseURL: false)?.path, "/base/v1/export")
        XCTAssertEqual(URLComponents(url: try XCTUnwrap(request.url), resolvingAgainstBaseURL: false)?.queryItems, [URLQueryItem(name: "scope", value: "personal")])
        XCTAssertEqual(request.value(forHTTPHeaderField: "Accept"), "application/vnd.cylo.export+json")
        XCTAssertTrue(try XCTUnwrap(request.value(forHTTPHeaderField: "Authorization")).hasPrefix("Bearer "))
        XCTAssertEqual(artifact.scope, "personal")
        XCTAssertEqual(artifact.filename, "cylo-export-exp-1.json")
        XCTAssertEqual(artifact.snapshotCursor, "17")
        XCTAssertEqual(try Data(contentsOf: artifact.fileURL), exportBody())
        let attributes = try FileManager.default.attributesOfItem(atPath: artifact.fileURL.path)
        XCTAssertEqual((attributes[.posixPermissions] as? NSNumber)?.intValue, 0o600)
        try FileManager.default.removeItem(at: artifact.fileURL)
    }

    func testScopedExportRejectsInvalidScopeSchemaFilenameAndDeclaredOversize() async throws {
        let client = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        do { _ = try await client.downloadExport(scope: "all"); XCTFail("invalid scope accepted") } catch { if case CyloError.invalidResponse = error {} else { XCTFail("unexpected error: \(error)") } }

        for (headers, body) in [
            (["Content-Type": "application/vnd.cylo.export+json", "Content-Disposition": "attachment; filename=\"cylo-export-x.json\""], exportBody(schemaVersion: 2)),
            (["Content-Type": "application/vnd.cylo.export+json", "Content-Disposition": "attachment; filename=\"../cylo-export-x.json\""], exportBody()),
            (["Content-Type": "application/vnd.cylo.export+json", "Content-Disposition": "attachment; filename=\"cylo-export-x.json\""], exportBody(scope: "workspace"))
        ] {
            ExportURLProtocol.responseHandler = { _ in (200, headers, body) }
            do { _ = try await client.downloadExport(scope: "personal"); XCTFail("invalid export accepted") }
            catch { if case CyloError.invalidResponse = error {} else { XCTFail("unexpected error: \(error)") } }
        }

        ExportURLProtocol.responseHandler = { _ in
            (200, ["Content-Type": "application/vnd.cylo.export+json", "Content-Disposition": "attachment; filename=\"cylo-export-x.json\"", "Content-Length": String(64 * 1024 * 1024 + 1)], Data())
        }
        do { _ = try await client.downloadExport(scope: "personal"); XCTFail("oversized export accepted") }
        catch { if case CyloError.exportTooLarge = error {} else { XCTFail("unexpected error: \(error)") } }
    }

    @MainActor
    func testExportResultFromOldConnectionIsDeletedAndNotPresented() async throws {
        let defaults = UserDefaults(suiteName: UUID().uuidString)!
        let model = CyloAppModel(defaults: defaults)
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let gate = AsyncTestGate()
        var producedURL: URL?
        var shouldPause = true
        model.exportDownloadOverrideForTesting = { scope in
            XCTAssertEqual(scope, "personal")
            if shouldPause {
                shouldPause = false
                await gate.pause()
            }
            let url = directory.appendingPathComponent("late.json")
            try Data("{}".utf8).write(to: url)
            producedURL = url
            return ScopedExportArtifact(fileURL: url, filename: "cylo-export-late.json", scope: scope, snapshotCursor: "1")
        }
        let epoch = model.connectionEpoch
        let task = Task { await model.prepareExport(scope: "personal", expectedConnectionEpoch: epoch) }
        await gate.waitUntilStarted()
        model.serverAddress = ""
        await model.updateServerAddress()
        await gate.resume()
        await task.value
        XCTAssertNil(model.exportArtifact)
        XCTAssertNil(model.exportStatus, "switching connections should clear the stale Preparing status")
        XCTAssertFalse(FileManager.default.fileExists(atPath: try XCTUnwrap(producedURL).path))

        await model.prepareExport(scope: "personal", expectedConnectionEpoch: model.connectionEpoch)
        XCTAssertEqual(model.exportArtifact?.filename, "cylo-export-late.json", "a later request on the new epoch should be allowed")
        model.cancelExport()
    }

    @MainActor
    func testExportSaveAndCancelRemovePreparedTemporaryFile() throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let fileURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try Data("{}".utf8).write(to: fileURL)
        model.setExportArtifactForTesting(ScopedExportArtifact(fileURL: fileURL, filename: "cylo-export-one.json", scope: "personal", snapshotCursor: "1"))

        model.exportSaveFinished(saved: false)

        XCTAssertNil(model.exportArtifact)
        XCTAssertFalse(FileManager.default.fileExists(atPath: fileURL.path))
        XCTAssertTrue(model.exportStatus?.contains("canceled") == true)

        let secondURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try Data("{}".utf8).write(to: secondURL)
        model.setExportArtifactForTesting(ScopedExportArtifact(fileURL: secondURL, filename: "cylo-export-two.json", scope: "workspace", snapshotCursor: "2"))
        model.cancelExport()

        XCTAssertNil(model.exportArtifact)
        XCTAssertFalse(FileManager.default.fileExists(atPath: secondURL.path))
    }

    @MainActor
    func testCanceledPrepareTaskCleansLateFileAndCanBeRetried() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let gate = AsyncTestGate()
        var shouldPause = true
        var producedURL: URL?
        model.exportDownloadOverrideForTesting = { scope in
            if shouldPause {
                shouldPause = false
                await gate.pause()
            }
            let url = directory.appendingPathComponent("task-cancelled.json")
            try Data("{}".utf8).write(to: url)
            producedURL = url
            return ScopedExportArtifact(fileURL: url, filename: "cylo-export-task-cancelled.json", scope: scope, snapshotCursor: "1")
        }

        let task = Task { await model.prepareExport(scope: "personal", expectedConnectionEpoch: model.connectionEpoch) }
        await gate.waitUntilStarted()
        task.cancel()
        await gate.resume()
        await task.value

        XCTAssertNil(model.exportArtifact)
        XCTAssertFalse(model.exportStatus == "Preparing the authorized export…")
        XCTAssertFalse(FileManager.default.fileExists(atPath: try XCTUnwrap(producedURL).path))
        await model.prepareExport(scope: "personal", expectedConnectionEpoch: model.connectionEpoch)
        XCTAssertEqual(model.exportArtifact?.filename, "cylo-export-task-cancelled.json")
        model.cancelExport()
    }

    @MainActor
    func testMemoryReadAuthorizationFailureClearsCachedClaimsAndPreparedExport() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.setMemoryClaimsForTesting([memoryClaim(id: "memory-old", content: "stale private content")])
        let fileURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try Data("export containing stale memory".utf8).write(to: fileURL)
        model.setExportArtifactForTesting(ScopedExportArtifact(fileURL: fileURL, filename: "cylo-export-old.json", scope: "personal", snapshotCursor: "5"))
        model.memorySearchOverrideForTesting = { _ in
            throw CyloError.http(status: 403, code: "forbidden", message: "memory read denied", details: nil)
        }

        await model.refreshMemory()

        XCTAssertTrue(model.memoryClaims.isEmpty)
        XCTAssertTrue(model.memoryMissing.isEmpty)
        XCTAssertTrue(model.memoryConflicts.isEmpty)
        XCTAssertNil(model.exportArtifact)
        XCTAssertFalse(FileManager.default.fileExists(atPath: fileURL.path))
        XCTAssertTrue(model.exportStatus?.contains("access was denied") == true)
    }

    @MainActor
    func testConnectionChangeClearsMemoryEvenWhenNewAddressIsInvalid() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.setMemoryClaimsForTesting([memoryClaim(id: "memory-old-origin", content: "old origin private content")])
        model.serverAddress = "invalid-url"

        await model.updateServerAddress()

        XCTAssertTrue(model.memoryClaims.isEmpty)
        XCTAssertTrue(model.memoryMissing.isEmpty)
        XCTAssertTrue(model.memoryConflicts.isEmpty)
    }

    @MainActor
    func testMemoryEventClearsStaleClaimAndPreparedExportBeforeRefreshing() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.setMemoryClaimsForTesting([memoryClaim(id: "memory-deleted", content: "deleted secret")])
        let fileURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try Data("export containing deleted memory".utf8).write(to: fileURL)
        model.setExportArtifactForTesting(ScopedExportArtifact(fileURL: fileURL, filename: "cylo-export-old.json", scope: "personal", snapshotCursor: "5"))
        model.memorySearchOverrideForTesting = { _ in MemorySearchResult(items: [self.memoryClaim(id: "memory-current", content: "current result")], missing: [], conflicts: []) }

        await model.handleEventForTesting(EventEnvelope(schemaVersion: 1, eventId: "event-1", cursor: "6", type: "memory.deleted", occurredAt: "2026-10-21T12:00:00Z", workspaceId: "workspace-1", subjectId: "subject-1", aggregateType: "memory", aggregateId: "memory-deleted", aggregateVersion: 2, payload: .object([:])))

        XCTAssertEqual(model.memoryClaims.map(\.id), ["memory-current"])
        XCTAssertNil(model.exportArtifact)
        XCTAssertFalse(FileManager.default.fileExists(atPath: fileURL.path))
    }

    @MainActor
    func testPreparedExportAuditEventDoesNotInvalidateItsOwnArtifact() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let fileURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try Data("prepared export".utf8).write(to: fileURL)
        model.setExportArtifactForTesting(ScopedExportArtifact(fileURL: fileURL, filename: "cylo-export-one.json", scope: "personal", snapshotCursor: "5"))

        await model.handleEventForTesting(EventEnvelope(schemaVersion: 1, eventId: "event-export", cursor: "6", type: "data.export.prepared", occurredAt: "2026-10-21T12:00:00Z", workspaceId: "workspace-1", subjectId: "subject-1", aggregateType: "data_export", aggregateId: "export-one", aggregateVersion: 1, payload: .object([:])))

        XCTAssertNotNil(model.exportArtifact)
        XCTAssertTrue(FileManager.default.fileExists(atPath: fileURL.path))
        model.cancelExport()
    }

    @MainActor
    func testNewMemoryQueryFencesOldInFlightResultAndCoalescesToLatestQuery() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.setMemoryClaimsForTesting([memoryClaim(id: "memory-before", content: "before")])
        let gate = AsyncTestGate()
        var observedQueries: [String] = []
        model.memorySearchOverrideForTesting = { query in
            observedQueries.append(query)
            if query == "old" { await gate.pause() }
            return MemorySearchResult(items: [self.memoryClaim(id: "memory-\(query)", content: query)], missing: [], conflicts: [])
        }

        let oldSearch = Task { await model.refreshMemory(query: "old") }
        await gate.waitUntilStarted()
        await model.refreshMemory(query: "new")
        await gate.resume()
        await oldSearch.value

        XCTAssertEqual(observedQueries, ["old", "new"])
        XCTAssertEqual(model.memoryClaims.map(\.id), ["memory-new"])
    }

    func testConnectionGenerationRejectsResponsesAfterOriginOrCredentialSwitch() throws {
        let serverA = try XCTUnwrap(URL(string: "https://server-a.example"))
        let serverB = try XCTUnwrap(URL(string: "https://server-b.example"))
        var generation = ConnectionGeneration(origin: serverA)
        let originalRequest = generation.value

        XCTAssertTrue(generation.accepts(originalRequest, from: serverA))
        generation.switchOrigin(to: serverB)
        XCTAssertFalse(generation.accepts(originalRequest, from: serverA))
        XCTAssertFalse(generation.accepts(originalRequest, from: serverB))

        let reauthenticatedRequest = generation.value
        generation.advance() // Same server, new device identity.
        XCTAssertFalse(generation.accepts(reauthenticatedRequest, from: serverB))
    }

    func testSnapshotDecodesAuthoritativeApprovalsAndRuns() throws {
        let data = Data(#"{"server_version":"srv-3","event_cursor":"19","projects":[],"tasks":[{"id":"task-1","title":"Review","goal":"Review changes","source":"user","status":"awaiting_user","version":4,"created_at":"2026-10-20T12:00:00Z","updated_at":"2026-10-20T12:01:00Z","evidence":[],"can_accept_result":true}],"approvals":[{"id":"approval-1","task_id":"task-1","status":"pending","prompt":"Approve deployment?","version":2,"created_at":"2026-10-20T12:00:00Z","requested_action_digest":"sha256:example"}],"devices":[],"runs":[{"id":"run-1","task_id":"task-1","attempt_id":"attempt-1","status":"verification_pending","desired_control":"stop","control_version":1,"started_at":"2026-10-20T12:00:00Z","evidence":[],"termination_confirmed":true}]}"#.utf8)
        let snapshot = try CyloJSON.decoder.decode(Snapshot.self, from: data)
        XCTAssertEqual(snapshot.eventCursor, "19")
        XCTAssertEqual(snapshot.tasks.first?.status, .awaitingUser)
        XCTAssertEqual(snapshot.approvals.first?.id, "approval-1")
        XCTAssertEqual(snapshot.runs.first?.desiredControl, "stop")
        XCTAssertEqual(snapshot.tasks.first?.canAcceptResult, true)
        XCTAssertEqual(snapshot.runs.first?.terminationConfirmed, true)
    }

    func testCapabilityInventoryKeepsDesiredAndObservedDeploymentStatesSeparate() throws {
        let data = Data(#"{"items":[{"deployment_id":"deploy-1","workspace_id":"workspace-1","service_id":"service-1","release_digest":"sha256:release","requested_operation":"start","desired_state":"running","observed_state":"pending","grant_id":"grant-1","config_ref":"","config_sha256":"","version":2,"created_by":"device-1","created_at":"2026-10-21T12:00:00Z","updated_at":"2026-10-21T12:01:00Z","latest_attempt_id":"attempt-1","latest_fence":3,"last_error_code":""}]}"#.utf8)
        let inventory = try CyloJSON.decoder.decode(CapabilityDeploymentList.self, from: data)
        XCTAssertEqual(inventory.items.first?.desiredState, "running")
        XCTAssertEqual(inventory.items.first?.observedState, "pending")
        XCTAssertEqual(inventory.items.first?.latestFence, 3)
    }

    func testCapabilityRevocationKeepsRunnerCleanupOutcomeSeparate() throws {
        func deployment(revokedAt: Any, revokedBy: Any, cleanup: Any = NSNull(), desired: String, observed: String) throws -> CapabilityDeployment {
            let item: [String: Any] = [
                "deployment_id": "deploy-1",
                "workspace_id": "workspace-1",
                "service_id": "service-1",
                "release_digest": "sha256:release",
                "requested_operation": "start",
                "desired_state": desired,
                "observed_state": observed,
                "grant_id": "grant-1",
                "config_ref": "",
                "config_sha256": "",
                "version": 8,
                "created_by": "owner-1",
                "created_at": "2026-10-21T12:00:00Z",
                "updated_at": "2026-10-21T12:01:00Z",
                "latest_attempt_id": "attempt-1",
                "latest_fence": 4,
                "last_error_code": "",
                "revoked_at": revokedAt,
                "revoked_by": revokedBy,
                "revocation_cleanup_state": cleanup
            ]
            let data = try JSONSerialization.data(withJSONObject: ["items": [item]])
            return try XCTUnwrap(CyloJSON.decoder.decode(CapabilityDeploymentList.self, from: data).items.first)
        }

        let active = try deployment(revokedAt: NSNull(), revokedBy: NSNull(), desired: "running", observed: "running")
        XCTAssertNil(active.revokedAt)
        XCTAssertNil(active.revokedBy)
        XCTAssertNil(active.revocationCleanupStatus)

        let cleanupPending = try deployment(revokedAt: "2026-10-21T12:02:00Z", revokedBy: "owner-1", cleanup: "pending", desired: "stopped", observed: "running")
        XCTAssertEqual(cleanupPending.revokedAt, "2026-10-21T12:02:00Z")
        XCTAssertEqual(cleanupPending.revokedBy, "owner-1")
        XCTAssertEqual(cleanupPending.revocationCleanupStatus, .pending(observedState: "running"))

        let cleanupUncertain = try deployment(revokedAt: "2026-10-21T12:02:00Z", revokedBy: "owner-1", cleanup: "uncertain", desired: "stopped", observed: "uncertain")
        XCTAssertEqual(cleanupUncertain.revocationCleanupStatus, .uncertain)

        let cleanupUnknown = try deployment(revokedAt: "2026-10-21T12:02:00Z", revokedBy: "owner-1", desired: "stopped", observed: "unknown")
        XCTAssertEqual(cleanupUnknown.revocationCleanupStatus, .unknown(observedState: "unknown"))
        let futureCleanupState = try deployment(revokedAt: "2026-10-21T12:02:00Z", revokedBy: "owner-1", cleanup: "future_state", desired: "stopped", observed: "stopped")
        XCTAssertEqual(futureCleanupState.revocationCleanupStatus, .unknown(observedState: "stopped"))

        let previouslyStopped = try deployment(revokedAt: "2026-10-21T12:02:00Z", revokedBy: "owner-1", desired: "stopped", observed: "stopped")
        XCTAssertEqual(previouslyStopped.revocationCleanupStatus, .unknown(observedState: "stopped"), "A stopped observation without cleanup proof may predate revocation.")

        let cleanupConfirmed = try deployment(revokedAt: "2026-10-21T12:02:00Z", revokedBy: "owner-1", cleanup: "confirmed", desired: "stopped", observed: "stopped")
        XCTAssertEqual(cleanupConfirmed.revocationCleanupStatus, .confirmed)
    }

    @MainActor
    func testRevocationReturnsFalseWhenConnectionChangesWhileCommandIsSuspended() async throws {
        let deployment = try makeRevocationDeployment(observed: "stopped")
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.setCapabilityDeploymentsForTesting([deployment])
        let submitGate = AsyncTestGate()
        var didRefresh = false
        model.capabilityRevocationSubmitOverrideForTesting = { version, payload in
            XCTAssertEqual(version, 8)
            XCTAssertEqual(payload, .object(["deployment_id": .string("deploy-1")]))
            await submitGate.pause()
            return true
        }
        model.capabilityRevocationRefreshOverrideForTesting = { didRefresh = true }

        let epoch = model.connectionEpoch
        let task = Task { await model.revokeCapabilityDeployment(deployment, expectedConnectionEpoch: epoch) }
        await submitGate.waitUntilStarted()
        model.serverAddress = ""
        await model.updateServerAddress()
        await submitGate.resume()

        let result = await task.value
        XCTAssertFalse(result)
        XCTAssertFalse(model.notice?.contains("Authorization revoked") ?? false)
        XCTAssertFalse(didRefresh)
    }

    @MainActor
    func testRevocationReturnsFalseWhenConnectionChangesDuringInventoryRefresh() async throws {
        let deployment = try makeRevocationDeployment(observed: "stopped")
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.setCapabilityDeploymentsForTesting([deployment])
        model.capabilityRevocationSubmitOverrideForTesting = { version, _ in
            XCTAssertEqual(version, 8)
            return true
        }
        let refreshGate = AsyncTestGate()
        model.capabilityRevocationRefreshOverrideForTesting = { await refreshGate.pause() }

        let epoch = model.connectionEpoch
        let task = Task { await model.revokeCapabilityDeployment(deployment, expectedConnectionEpoch: epoch) }
        await refreshGate.waitUntilStarted()
        model.serverAddress = ""
        await model.updateServerAddress()
        await refreshGate.resume()

        let result = await task.value
        XCTAssertFalse(result)
        XCTAssertFalse(model.notice?.contains("Authorization revoked") ?? false)
    }

    @MainActor
    func testRevocationVersionConflictRefreshesInventoryAndDoesNotReportSuccess() async throws {
        let deployment = try makeRevocationDeployment(observed: "running")
        let refreshedDeployment = try makeRevocationDeployment(observed: "running", version: 9)
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.setCapabilityDeploymentsForTesting([deployment])
        model.capabilityRevocationSubmitOverrideForTesting = { version, payload in
            XCTAssertEqual(version, 8)
            XCTAssertEqual(payload, .object(["deployment_id": .string("deploy-1")]))
            return false
        }
        model.capabilityRevocationRefreshOverrideForTesting = {
            model.setCapabilityDeploymentsForTesting([refreshedDeployment])
        }

        let result = await model.revokeCapabilityDeployment(deployment, expectedConnectionEpoch: model.connectionEpoch)

        XCTAssertFalse(result)
        XCTAssertEqual(model.capabilityDeployments.first?.version, 9)
        XCTAssertEqual(model.notice, "The deployment version changed. The latest capability inventory has been loaded; review it before trying again.")
    }

    @MainActor
    private func makeRevocationDeployment(observed: String, version: Int64 = 8) throws -> CapabilityDeployment {
        let data = Data(#"{"items":[{"deployment_id":"deploy-1","workspace_id":"workspace-1","service_id":"service-1","release_digest":"sha256:release","requested_operation":"start","desired_state":"stopped","observed_state":"\#(observed)","grant_id":"grant-1","config_ref":"","config_sha256":"","version":\#(version),"created_by":"owner-1","created_at":"2026-10-21T12:00:00Z","updated_at":"2026-10-21T12:01:00Z","latest_attempt_id":"attempt-1","latest_fence":4,"last_error_code":"","revoked_at":null,"revoked_by":null,"revocation_cleanup_state":null}]}"#.utf8)
        return try XCTUnwrap(CyloJSON.decoder.decode(CapabilityDeploymentList.self, from: data).items.first)
    }

    func testCapabilityRequestBindsExistingServiceToCurrentDeploymentVersion() throws {
        let data = Data(#"{"items":[{"deployment_id":"deploy-1","workspace_id":"workspace-1","service_id":"service-1","release_digest":"sha256:release","requested_operation":"start","desired_state":"running","observed_state":"pending","grant_id":"grant-1","config_ref":"","config_sha256":"","version":7,"created_by":"device-1","created_at":"2026-10-21T12:00:00Z","updated_at":"2026-10-21T12:01:00Z","latest_attempt_id":"attempt-1","latest_fence":3,"last_error_code":""}]}"#.utf8)
        let deployments = try CyloJSON.decoder.decode(CapabilityDeploymentList.self, from: data).items

        XCTAssertEqual(deployments.version(forServiceID: "service-1"), 7)
        XCTAssertNil(deployments.version(forServiceID: "new-service"))
    }

    func testSSEParserCombinesDataLinesAndPreservesDecimalCursor() throws {
        let payload = #"{"schema_version":1,"event_id":"ev-7","cursor":"9007199254740993","type":"task.updated","occurred_at":"2026-10-20T12:00:00Z","workspace_id":"workspace","aggregate_type":"task","aggregate_id":"task-1","aggregate_version":5,"payload":{"status":"active"}}"#
        var parser = SSEParser()
        XCTAssertNil(try parser.consume(line: "id: 9007199254740993"))
        XCTAssertNil(try parser.consume(line: "event: task.updated"))
        XCTAssertNil(try parser.consume(line: "data: \(payload)"))
        guard case let .event(event)? = try parser.consume(line: "") else {
            return XCTFail("Expected one event after the SSE record boundary")
        }
        XCTAssertEqual(event.cursor, "9007199254740993")
        XCTAssertEqual(event.schemaVersion, 1)
    }

    func testExpiredSSECursorRequestsSnapshotRepair() throws {
        var parser = SSEParser()
        XCTAssertNil(try parser.consume(line: "event: snapshot_required"))
        XCTAssertNil(try parser.consume(line: #"data: {"reason":"cursor_expired"}"#))
        guard case let .snapshotRequired(reason)? = try parser.consume(line: "") else {
            return XCTFail("Expected snapshot repair request")
        }
        XCTAssertEqual(reason, "cursor_expired")
    }

    func testSSEParserDropsUnterminatedRecordAtEOF() throws {
        let payload = #"{"schema_version":1,"event_id":"ev-8","cursor":"20","type":"task.updated","occurred_at":"2026-10-20T12:00:00Z","workspace_id":"workspace","aggregate_type":"task","aggregate_id":"task-1","aggregate_version":6,"payload":{}}"#
        var parser = SSEParser()
        _ = try parser.consume(line: "id: 20")
        _ = try parser.consume(line: "event: task.updated")
        _ = try parser.consume(line: "data: \(payload)")
        XCTAssertNil(try parser.finish(), "An SSE frame without its record boundary must not advance the event cursor.")
    }

    func testCommandReceiptDecodesOmittedResourceMetadata() throws {
        let data = Data(#"{"command_id":"cmd-1","idempotency_key":"key-1","workspace_id":"ws-1","kind":"task.append","status":"accepted","accepted_at":"2026-10-20T12:00:00Z","event_cursor":"21"}"#.utf8)
        let receipt = try CyloJSON.decoder.decode(CommandReceipt.self, from: data)
        XCTAssertEqual(receipt.idempotencyKey, "key-1")
        XCTAssertNil(receipt.resourceType)
        XCTAssertNil(receipt.resourceId)
    }

    func testJournalRejectsSameKeyBoundToDifferentPayload() async throws {
        let root = URL(fileURLWithPath: ProcessInfo.processInfo.environment["CYLO_BUILD_ROOT"] ?? "/Users/zhuwei/terminal/cylo-apple-build/test-data", isDirectory: true)
        let directory = root.appendingPathComponent("swift-journal-tests", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let file = directory.appendingPathComponent("\(UUID().uuidString).json")
        let journal = try CommandJournal(fileURL: file)
        let first = PendingCommand(idempotencyKey: "stable-key", kind: "task.append", expectedVersion: 2, payload: .object(["content": .string("first")]), createdAt: "now", workspaceID: "ws", expectedResourceType: "task", expectedResourceID: "t1")
        let changed = PendingCommand(idempotencyKey: "stable-key", kind: "task.append", expectedVersion: 2, payload: .object(["content": .string("second")]), createdAt: "later", workspaceID: "ws", expectedResourceType: "task", expectedResourceID: "t1")
        try await journal.append(first)
        do {
            try await journal.append(changed)
            XCTFail("Expected a local idempotency conflict")
        } catch let error as CyloError {
            guard case let .http(status, code, _, _) = error else { return XCTFail("Unexpected error: \(error)") }
            XCTAssertEqual(status, 409)
            XCTAssertEqual(code, "local_idempotency_conflict")
        }
        try? FileManager.default.removeItem(at: file)
    }
}
