import ZoeKit

/// Partition authoritative tasks without duplicating a task across activity sections.
struct TaskActivityGroups {
    let attention: [TaskRecord]
    let inProgress: [TaskRecord]
    let completed: [TaskRecord]

    init(tasks: [TaskRecord], approvals: [Approval]) {
        let pendingTaskIDs = Set(approvals.filter { $0.status == "pending" }.map(\.taskId))
        let needsAttention = tasks.filter {
            pendingTaskIDs.contains($0.id) || $0.canAcceptResult || $0.status == .awaitingUser || $0.status == .failed
        }
        let attentionIDs = Set(needsAttention.map(\.id))
        attention = needsAttention
        inProgress = tasks.filter { !attentionIDs.contains($0.id) && $0.status.canAcceptEntries }
        completed = tasks.filter { !attentionIDs.contains($0.id) && $0.status == .complete }
    }
}

/// Lifecycle records remain server evidence; task presentation shows work outcomes.
func visibleTaskEvidence(_ evidence: [Evidence]) -> [Evidence] {
    evidence.filter { !internalRuntimeEvidenceKinds.contains($0.kind) }
}

private let internalRuntimeEvidenceKinds: Set<String> = [
    "dispatch_started", "dispatch_bound", "effect_unknown", "no_effect_terminal",
    "create_rejected", "runtime_terminated", "stop_confirmed"
]

func taskFailureMessage(_ task: TaskRecord) -> String? {
    guard let error = task.error, !error.isEmpty else { return nil }
    let internalFailure = error == "runtime_create_rejected" || task.evidence.contains {
        ["no_effect_terminal", "create_rejected", "effect_unknown"].contains($0.kind)
    }
    if task.status == .failed && internalFailure {
        return uiText("This request could not be completed.")
    }
    return error
}
