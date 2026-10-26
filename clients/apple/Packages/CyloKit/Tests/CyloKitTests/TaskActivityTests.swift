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

    private func task(_ id: String, _ status: TaskStatus, reviewable: Bool = false) -> TaskRecord {
        TaskRecord(id: id, projectId: nil, title: id, goal: id, source: "text", profileId: nil, status: status,
                   version: 1, createdAt: "2026-10-25T12:00:00Z", updatedAt: "2026-10-25T12:00:00Z",
                   acceptedAt: nil, startedAt: nil, finishedAt: nil, result: nil, error: nil, evidence: [],
                   canAcceptResult: reviewable)
    }
}
