import CyloKit
import SwiftUI

struct ConversationView: View {
    @ObservedObject var model: CyloAppModel
    @Environment(\.openTask) private var openTask
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                FreshnessBanner(model: model)
                Text(uiText("What would you like to work on?")).font(.largeTitle.bold())
                TaskComposer(model: model, taskID: nil, projectID: nil, accepted: openTask)
                if !model.tasks.isEmpty {
                    Text(uiText("Recent tasks")).font(.headline)
                    ForEach(model.tasks.prefix(8)) { task in
                        NavigationLink(value: WorkspaceRoute.task(task.id)) { TaskSummary(task: task, model: model) }.buttonStyle(.plain)
                        Divider()
                    }
                }
                PendingCommandRows(summaries: model.pendingCommandSummaries, model: model)
            }.frame(maxWidth: 760).padding(24).frame(maxWidth: .infinity)
        }
        .navigationTitle(uiText("Conversation"))
    }
}

struct TaskComposer: View {
    @ObservedObject var model: CyloAppModel
    let taskID: String?
    let projectID: String?
    var accepted: (String) -> Void = { _ in }
    @State private var error: String?
    @State private var submitting = false
    @State private var pendingKey: String?
    @State private var pendingMessage: String?
    @State private var showVoice = false
    private var draft: Binding<String> { Binding(get: { taskID.map { model.taskDrafts[$0] ?? "" } ?? model.newTaskDraft }, set: { model.updateTaskDraft($0, for: taskID) }) }
    private var canSend: Bool { taskID.map { model.task(id: $0)?.status.canAcceptEntries == true } ?? true }
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .bottom) {
                TextField(uiText("Message Cylo…"), text: draft, axis: .vertical).lineLimit(2...8).textFieldStyle(.plain).disabled(!canSend || submitting)
                Button { showVoice = true } label: { Label(uiText("Voice"), systemImage: "waveform").labelStyle(.iconOnly).frame(minWidth: 44, minHeight: 44).contentShape(Rectangle()) }.disabled(submitting || pendingKey != nil)
                Button { submit() } label: { Label(uiText("Send"), systemImage: "arrow.up.circle.fill").labelStyle(.iconOnly).font(.title2).frame(minWidth: 44, minHeight: 44).contentShape(Rectangle()) }
                    .keyboardShortcut(.return, modifiers: .command)
                    .disabled(submitting || pendingKey != nil || model.isBusy || !canSend || draft.wrappedValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            if submitting { ProgressView().controlSize(.small) }
            if !canSend { Text(uiText("This task no longer accepts messages.")).font(.caption).foregroundStyle(.secondary) }
            NoticeCard(message: error)
            if let pendingMessage {
                VStack(alignment: .leading, spacing: 6) {
                    Label(uiText("Confirming…"), systemImage: "clock").font(.subheadline.weight(.medium))
                    Text(uiText(pendingMessage)).font(.caption).foregroundStyle(.secondary)
                    Button(uiText("Check status")) { submit() }.disabled(submitting || model.isBusy)
                }
            }
        }
        .padding(16).background(.regularMaterial, in: RoundedRectangle(cornerRadius: 18)).overlay(RoundedRectangle(cornerRadius: 18).stroke(.quaternary))
        .sheet(isPresented: $showVoice) { VoicePanel(model: model) { transcript in draft.wrappedValue = transcript; showVoice = false } }
        .onAppear { restorePendingFeedback() }
        .onChange(of: model.pendingCommandSummaries) { _, _ in restorePendingFeedback() }
    }
    private func restorePendingFeedback() {
        if let key = model.pendingTaskSubmissionKey(for: taskID) {
            pendingKey = key
            pendingMessage = uiText("This saved action is awaiting server confirmation.")
        } else if !submitting {
            pendingKey = nil
            pendingMessage = nil
        }
    }
    private func submit() {
        submitting = true; error = nil
        Task {
            let result: TaskSubmissionResult
            if let pendingKey { result = await model.retryTaskSubmission(idempotencyKey: pendingKey) }
            else { result = await model.sendTaskDraft(for: taskID, projectID: projectID) }
            submitting = false
            switch result {
            case let .accepted(receipt):
                pendingKey = nil; pendingMessage = nil
                if let id = receipt.resourceId { accepted(id) }
            case let .failed(message): pendingKey = nil; pendingMessage = nil; error = uiText(message)
            case let .unknown(key, message): pendingKey = key; pendingMessage = message
            }
        }
    }
}

