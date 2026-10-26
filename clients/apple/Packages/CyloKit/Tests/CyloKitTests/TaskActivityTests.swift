import XCTest
@testable import CyloKit
@testable import CyloUI

final class TaskActivityTests: XCTestCase {
    func testApprovalAndReviewTakePrecedenceOverExecutionAndCompletion() {
        let tasks = [task("active", .active), task("review", .complete, reviewable: true), task("ordinary", .active)]
        let approval = Approval(id: "approval", taskId: "active", status: "pending", prompt: "Confirm", version: 1,
                                createdAt: "2026-10-25T12:00:00Z", expiresAt: nil, requestedAction: nil, requestedActionDigest: nil)
        let groups = TaskActivityGroups(tasks: tasks, approvals: [approval])

        XCTAssertEqual(groups.attention.map(\.id), ["active", "review"])
        XCTAssertEqual(groups.inProgress.map(\.id), ["ordinary"])
        XCTAssertTrue(groups.completed.isEmpty)
    }

    func testFailuresRemainVisibleAndOnlyAuthoritativeCompletionAppearsAsCompleted() {
        let tasks = [task("failed", .failed), task("waiting", .awaitingUser), task("paused", .paused),
                     task("done", .complete), task("canceled", .canceled)]
        let groups = TaskActivityGroups(tasks: tasks, approvals: [])

        XCTAssertEqual(groups.attention.map(\.id), ["failed", "waiting"])
        XCTAssertEqual(groups.inProgress.map(\.id), ["paused"])
        XCTAssertEqual(groups.completed.map(\.id), ["done"])
    }

    func testSettledApprovalDoesNotKeepTaskInAttentionAfterSnapshotChanges() {
        let approval = Approval(id: "approval", taskId: "task", status: "approved", prompt: "Confirm", version: 2,
                                createdAt: "2026-10-25T12:00:00Z", expiresAt: nil, requestedAction: nil, requestedActionDigest: nil)
        let groups = TaskActivityGroups(tasks: [task("task", .active)], approvals: [approval])

        XCTAssertTrue(groups.attention.isEmpty)
        XCTAssertEqual(groups.inProgress.map(\.id), ["task"])
        XCTAssertTrue(TaskActivityGroups(tasks: [], approvals: [approval]).attention.isEmpty)
    }

    func testRuntimeProofsAndUnknownEffectsStayOutOfTaskEvidenceAndFailureText() {
        let kinds = ["dispatch_started", "dispatch_bound", "effect_unknown", "no_effect_terminal", "create_rejected", "runtime_terminated", "stop_confirmed"]
        let internalEvidence = kinds.map { evidence($0, summary: "Internal AO proof and session diagnostic") }
        XCTAssertTrue(visibleTaskEvidence(internalEvidence).isEmpty)
        for kind in ["effect_unknown", "no_effect_terminal", "create_rejected"] {
            var failed = task("failed", .failed)
            failed.error = "Internal AO proof and session diagnostic"
            failed.evidence = [evidence(kind, summary: failed.error!)]
            XCTAssertEqual(taskFailureMessage(failed), uiText("This request could not be completed."))
            XCTAssertEqual(failed.status, .failed)
        }
        var rejected = task("rejected", .failed)
        rejected.error = "ao_worker_create_rejected"
        XCTAssertEqual(taskFailureMessage(rejected), uiText("This request could not be completed."))
    }

    func testBusinessFailureAndEvidenceSurviveOrdinaryRuntimeLifecycle() {
        var failed = task("business", .failed)
        failed.error = "The destination requires your approval."
        failed.result = "Prepared a draft for review."
        let business = evidence("agent_result", summary: "Prepared a draft for review.")
        failed.evidence = [evidence("dispatch_started", summary: "Internal dispatch"), business,
                           evidence("runtime_terminated", summary: "Internal termination")]
        XCTAssertEqual(taskFailureMessage(failed), failed.error)
        XCTAssertEqual(visibleTaskEvidence(failed.evidence), [business])
        XCTAssertEqual(failed.result, "Prepared a draft for review.")
    }

    private func evidence(_ kind: String, summary: String) -> Evidence {
        Evidence(id: kind, kind: kind, summary: summary, source: "runner", createdAt: "2026-10-26T12:00:00Z", verified: false)
    }

    private func task(_ id: String, _ status: TaskStatus, reviewable: Bool = false) -> TaskRecord {
        TaskRecord(id: id, projectId: nil, title: id, goal: id, source: "text", profileId: nil, status: status,
                   version: 1, createdAt: "2026-10-25T12:00:00Z", updatedAt: "2026-10-25T12:00:00Z",
                   acceptedAt: nil, startedAt: nil, finishedAt: nil, result: nil, error: nil, evidence: [],
                   canAcceptResult: reviewable)
    }
}
