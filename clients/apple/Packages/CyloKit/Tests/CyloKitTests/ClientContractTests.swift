import XCTest
@testable import CyloKit

final class ClientContractTests: XCTestCase {
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
