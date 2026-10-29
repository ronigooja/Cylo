import XCTest
import Foundation
@testable import ZoeKit
@testable import ZoeUI

private struct IndependentVoiceCredential: BearerTokenProviding {
    func bearerToken() throws -> String { "synthetic-independent-voice-token" }
}

final class VoiceLifecycleIndependentTests: XCTestCase {
    @MainActor
    func testContinuousVoicePreferenceDefaultsOffAndPersists() throws {
        let suite = UserDefaults(suiteName: UUID().uuidString)!
        let first = ZoeAppModel(defaults: suite)
        XCTAssertFalse(first.continuousVoiceEnabled)
        first.setContinuousVoiceEnabled(true)
        let second = ZoeAppModel(defaults: suite)
        XCTAssertTrue(second.continuousVoiceEnabled)
    }

    @MainActor
    func testReconnectAndStoppedStatusesAreRepresentable() {
        let model = ZoeAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        model.setContinuousVoiceEnabled(false)
        XCTAssertEqual(model.voiceStatus, .idle)
        XCTAssertNotEqual(model.voiceStatus, .reconnecting(reason: "temporary"))
        XCTAssertNotEqual(model.voiceStatus, .stopped)
    }

    @MainActor
    func testHeartbeatFailureRequiresCurrentEpochAPIAndUncancelledTask() async throws {
        let model = ZoeAppModel(defaults: UserDefaults(suiteName: UUID().uuidString)!)
        let api = try APIClient(serverURL: URL(string: "https://zoe.example")!, credentials: IndependentVoiceCredential())
        let otherAPI = try APIClient(serverURL: URL(string: "https://zoe.example")!, credentials: IndependentVoiceCredential())
        let session = VoiceSessionResponse(voiceSessionId: "same-session", generation: 4, state: "active", answerSdp: "")
        let oldEpoch = model.installVoiceSessionForTesting(session, api: api)
        let epoch = model.installVoiceSessionForTesting(session, api: api)
        var localStops = 0
        var hangups = 0
        model.voiceLocalMediaStopObserverForTesting = { localStops += 1 }
        model.voiceHangupOverrideForTesting = { _ in
            hangups += 1
            return VoiceOperationResponse(ok: true, state: "ended", upstreamTermination: "verified", poolBlocked: false)
        }
        await model.handleVoiceHeartbeatFailureForTesting(sessionID: session.voiceSessionId, api: api, mediaEpoch: oldEpoch, generation: model.connectionEpoch, message: "old epoch")
        await model.handleVoiceHeartbeatFailureForTesting(sessionID: session.voiceSessionId, api: otherAPI, mediaEpoch: epoch, generation: model.connectionEpoch, message: "old API")
        let cancelled = Task { @MainActor in
            withUnsafeCurrentTask { $0?.cancel() }
            await model.handleVoiceHeartbeatFailureForTesting(sessionID: session.voiceSessionId, api: api, mediaEpoch: epoch, generation: model.connectionEpoch, message: "cancelled")
        }
        await cancelled.value
        XCTAssertEqual(localStops, 0)
        XCTAssertEqual(hangups, 0)
        XCTAssertTrue(model.canContinueVoiceAttemptForTesting(epoch))
        guard case .active = model.voiceStatus else { return XCTFail("stale/cancelled failure must leave media active") }

        await model.handleVoiceHeartbeatFailureForTesting(sessionID: session.voiceSessionId, api: api, mediaEpoch: epoch, generation: model.connectionEpoch, message: "current failure")
        XCTAssertEqual(localStops, 1, "positive control must execute the same guarded handler")
        XCTAssertEqual(hangups, 1)
        XCTAssertFalse(model.canContinueVoiceAttemptForTesting(epoch))
        XCTAssertFalse(model.canEndVoiceSession)
        guard case .interrupted = model.voiceStatus else { return XCTFail("current failure must stop local media") }
    }
}