struct VoicePanel: View {
    @ObservedObject var model: CyloAppModel
    var useDraft: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        NavigationStack {
            Form {
                Section {
                    switch model.voiceStatus {
                    case .idle: Button(uiText("Connect voice"), systemImage: "mic") { Task { await model.startVoice() } }.disabled(model.isBusy || model.voicePoolStatus?.blocked == true)
                    case .connecting: ProgressView(uiText("Connecting voice…"))
                    case .active: Label(uiText("Voice connected"), systemImage: "waveform").foregroundStyle(.green)
                    case let .unavailable(reason), let .interrupted(reason):
                        Text(reason).foregroundStyle(.secondary)
                        Button(uiText("Retry voice")) { Task { await model.startVoice() } }.disabled(model.canEndVoiceSession || model.isBusy)
                    }
                    if model.voicePoolStatus?.blocked == true { Text(uiText("Voice is unavailable while the account pool is quarantined.")).foregroundStyle(.orange) }
                }
                Section(uiText("Transcript draft")) {
                    TextEditor(text: Binding(get: { model.voiceDraft }, set: model.updateVoiceDraft)).frame(minHeight: 140).accessibilityLabel(uiText("Voice transcript draft"))
                    Text(uiText("Review before sending. Using this draft does not submit a task.")).font(.caption).foregroundStyle(.secondary)
                    Button(uiText("Use draft")) { useDraft(model.voiceDraft) }.disabled(model.voiceDraft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
                if model.canEndVoiceSession { Button(uiText("End voice"), role: .destructive) { Task { await model.endVoiceSession() } } }
            }.navigationTitle(uiText("Voice"))
                .toolbar { ToolbarItem(placement: .confirmationAction) { Button(uiText("Done")) { dismiss() } } }
                .task { await model.refreshVoiceReadiness() }
        }.frame(idealWidth: 480, idealHeight: 480)
    }
}

struct PendingCommandRows: View {
    let summaries: [PendingCommandSummary]
    @ObservedObject var model: CyloAppModel

    var body: some View {
        ForEach(summaries) { summary in
            HStack(alignment: .top, spacing: 10) {
                VStack(alignment: .leading, spacing: 3) {
                    Text(summary.title).font(.subheadline.weight(.medium))
                    Text(statusText(summary.state)).font(.caption).foregroundStyle(.secondary)
                }
                Spacer(minLength: 4)
                if summary.state == .waitingForConfirmation {
                    Button(uiText("Check status")) { Task { await model.checkPendingCommand(summary) } }
                        .disabled(model.isBusy)
                }
            }
            .padding(.horizontal)
        }
    }

    private func statusText(_ state: PendingCommandSummary.State) -> String {
        switch state {
        case .waitingForConfirmation: uiText("Cylo is waiting to confirm whether the server applied this action.")
        case .checkingStatus: uiText("Checking the saved action with the server…")
        case .acceptedAwaitingSnapshot: uiText("The server accepted this action; the latest task state is still loading.")
        case .needsReview: uiText("The server state changed. Review it before choosing a new action.")
        case .differentWorkspace: uiText("This saved action belongs to a different workspace and was not retried.")
        case .workspaceUnavailable: uiText("The current workspace could not be confirmed. Refresh the server state before checking or retrying this action.")
        }
    }
}

struct TaskSummary: View {
    let task: TaskRecord
    @ObservedObject var model: CyloAppModel
    var body: some View {
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 5) {
                Text(task.title).font(.headline).foregroundStyle(.primary).lineLimit(2)
                HStack { Text(uiText(task.status.label)); if task.projectId != nil { Text(uiText("·")); Text(model.projectName(task.projectId)) } }.font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            Image(systemName: "chevron.right").font(.caption).foregroundStyle(.tertiary)
        }.padding(.vertical, 6).contentShape(Rectangle())
    }
}

struct TasksView: View {
    @ObservedObject var model: CyloAppModel
    @State private var query = ""
    @State private var filter = "all"
    @State private var searchPresented = false
    private var visible: [TaskRecord] {
        model.tasks.filter { task in
            (query.isEmpty || task.title.localizedCaseInsensitiveContains(query) || task.goal.localizedCaseInsensitiveContains(query)) &&
            (filter == "all" || (filter == "review" ? task.status == .awaitingUser : task.status.canAcceptEntries))
        }
    }
    var body: some View {
        List {
            Picker(uiText("Filter"), selection: $filter) { Text(uiText("All")).tag("all"); Text(uiText("Active")).tag("active"); Text(uiText("Needs review")).tag("review") }.pickerStyle(.segmented)
            FreshnessBanner(model: model)
            ForEach(visible) { task in NavigationLink(value: WorkspaceRoute.task(task.id)) { TaskSummary(task: task, model: model) } }
            if visible.isEmpty { ContentUnavailableView(uiText("No matching tasks"), systemImage: "checklist") }
        }
        .searchable(text: $query, isPresented: $searchPresented, prompt: uiText("Search tasks"))
        .navigationTitle(uiText("Tasks"))
        .toolbar { Button { searchPresented = true } label: { Label(uiText("Search tasks"), systemImage: "magnifyingglass") }.keyboardShortcut("f", modifiers: .command) }
        .refreshable { await model.reconnect() }
    }
}

