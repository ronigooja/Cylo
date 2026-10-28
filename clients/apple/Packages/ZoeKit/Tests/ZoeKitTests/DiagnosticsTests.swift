import XCTest
@testable import ZoeKit

final class DiagnosticsTests: XCTestCase {
    func testDiagnosticEventHashesInstallationAndQueueBoundsOldestEntries() async throws {
        let server = URL(string: "https://zoe-\(UUID().uuidString).example")!
        let installation = "diagnostics-test-installation"
        let queue = try XCTUnwrap(EncryptedDiagnosticQueue(serverURL: server, installationID: installation))
        for index in 0..<101 {
            let event = DiagnosticEvent(clientRequestID: "r-\(index)", phase: "config_request", requestPath: "/v1/voice/config", installationID: installation)
            await queue.append(event)
        }
        let events = await queue.take(limit: 200)
        XCTAssertEqual(events.count, 100)
        XCTAssertEqual(events.first?.clientRequestID, "r-1")
        XCTAssertEqual(events.first?.deviceIDHash.count, 64)
        XCTAssertFalse(events.first?.errorSummary.contains("Authorization") == true)
    }
}
