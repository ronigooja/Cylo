import XCTest
@testable import CyloKit
@testable import CyloUI
import Foundation
import WebRTC

private final class ExportURLProtocol: URLProtocol {
    static var responseHandler: ((URLRequest) -> (Int, [String: String], Data))?
    static var failureHandler: ((URLRequest) -> Error?)?
    static var capturedRequest: URLRequest?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        Self.capturedRequest = request
        if let error = Self.failureHandler?(request) {
            client?.urlProtocol(self, didFailWithError: error)
            return
        }
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
        ExportURLProtocol.failureHandler = nil
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
    func testSnapshotAuthorizationFailureClearsMemoryAndPreparedExport() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.setMemoryClaimsForTesting([memoryClaim(id: "memory-old", content: "stale private content")])
        let fileURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try Data("export containing stale memory".utf8).write(to: fileURL)
        model.setExportArtifactForTesting(ScopedExportArtifact(fileURL: fileURL, filename: "cylo-export-old.json", scope: "personal", snapshotCursor: "5"))
        model.snapshotFetchOverrideForTesting = {
            throw CyloError.http(status: 403, code: "forbidden", message: "snapshot denied", details: nil)
        }

        do { try await model.refreshSnapshotForTesting(); XCTFail("forbidden snapshot accepted") }
        catch {
            if let failure = error as? CyloError, case let .http(status, _, _, _) = failure, status == 403 {} else {
                XCTFail("unexpected error: \(error)")
            }
        }

        XCTAssertTrue(model.memoryClaims.isEmpty)
        XCTAssertNil(model.exportArtifact)
        XCTAssertFalse(FileManager.default.fileExists(atPath: fileURL.path))
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
    func testDelayedOldConnectionMemoryEventCannotInvalidateNewConnectionData() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let oldEpoch = model.connectionEpoch
        model.serverAddress = "invalid-url"
        await model.updateServerAddress()
        let fileURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try Data("new connection export".utf8).write(to: fileURL)
        model.setExportArtifactForTesting(ScopedExportArtifact(fileURL: fileURL, filename: "cylo-export-new.json", scope: "personal", snapshotCursor: "1"))
        model.setMemoryClaimsForTesting([memoryClaim(id: "memory-new", content: "new connection claim")])

        await model.handleEventForTesting(EventEnvelope(schemaVersion: 1, eventId: "old-event", cursor: "2", type: "memory.deleted", occurredAt: "2026-10-21T12:00:00Z", workspaceId: "workspace-old", subjectId: "subject-old", aggregateType: "memory", aggregateId: "memory-old", aggregateVersion: 2, payload: .object([:])), expectedConnectionEpoch: oldEpoch)