struct TaskDetailView: View {
    let taskID: String
    @ObservedObject var model: CyloAppModel
    @State private var showingStopConfirmation = false
    @State private var stopError: String?
    @State private var stopRequested = false
    var body: some View {
        Group {
            if let task = model.task(id: taskID) {
                ScrollView {
                    VStack(alignment: .leading, spacing: 20) {
                        FreshnessBanner(model: model)
                        NoticeCard(message: stopError)
                        if stopRequested { Label(uiText("Stop requested; awaiting runtime confirmation."), systemImage: "clock").font(.caption).foregroundStyle(.secondary) }
                        HStack { Text(uiText(task.status.label)).font(.subheadline.weight(.medium)); Spacer(); Text(model.projectName(task.projectId)).font(.caption).foregroundStyle(.secondary) }
                        if model.selectedTaskID == taskID {
                            if model.isLoadingTaskEntries { ProgressView(uiText("Loading conversation…")) }
                            NoticeCard(message: model.taskEntriesError)
                            if model.taskEntriesError != nil { Button(uiText("Retry")) { Task { await model.selectTask(taskID) } } }
                            ForEach(model.taskEntries) { entry in
                                VStack(alignment: .leading, spacing: 5) {
                                    Text(entryAuthor(entry.kind)).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                                    MessageBody(content: entry.content)
                                }.frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 4)
                            }
                            if model.hasMoreTaskEntries { Button(uiText("Load more messages")) { Task { await model.loadMoreTaskEntries() } }.disabled(model.isLoadingTaskEntries) }
                        }
                        if let result = task.result, !result.isEmpty { VStack(alignment: .leading, spacing: 8) { Text(uiText("Result")).font(.headline); MessageBody(content: result) } }
                        if let error = task.error, !error.isEmpty { NoticeCard(message: error) }
                        ResultDecisionRows(task: task, model: model)
                        ApprovalRows(approvals: model.approvals.filter { $0.taskId == task.id && $0.status == "pending" }, model: model)
                        DisclosureGroup(uiText("Technical details")) {
                            VStack(alignment: .leading, spacing: 12) {
                                LabeledContent(uiText("Task ID"), value: task.id).textSelection(.enabled)
                                LabeledContent(uiText("Goal")) { Text(task.goal).textSelection(.enabled) }
                                LabeledContent(uiText("Updated"), value: uiDate(task.updatedAt))
                                EvidenceRows(evidence: task.evidence)
                                RunRows(runs: model.runs.filter { $0.taskId == task.id })
                            }.padding(.top, 8)
                        }.font(.callout)
                    }.frame(maxWidth: 760).padding(20).frame(maxWidth: .infinity)
                }
                .safeAreaInset(edge: .bottom) { TaskComposer(model: model, taskID: task.id, projectID: task.projectId).id(task.id).frame(maxWidth: 760).padding(.horizontal).padding(.bottom, 8).frame(maxWidth: .infinity).background(.bar) }
                .navigationTitle(task.title)
                .toolbar { if task.status.canAcceptEntries { Button(uiText("Request stop"), systemImage: "stop.circle", role: .destructive) { showingStopConfirmation = true } } }
                .confirmationDialog(uiText("Request that Cylo stop this task?"), isPresented: $showingStopConfirmation) {
                    Button(uiText("Request stop"), role: .destructive) { Task {
                        stopError = nil
                        if await model.stop(task, reason: "Requested from Apple client") { stopRequested = true }
                        else { stopError = model.notice ?? uiText("The stop request was not accepted.") }
                    } }
                    Button(uiText("Cancel"), role: .cancel) {}
                } message: { Text(uiText("Cylo will report separately when the runtime confirms termination.")) }
            } else {
                ContentUnavailableView {
                    Label(uiText("Task not loaded"), systemImage: "bubble.left.and.bubble.right")
                } description: { Text(uiText("Refresh to load the current task state.")) } actions: { Button(uiText("Refresh")) { Task { await model.reconnect() } } }
            }
        }
        #if os(iOS)
        .toolbar(.hidden, for: .tabBar)
        #endif
        .task(id: taskID) { await model.selectTask(taskID) }
    }
}

