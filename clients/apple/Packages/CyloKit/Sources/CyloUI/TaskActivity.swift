import CyloKit

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