        XCTAssertEqual(model.memoryClaims.map(\.id), ["memory-new"])
        XCTAssertEqual(model.exportArtifact?.filename, "cylo-export-new.json")
        XCTAssertTrue(FileManager.default.fileExists(atPath: fileURL.path))
        model.cancelExport()
    }

    @MainActor
    func testRecoveredMemoryDeleteReceiptReplaysJournalAndInvalidatesPreparedExport() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.setMemoryClaimsForTesting([memoryClaim(id: "memory-before-replay", content: "before replay")])
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let artifactURL = directory.appendingPathComponent("private-export.json")
        try Data("export containing deleted memory".utf8).write(to: artifactURL)
        model.setExportArtifactForTesting(ScopedExportArtifact(fileURL: artifactURL, filename: "cylo-export-private.json", scope: "personal", snapshotCursor: "7"))

        let key = "recovery-key-\(UUID().uuidString.lowercased())"
        let journal = try CommandJournal(fileURL: directory.appendingPathComponent("commands.json"))
        try await journal.append(PendingCommand(idempotencyKey: key, kind: "memory.delete", expectedVersion: 2, payload: .object(["memory_id": .string("memory-1")]), createdAt: "2026-10-21T12:00:00Z", workspaceID: "workspace-1", expectedResourceType: "memory", expectedResourceID: "memory-1"))
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        var observedReceiptLookup = false
        ExportURLProtocol.responseHandler = { request in
            if request.url?.path == "/v1/commands", request.httpMethod == "GET", URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems?.contains(where: { $0.name == "key" && $0.value == key }) == true {
                observedReceiptLookup = true
                return (200, ["Content-Type": "application/json"], Data("{\"command_id\":\"cmd-recovered\",\"idempotency_key\":\"\(key)\",\"workspace_id\":\"workspace-1\",\"kind\":\"memory.delete\",\"status\":\"accepted\",\"resource_type\":\"memory\",\"resource_id\":\"memory-1\",\"resource_version\":3,\"accepted_at\":\"2026-10-21T12:00:00Z\",\"event_cursor\":\"8\"}".utf8))
            }
            return (503, ["Content-Type": "application/json"], Data())
        }
        var observedQueries: [String] = []
        model.memorySearchOverrideForTesting = { query in
            observedQueries.append(query)
            return MemorySearchResult(items: [], missing: [], conflicts: [])
        }

        await model.recoverPendingCommandsForTesting(api: api, journal: journal)

        let remainingCommands = try await journal.all()
        XCTAssertTrue(observedReceiptLookup, "recovery must resolve the persisted idempotency key through the receipt endpoint")
        XCTAssertTrue(remainingCommands.isEmpty, "an accepted recovered receipt must remove its journal row")
        XCTAssertEqual(observedQueries, [""])
        XCTAssertTrue(model.memoryClaims.isEmpty)
        XCTAssertNil(model.exportArtifact, "a recovered memory deletion must invalidate a prepared private export")
        XCTAssertFalse(FileManager.default.fileExists(atPath: artifactURL.path), "the invalidated private export file must be removed")
    }

    @MainActor
    func testRecoveredMemoryReceiptFromOldGenerationCannotInvalidateNewExport() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let artifactURL = directory.appendingPathComponent("new-private-export.json")
        try Data("new connection export".utf8).write(to: artifactURL)
        model.setExportArtifactForTesting(ScopedExportArtifact(fileURL: artifactURL, filename: "cylo-export-new.json", scope: "personal", snapshotCursor: "9"))

        let key = "old-recovery-key-\(UUID().uuidString.lowercased())"
        let journal = try CommandJournal(fileURL: directory.appendingPathComponent("commands.json"))
        try await journal.append(PendingCommand(idempotencyKey: key, kind: "memory.delete", expectedVersion: 2, payload: .object(["memory_id": .string("memory-1")]), createdAt: "2026-10-21T12:00:00Z", workspaceID: "workspace-1", expectedResourceType: "memory", expectedResourceID: "memory-1"))
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        ExportURLProtocol.responseHandler = { request in
            guard request.url?.path == "/v1/commands" else { return (503, [:], Data()) }
            return (200, ["Content-Type": "application/json"], Data("{\"command_id\":\"cmd-old\",\"idempotency_key\":\"\(key)\",\"workspace_id\":\"workspace-1\",\"kind\":\"memory.delete\",\"status\":\"accepted\",\"resource_type\":\"memory\",\"resource_id\":\"memory-1\",\"resource_version\":3,\"accepted_at\":\"2026-10-21T12:00:00Z\",\"event_cursor\":\"8\"}".utf8))
        }

        await model.recoverPendingCommandsForTesting(api: api, journal: journal, expectedGeneration: model.connectionEpoch &- 1)

        XCTAssertEqual(model.exportArtifact?.filename, "cylo-export-new.json")
        XCTAssertTrue(FileManager.default.fileExists(atPath: artifactURL.path))
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

    @MainActor
    func testMemoryQueryFromPreviousConnectionCannotRepopulateClearedClaims() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.setMemoryClaimsForTesting([memoryClaim(id: "memory-before", content: "before")])
        let gate = AsyncTestGate()
        model.memorySearchOverrideForTesting = { _ in
            await gate.pause()
            return MemorySearchResult(items: [self.memoryClaim(id: "memory-old-origin", content: "old origin result")], missing: [], conflicts: [])
        }

        let oldSearch = Task { await model.refreshMemory() }
        await gate.waitUntilStarted()
        model.serverAddress = "invalid-url"
        await model.updateServerAddress()
        await gate.resume()
        await oldSearch.value

        XCTAssertTrue(model.memoryClaims.isEmpty)
    }

    @MainActor
    func testMemoryEventFencesAndDeletesInFlightPreparedExport() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let gate = AsyncTestGate()
        var producedURL: URL?
        model.exportDownloadOverrideForTesting = { scope in
            await gate.pause()
            let url = directory.appendingPathComponent("stale-export.json")
            try Data("stale export".utf8).write(to: url)
            producedURL = url
            return ScopedExportArtifact(fileURL: url, filename: "cylo-export-stale.json", scope: scope, snapshotCursor: "5")
        }
        model.memorySearchOverrideForTesting = { _ in MemorySearchResult(items: [], missing: [], conflicts: []) }

        let preparing = Task { await model.prepareExport(scope: "personal", expectedConnectionEpoch: model.connectionEpoch) }
        await gate.waitUntilStarted()
        await model.handleEventForTesting(EventEnvelope(schemaVersion: 1, eventId: "event-delete", cursor: "6", type: "memory.deleted", occurredAt: "2026-10-21T12:00:00Z", workspaceId: "workspace-1", subjectId: "subject-1", aggregateType: "memory", aggregateId: "memory-1", aggregateVersion: 2, payload: .object([:])))
        await gate.resume()
        await preparing.value

        XCTAssertNil(model.exportArtifact)
        XCTAssertFalse(FileManager.default.fileExists(atPath: try XCTUnwrap(producedURL).path))
        XCTAssertTrue(model.exportStatus?.contains("Server memory changed") == true)
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

    func testUncertainSubmissionChecksReceiptBeforeRetryAndDoesNotPostTwice() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try CommandJournal(fileURL: directory.appendingPathComponent("commands.json"))
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        let coordinator = CommandCoordinator(api: api, journal: journal)
        let key = "stable-retry-\(UUID().uuidString.lowercased())"
        var postCount = 0
        var receiptCount = 0
        var acceptedReceipt = Data()
        ExportURLProtocol.failureHandler = { request in
            guard request.url?.path == "/v1/commands", request.httpMethod == "POST" else { return nil }
            postCount += 1
            let submittedKey = request.value(forHTTPHeaderField: "Idempotency-Key") ?? ""
            acceptedReceipt = Self.commandReceipt(key: submittedKey, kind: "task.create", workspaceID: "workspace-1", resourceID: "task-created")
            return URLError(.timedOut)
        }
        ExportURLProtocol.responseHandler = { request in
            if request.url?.path == "/v1/commands", request.httpMethod == "GET" {
                receiptCount += 1
                return (200, ["Content-Type": "application/json"], acceptedReceipt)
            }
            return (503, ["Content-Type": "application/json"], Data())
        }

        do {
            _ = try await coordinator.submit(kind: "task.create", payload: .object(["title": .string("Work")]), workspaceID: "workspace-1", idempotencyKey: key)
            XCTFail("The first response should be lost after the server accepts the command")
        } catch { }
        let pendingAfterTimeout = try await journal.all()
        XCTAssertEqual(pendingAfterTimeout.map(\.idempotencyKey), [key])

        let recovered = try await coordinator.retryPending(idempotencyKey: key, currentWorkspaceID: "workspace-1")

        XCTAssertTrue(recovered.recovered)
        XCTAssertEqual(recovered.receipt.idempotencyKey, key)
        XCTAssertEqual(postCount, 1, "a receipt found after a lost response must prevent another command POST")
        XCTAssertEqual(receiptCount, 1)
        let remaining = try await journal.all()
        XCTAssertTrue(remaining.isEmpty)
    }

    func testPendingRetryPostsOriginalPayloadAndKeyOnlyAfterDefinitiveReceipt404() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try CommandJournal(fileURL: directory.appendingPathComponent("commands.json"))
        let key = "definitive-404-\(UUID().uuidString.lowercased())"
        let payload: JSONValue = .object(["title": .string("Original title"), "goal": .string("Original payload")])
        try await journal.append(PendingCommand(idempotencyKey: key, kind: "task.create", expectedVersion: nil, payload: payload, createdAt: "2026-10-22T12:00:00Z", workspaceID: "workspace-1", expectedResourceType: "task", expectedResourceID: nil))
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        let coordinator = CommandCoordinator(api: api, journal: journal)
        var receiptCount = 0
        var postCount = 0
        var postKey: String?
        var postBody: Data?
        ExportURLProtocol.responseHandler = { request in
            if request.url?.path == "/v1/commands", request.httpMethod == "GET" {
                receiptCount += 1
                return (404, ["Content-Type": "application/json"], Data())
            }
            if request.url?.path == "/v1/commands", request.httpMethod == "POST" {
                postCount += 1
                postKey = request.value(forHTTPHeaderField: "Idempotency-Key")
                postBody = Self.requestBody(request)
                return (200, ["Content-Type": "application/json"], Self.commandReceipt(key: key, kind: "task.create", workspaceID: "workspace-1", resourceID: "task-created"))
            }
            return (503, ["Content-Type": "application/json"], Data())
        }

        let outcome = try await coordinator.retryPending(idempotencyKey: key, currentWorkspaceID: "workspace-1")

        XCTAssertEqual(receiptCount, 1, "receipt lookup must precede the safe retry")
        XCTAssertEqual(postCount, 1)
        XCTAssertEqual(postKey, key)
        let postedEnvelope = try XCTUnwrap(postBody.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] })
        XCTAssertEqual(postedEnvelope["kind"] as? String, "task.create")
        XCTAssertEqual(postedEnvelope["payload"] as? [String: String], ["title": "Original title", "goal": "Original payload"])
        XCTAssertEqual(outcome.receipt.idempotencyKey, key)
        let remaining = try await journal.all()
        XCTAssertTrue(remaining.isEmpty)
    }

    func testPendingRetryRefusesIncompatibleWorkspaceWithoutNetworkRequest() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try CommandJournal(fileURL: directory.appendingPathComponent("commands.json"))
        let key = "workspace-guard-\(UUID().uuidString.lowercased())"
        try await journal.append(PendingCommand(idempotencyKey: key, kind: "task.create", expectedVersion: nil, payload: .object([:]), createdAt: "now", workspaceID: "old-workspace", expectedResourceType: "task", expectedResourceID: nil))
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        let coordinator = CommandCoordinator(api: api, journal: journal)
        ExportURLProtocol.responseHandler = { _ in XCTFail("workspace mismatch must not make a network request"); return (500, [:], Data()) }

        do {
            _ = try await coordinator.retryPending(idempotencyKey: key, currentWorkspaceID: "new-workspace")
            XCTFail("Expected a workspace guard")
        } catch let error as CyloError {
            guard case .pendingCommandWorkspaceMismatch = error else { return XCTFail("Unexpected error: \(error)") }
        }
        let remaining = try await journal.all()
        XCTAssertEqual(remaining.map(\.idempotencyKey), [key])
    }

    @MainActor
    func testUnknownWorkspaceNeverPostsLegacyPendingCommand() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try CommandJournal(fileURL: directory.appendingPathComponent("commands.json"))
        let key = "legacy-unbound-\(UUID().uuidString.lowercased())"
        try await journal.append(PendingCommand(idempotencyKey: key, kind: "task.create", expectedVersion: nil, payload: .object(["title": .string("Unbound")]), createdAt: "now", workspaceID: nil, expectedResourceType: "task", expectedResourceID: nil))
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        var requestCount = 0
        ExportURLProtocol.responseHandler = { _ in requestCount += 1; return (404, [:], Data()) }
        let coordinator = CommandCoordinator(api: api, journal: journal)
        do {
            _ = try await coordinator.retryPending(idempotencyKey: key, currentWorkspaceID: nil)
            XCTFail("An unknown workspace cannot authorize a pending command")
        } catch let error as CyloError {
            guard case .pendingCommandWorkspaceMismatch = error else { return XCTFail("Unexpected error: \(error)") }
        }

        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        await model.recoverPendingCommandsForTesting(api: api, journal: journal, workspaceID: nil)

        XCTAssertEqual(requestCount, 0)
        let remaining = try await journal.all()
        XCTAssertEqual(remaining.map(\.idempotencyKey), [key])
        XCTAssertEqual(model.pendingCommandSummaries.first?.state, .workspaceUnavailable)
    }

    func testPendingReceiptLookupNetworkFailureDoesNotResubmitOrDropJournalRow() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try CommandJournal(fileURL: directory.appendingPathComponent("commands.json"))
        let key = "receipt-timeout-\(UUID().uuidString.lowercased())"
        try await journal.append(PendingCommand(idempotencyKey: key, kind: "task.create", expectedVersion: nil, payload: .object(["title": .string("Work")]), createdAt: "now", workspaceID: "workspace-1", expectedResourceType: "task", expectedResourceID: nil))
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        let coordinator = CommandCoordinator(api: api, journal: journal)
        var postCount = 0
        ExportURLProtocol.failureHandler = { request in
            if request.url?.path == "/v1/commands", request.httpMethod == "GET" { return URLError(.timedOut) }
            if request.url?.path == "/v1/commands", request.httpMethod == "POST" { postCount += 1 }
            return nil
        }
        ExportURLProtocol.responseHandler = { _ in XCTFail("a failed receipt lookup must not fall through to POST"); return (500, [:], Data()) }

        do {
            _ = try await coordinator.retryPending(idempotencyKey: key, currentWorkspaceID: "workspace-1")
            XCTFail("Expected receipt lookup to remain uncertain")
        } catch { }

        XCTAssertEqual(postCount, 0)
        let remaining = try await journal.all()
        XCTAssertEqual(remaining.map(\.idempotencyKey), [key])
    }

    @MainActor
    func testVersionConflictMarksPendingActionForReviewAndRefreshesWithoutRewritingIt() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let journal = try CommandJournal(fileURL: directory.appendingPathComponent("commands.json"))
        let key = "version-conflict-\(UUID().uuidString.lowercased())"
        let payload: JSONValue = .object(["task_id": .string("task-1"), "content": .string("original content")])
        try await journal.append(PendingCommand(idempotencyKey: key, kind: "task.append", expectedVersion: 4, payload: payload, createdAt: "now", workspaceID: "workspace-1", expectedResourceType: "task", expectedResourceID: "task-1"))
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        var postedVersion: Int64?
        var postedEnvelope: [String: Any]?
        var snapshotRefreshes = 0
        ExportURLProtocol.responseHandler = { request in
            if request.httpMethod == "GET" {
                return (404, ["Content-Type": "application/json"], Data())
            }
            if request.httpMethod == "POST" {
                if let body = Self.requestBody(request), let envelope = try? JSONSerialization.jsonObject(with: body) as? [String: Any] {
                    postedEnvelope = envelope
                    postedVersion = (envelope["expected_version"] as? NSNumber)?.int64Value
                }
                return (409, ["Content-Type": "application/json"], Data(#"{"code":"version_conflict","message":"version changed","details":{"current_version":5}}"#.utf8))
            }
            return (500, [:], Data())
        }
        let oldSnapshot = Snapshot(serverVersion: "1", eventCursor: "1", projects: [], tasks: [], approvals: [], devices: [Device(id: "device-1", workspaceId: "workspace-1", name: "Test", status: "approved", version: 1, createdAt: "now", approvedAt: nil, lastSeenAt: nil)], runs: [])
        let freshSnapshot = Snapshot(serverVersion: "2", eventCursor: "2", projects: [], tasks: [], approvals: [], devices: oldSnapshot.devices, runs: [])
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        await model.preparePendingCheckForTesting(api: api, journal: journal, snapshot: oldSnapshot)
        model.snapshotFetchOverrideForTesting = { snapshotRefreshes += 1; return freshSnapshot }
        let summary = try XCTUnwrap(model.pendingCommandSummaries.first)

        await model.checkPendingCommand(summary)

        XCTAssertEqual(postedVersion, 4)
        XCTAssertEqual(postedEnvelope?["payload"] as? [String: String], ["task_id": "task-1", "content": "original content"])
        XCTAssertEqual(snapshotRefreshes, 1)
        XCTAssertEqual(model.snapshot?.serverVersion, "2")
        XCTAssertEqual(model.pendingCommandSummaries.first?.state, .needsReview)
        let remaining = try await journal.all()
        XCTAssertTrue(remaining.isEmpty, "a conflict is final for the original immutable command")
    }

    @MainActor
    func testDelayedOldJournalReadCannotReplaceSummariesAfterConnectionChanges() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let oldJournal = try CommandJournal(fileURL: directory.appendingPathComponent("old-commands.json"))
        let newJournal = try CommandJournal(fileURL: directory.appendingPathComponent("new-commands.json"))
        let oldCommand = PendingCommand(idempotencyKey: "old-connection", kind: "task.create", expectedVersion: nil, payload: .object(["title": .string("Old task")]), createdAt: "now", workspaceID: "workspace-1", expectedResourceType: "task", expectedResourceID: nil)
        try await oldJournal.append(oldCommand)
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        await model.preparePendingCheckForTesting(api: api, journal: oldJournal, snapshot: Snapshot(serverVersion: "1", eventCursor: "1", projects: [], tasks: [], approvals: [], devices: [], runs: []))
        let readStarted = expectation(description: "old journal read started")
        var resumeRead: CheckedContinuation<[PendingCommand], Never>!
        model.pendingCommandReadOverrideForTesting = {
            await withCheckedContinuation { continuation in
                resumeRead = continuation
                readStarted.fulfill()
            }
        }

        let staleReload = Task { await model.reloadPendingCommandSummariesForTesting() }
        await fulfillment(of: [readStarted], timeout: 2)
        model.replacePendingCheckConnectionForTesting(api: api, journal: newJournal)
        resumeRead.resume(returning: [oldCommand])
        await staleReload.value

        XCTAssertTrue(model.pendingCommandSummaries.isEmpty)
    }

    @MainActor
    func testAppendForTaskMissingFromLatestSnapshotDoesNotCreateAnotherTask() async throws {
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        let task = TaskRecord(id: "task-removed", projectId: nil, title: "Existing task", goal: "Existing goal", source: "user", profileId: nil, status: .active, version: 2, createdAt: "now", updatedAt: "now", acceptedAt: nil, startedAt: nil, finishedAt: nil, result: nil, error: nil, evidence: [], canAcceptResult: false)
        let snapshot = Snapshot(serverVersion: "1", eventCursor: "1", projects: [], tasks: [task], approvals: [], devices: [], runs: [])
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        await model.preparePendingCheckForTesting(api: api, journal: try CommandJournal(fileURL: FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)), snapshot: snapshot)
        model.setSnapshotForTesting(Snapshot(serverVersion: "2", eventCursor: "2", projects: [], tasks: [], approvals: [], devices: [], runs: []))
        var requestCount = 0
        ExportURLProtocol.responseHandler = { _ in requestCount += 1; return (500, [:], Data()) }

        await model.append(to: task, content: "Continue the existing task")

        XCTAssertEqual(requestCount, 0)
        XCTAssertTrue(model.notice?.contains("finished") == true || model.notice?.contains("unavailable") == true)
    }

    @MainActor
    func testRecoveredMemoryReceiptInvalidatesExportBeforeLaterJournalRowFails() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let exportURL = directory.appendingPathComponent("private-export.json")
        try Data("stale memory export".utf8).write(to: exportURL)
        model.setExportArtifactForTesting(ScopedExportArtifact(fileURL: exportURL, filename: "cylo-export-old.json", scope: "personal", snapshotCursor: "4"))
        var memoryRefreshCount = 0
        model.memorySearchOverrideForTesting = { _ in
            memoryRefreshCount += 1
            return MemorySearchResult(items: [], missing: [], conflicts: [])
        }
        let deleteKey = "accepted-delete-\(UUID().uuidString.lowercased())"
        let laterKey = "later-command-\(UUID().uuidString.lowercased())"
        let journal = try CommandJournal(fileURL: directory.appendingPathComponent("commands.json"))
        try await journal.append(PendingCommand(idempotencyKey: deleteKey, kind: "memory.delete", expectedVersion: 1, payload: .object(["memory_id": .string("memory-1")]), createdAt: "now", workspaceID: "workspace-1", expectedResourceType: "memory", expectedResourceID: "memory-1"))
        try await journal.append(PendingCommand(idempotencyKey: laterKey, kind: "task.create", expectedVersion: nil, payload: .object(["title": .string("Later")]), createdAt: "later", workspaceID: "workspace-1", expectedResourceType: "task", expectedResourceID: nil))
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        ExportURLProtocol.responseHandler = { request in
            guard request.url?.path == "/v1/commands", request.httpMethod == "GET" else { return (503, [:], Data()) }
            let key = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems?.first(where: { $0.name == "key" })?.value
            if key == deleteKey {
                return (200, ["Content-Type": "application/json"], Self.commandReceipt(key: deleteKey, kind: "memory.delete", workspaceID: "workspace-1", resourceID: "memory-1", resourceType: "memory"))
            }
            return (503, ["Content-Type": "application/json"], Data())
        }

        await model.recoverPendingCommandsForTesting(api: api, journal: journal)

        let remaining = try await journal.all()
        XCTAssertEqual(remaining.map(\.idempotencyKey), [laterKey], "the accepted row is removed while the unresolved row remains durable")
        XCTAssertEqual(memoryRefreshCount, 1, "accepted memory receipts reconcile immediately before replay moves to the next row")
        XCTAssertNil(model.exportArtifact)
        XCTAssertFalse(FileManager.default.fileExists(atPath: exportURL.path))
    }

    func testVoiceMediaEpochFenceRejectsDelayedAndDuplicateLossEvents() throws {
        var fence = VoiceMediaEpochFence()
        let first = fence.beginAttempt()
        XCTAssertTrue(fence.accepts(first))
        XCTAssertTrue(fence.stop(first), "the first terminal media event claims teardown")
        XCTAssertFalse(fence.accepts(first), "callbacks after teardown cannot revive the stopped attempt")
        XCTAssertFalse(fence.stop(first), "a duplicate connection/audio callback cannot repeat teardown or release")

        let second = fence.beginAttempt()
        XCTAssertGreaterThan(second, first)
        XCTAssertFalse(fence.stop(first), "a delayed event from the old connection cannot stop the new attempt")
        XCTAssertTrue(fence.accepts(second))
    }

    @MainActor
    func testVoiceAudioInterruptionDuringSetupFencesLateHandshakeAndDuplicateEvents() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        var localStopCount = 0
        model.voiceLocalMediaStopObserverForTesting = { localStopCount += 1 }
        let startingEpoch = model.beginVoiceAttemptForTesting()

        await model.handleVoiceMediaLossForTesting(.audio(.interrupted), mediaEpoch: startingEpoch)
        await model.handleVoiceMediaLossForTesting(.audio(.interrupted), mediaEpoch: startingEpoch)

        XCTAssertFalse(model.canContinueVoiceAttemptForTesting(startingEpoch), "a late SDP/connected response cannot reactivate the interrupted attempt")
        XCTAssertEqual(localStopCount, 1, "the startup interruption claims local teardown once")
        XCTAssertFalse(model.canEndVoiceSession, "the event does not invent an active server session")
        guard case .interrupted = model.voiceStatus else { return XCTFail("expected an interrupted voice setup") }

        let nextEpoch = model.beginVoiceAttemptForTesting()
        XCTAssertTrue(model.canContinueVoiceAttemptForTesting(nextEpoch))
        XCTAssertFalse(model.canContinueVoiceAttemptForTesting(startingEpoch), "old callbacks remain fenced after a later attempt begins")
    }

    @MainActor
    func testVoiceMediaLossStopsHeartbeatAndPreservesTaskWhileHangupQuarantinesPool() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let task = TaskRecord(id: "task-live", projectId: nil, title: "Keep this task", goal: "Task state survives voice media loss", source: "user", profileId: nil, status: .active, version: 3, createdAt: "now", updatedAt: "now", acceptedAt: nil, startedAt: nil, finishedAt: nil, result: nil, error: nil, evidence: [], canAcceptResult: false)
        model.setSnapshotForTesting(Snapshot(serverVersion: "1", eventCursor: "5", projects: [], tasks: [task], approvals: [], devices: [], runs: []))
        var hangupCount = 0
        var localStopCount = 0
        ExportURLProtocol.responseHandler = { request in
            if request.url?.path == "/v1/voice/sessions/session-1/hangup" {
                hangupCount += 1
                return (202, ["Content-Type": "application/json"], Data(#"{"ok":true,"state":"ended","upstream_termination":"unverified","pool_blocked":true}"#.utf8))
            }
            if request.url?.path == "/v1/voice/pool/status" {
                return (200, ["Content-Type": "application/json"], Data(#"{"blocked":true,"reason_code":"upstream_termination_unverified","updated_at":"2026-10-22T12:00:00Z","account_lease_identity":false,"account_scoped_termination":false,"trusted_termination_evidence":false}"#.utf8))
            }
            return (404, [:], Data())
        }
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        let session = VoiceSessionResponse(voiceSessionId: "session-1", generation: 4, state: "active", answerSdp: "")
        let epoch = model.installVoiceSessionForTesting(session, api: api)
        let heartbeatStarted = expectation(description: "heartbeat task starts")
        let heartbeatCancelled = expectation(description: "heartbeat task is canceled on media loss")
        model.installVoiceHeartbeatForTesting(Task {
            heartbeatStarted.fulfill()
            await withTaskCancellationHandler {
                try? await Task.sleep(for: .seconds(30))
            } onCancel: {
                heartbeatCancelled.fulfill()
            }
        })
        model.voiceLocalMediaStopObserverForTesting = { localStopCount += 1 }
        await fulfillment(of: [heartbeatStarted], timeout: 1)

        await model.handleVoiceMediaLossForTesting(.connection(.disconnected), mediaEpoch: epoch)
        await model.handleVoiceMediaLossForTesting(.connection(.failed), mediaEpoch: epoch)
        await fulfillment(of: [heartbeatCancelled], timeout: 1)

        XCTAssertFalse(model.voiceHeartbeatActiveForTesting)
        XCTAssertEqual(localStopCount, 1, "local media teardown is claimed once")
        XCTAssertEqual(hangupCount, 1, "duplicate terminal signals issue at most one hangup")
        XCTAssertEqual(model.tasks.map(\.id), ["task-live"], "media loss does not cancel or rewrite the authoritative task")
        guard case let .interrupted(reason) = model.voiceStatus else { return XCTFail("expected an interrupted voice status") }
        XCTAssertTrue(reason.contains("quarantined"))
        XCTAssertTrue(reason.contains("unverified"))
        XCTAssertTrue(model.voicePoolStatus?.blocked == true)
    }

    @MainActor
    func testDelayedOldHeartbeatFailureCannotTearDownNewVoiceSession() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        let old = VoiceSessionResponse(voiceSessionId: "session-old", generation: 2, state: "active", answerSdp: "")
        let oldEpoch = model.installVoiceSessionForTesting(old, api: api)
        let current = VoiceSessionResponse(voiceSessionId: "session-current", generation: 3, state: "active", answerSdp: "")
        let currentEpoch = model.installVoiceSessionForTesting(current, api: api)
        var localStopCount = 0
        model.voiceLocalMediaStopObserverForTesting = { localStopCount += 1 }

        await model.handleVoiceHeartbeatFailureForTesting(sessionID: old.voiceSessionId, api: api, mediaEpoch: oldEpoch, generation: 0, message: "late timeout")

        XCTAssertEqual(currentEpoch, oldEpoch + 1)
        XCTAssertTrue(model.canEndVoiceSession)
        XCTAssertEqual(localStopCount, 0)
        guard case let .active(sessionID, generation) = model.voiceStatus else { return XCTFail("the newer voice session must remain active") }
        XCTAssertEqual(sessionID, current.voiceSessionId)
        XCTAssertEqual(generation, current.generation)
    }

    @MainActor
    func testConcurrentRetryCannotSendSecondVoiceHangup() async throws {
        let model = CyloAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let api = try APIClient(serverURL: URL(string: "https://cylo.example")!, credentials: StaticExportCredential(), session: exportSession())
        let session = VoiceSessionResponse(voiceSessionId: "session-retry", generation: 5, state: "active", answerSdp: "")
        _ = model.installVoiceSessionForTesting(session, api: api)
        let gate = AsyncTestGate()
        var hangupCount = 0
        model.voiceHangupOverrideForTesting = { _ in
            hangupCount += 1
            await gate.pause()
            return VoiceOperationResponse(ok: true, state: "ended", upstreamTermination: "verified", poolBlocked: false)
        }

        let first = Task { await model.endVoiceSession() }
        await gate.waitUntilStarted()
        await model.endVoiceSession()
        XCTAssertEqual(hangupCount, 1, "a retry while the first hangup is pending cannot issue a second POST")
        await gate.resume()
        await first.value
        XCTAssertEqual(hangupCount, 1)
        XCTAssertFalse(model.canEndVoiceSession)
    }

    private static func commandReceipt(key: String, kind: String, workspaceID: String, resourceID: String, resourceType: String = "task") -> Data {
        Data("{\"command_id\":\"command-1\",\"idempotency_key\":\"\(key)\",\"workspace_id\":\"\(workspaceID)\",\"kind\":\"\(kind)\",\"status\":\"accepted\",\"resource_type\":\"\(resourceType)\",\"resource_id\":\"\(resourceID)\",\"resource_version\":1,\"accepted_at\":\"2026-10-22T12:00:00Z\",\"event_cursor\":\"8\"}".utf8)
    }

    private static func requestBody(_ request: URLRequest) -> Data? {
        if let body = request.httpBody { return body }
        guard let stream = request.httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            data.append(buffer, count: count)
        }
        return data.isEmpty ? nil : data
    }
}