struct ResultDecisionRows: View {
    let task: TaskRecord
    @ObservedObject var model: CyloAppModel
    @State private var showingRejectConfirmation = false
    @State private var error: String?

    var body: some View {
        if task.canAcceptResult {
            Section(uiText("Review result")) {
                NoticeCard(message: error)
                if let run = model.resultRun(for: task) {
                        Button(uiText("Accept result")) {
                        Task {
                            error = nil
                            if !(await model.decideResult(for: task, run: run, decision: "accept")) { error = model.notice ?? uiText("The decision was not accepted.") }
                        }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(model.isBusy)
                    Button(uiText("Reject result"), role: .destructive) { showingRejectConfirmation = true }
                        .disabled(model.isBusy)
                    Text(uiText("Rejecting returns the task to waiting. It does not automatically retry the task."))
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else {
                    Text(uiText("The server marks this result as reviewable, but its confirmed run is not available in the current snapshot. Refresh before deciding."))
                        .foregroundStyle(.secondary)
                }
            }
            .confirmationDialog(uiText("Reject this result?"), isPresented: $showingRejectConfirmation) {
                Button(uiText("Reject result"), role: .destructive) {
                    guard let run = model.resultRun(for: task) else { return }
                    Task {
                        error = nil
                        if !(await model.decideResult(for: task, run: run, decision: "reject")) { error = model.notice ?? uiText("The decision was not accepted.") }
                    }
                }
                Button(uiText("Cancel"), role: .cancel) {}
            } message: {
                Text(uiText("The task will return to waiting. Cylo will not automatically retry it."))
            }
        }
    }
}

struct ApprovalRows: View {
    let approvals: [Approval]
    @ObservedObject var model: CyloAppModel
    @State private var error: String?

    var body: some View {
        if !approvals.isEmpty {
            Section {
                NoticeCard(message: error)
                ForEach(approvals) { approval in
                    VStack(alignment: .leading, spacing: 10) {
                        Text(approval.prompt).font(.headline)
                        if let digest = approval.requestedActionDigest {
                            DisclosureGroup(uiText("Requested action details")) { Text(digest).font(.caption).textSelection(.enabled) }
                        }
                        HStack {
                            Button(uiText("Approve")) { Task {
                                error = nil
                                if !(await model.decide(approval, decision: "approve")) { error = model.notice ?? uiText("The decision was not accepted.") }
                            } }
                                .buttonStyle(.borderedProminent)
                            Button(uiText("Reject"), role: .destructive) { Task {
                                error = nil
                                if !(await model.decide(approval, decision: "reject")) { error = model.notice ?? uiText("The decision was not accepted.") }
                            } }
                        }
                    }
                    .padding(.vertical, 6)
                }
            } header: {
                Text(uiText("Needs your decision"))
            } footer: {
                EmptyView()
            }
        }
    }
}

struct EvidenceRows: View {
    let evidence: [Evidence]
    var body: some View {
        Section(uiText("Evidence")) {
            if evidence.isEmpty { Text(uiText("No evidence has been recorded.")).foregroundStyle(.secondary) }
            ForEach(evidence) { item in
                VStack(alignment: .leading, spacing: 4) {
                    Text(item.summary)
                    HStack {
                        Text(item.kind)
                        Text(uiText("·"))
                        Text(item.source)
                        if item.verified { Text(uiText("· verified")) }
                    }
                    .font(.caption).foregroundStyle(.secondary)
                }
                .textSelection(.enabled)
            }
        }
    }
}

struct RunRows: View {
    let runs: [Run]
    var body: some View {
        DisclosureGroup(uiText("Execution details")) {
            if runs.isEmpty { Text(uiText("No run record is available.")).foregroundStyle(.secondary) }
            ForEach(runs) { run in
                VStack(alignment: .leading, spacing: 4) {
                    Text(uiText(run.status.capitalized))
                    LabeledContent(uiText("Control"), value: uiText(run.desiredControl.capitalized)).font(.caption)
                    LabeledContent(uiText("Version"), value: run.controlVersion.formatted()).font(.caption)
                    EvidenceRows(evidence: run.evidence)
                }
            }
        }
    }
}


private func entryAuthor(_ kind: String) -> String {
    switch kind {
    case "user", "user_message": uiText("You")
    case "assistant", "assistant_message": uiText("Cylo")
    default: uiText("Entry")
    }
}
