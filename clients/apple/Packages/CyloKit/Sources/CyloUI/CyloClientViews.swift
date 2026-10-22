import CyloKit
import SwiftUI
import UniformTypeIdentifiers

public struct CyloRootView: View {
    @ObservedObject private var model: CyloAppModel

    public init(model: CyloAppModel) { self.model = model }

    public var body: some View {
        TabView {
            NavigationStack { ConversationView(model: model) }
                .tabItem { Label("Conversation", systemImage: "bubble.left.and.bubble.right") }
            NavigationStack { TasksView(model: model) }
                .tabItem { Label("Tasks", systemImage: "checklist") }
            NavigationStack { ProjectsView(model: model) }
                .tabItem { Label("Projects", systemImage: "folder") }
            NavigationStack { MoreView(model: model) }
                .tabItem { Label("More", systemImage: "ellipsis") }
        }
        .task { await model.start() }
    }
}

private struct FreshnessBanner: View {
    @ObservedObject var model: CyloAppModel

    var body: some View {
        HStack(spacing: 8) {
            Circle().fill(color).frame(width: 8, height: 8)
            Text(label).font(.caption).lineLimit(2)
            Spacer(minLength: 4)
            Button("Refresh") { Task { await model.reconnect() } }
                .font(.caption)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 10))
        .accessibilityElement(children: .combine)
    }

    private var label: String {
        switch model.freshness {
        case .loading: "Connecting to Cylo…"
        case let .current(version, cursor): "Current · server \(version) · event \(cursor)"
        case let .stale(reason, cursor): "Stale at event \(cursor) · \(reason)"
        case let .offline(reason, cursor): "Offline\(cursor.map { " · saved event \($0)" } ?? "") · \(reason)"
        }
    }

    private var color: Color {
        switch model.freshness {
        case .current: .green
        case .loading: .orange
        case .stale: .orange
        case .offline: .red
        }
    }
}

private struct NoticeCard: View {
    let message: String?
    var body: some View {
        if let message, !message.isEmpty {
            Text(message)
                .font(.callout)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(12)
                .background(.blue.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
                .textSelection(.enabled)
        }
    }
}

private struct ConversationView: View {
    @ObservedObject var model: CyloAppModel
    @State private var selectedTaskID: String?
    @State private var draft = ""
    @State private var projectID = ""

    var body: some View {
        VStack(spacing: 12) {
            FreshnessBanner(model: model).padding(.horizontal)
            NoticeCard(message: model.notice).padding(.horizontal)
            if let task = model.task(id: selectedTaskID) {
                TaskConversation(task: task, model: model)
            } else {
                ContentUnavailableView("Start a conversation", systemImage: "bubble.left.and.bubble.right", description: Text("Send a request to create a durable task, or choose an existing task below."))
            }
            if !model.tasks.isEmpty || selectedTaskID != nil {
                Picker("Continue a task", selection: $selectedTaskID) {
                    Text("New task").tag(String?.none)
                    ForEach(model.tasks) { task in Text(task.title).tag(Optional(task.id)) }
                }
                .pickerStyle(.menu)
                .padding(.horizontal)
            }
            PendingCommandRows(summaries: model.pendingCommandSummaries, model: model)
            composer
        }
        .navigationTitle("Conversation")
        .task { await model.refreshVoiceReadiness() }
    }

    private var composer: some View {
        let selectedTask = model.task(id: selectedTaskID)
        let missingSelectedTask = selectedTaskID != nil && selectedTask == nil
        let canAppend = missingSelectedTask ? false : (selectedTask?.status.canAcceptEntries ?? true)
        return VStack(alignment: .leading, spacing: 8) {
            if selectedTaskID == nil, !model.projects.isEmpty {
                Picker("Project", selection: $projectID) {
                    Text("Unassigned").tag("")
                    ForEach(model.projects) { project in Text(project.name).tag(project.id) }
                }
                .pickerStyle(.menu)
            }
            HStack(alignment: .bottom, spacing: 10) {
                TextField("Describe work for Cylo…", text: $draft, axis: .vertical)
                    .lineLimit(2...6)
                    .textFieldStyle(.roundedBorder)
                    .disabled(!canAppend)
                Button {
                    let message = draft.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !message.isEmpty else { return }
                    draft = ""
                    Task {
                        if let task = selectedTask {
                            await model.append(to: task, content: message)
                        } else {
                            await model.createTask(goal: message, title: String(message.prefix(72)), projectID: projectID.isEmpty ? nil : projectID)
                        }
                    }
                } label: { Label("Send", systemImage: "arrow.up.circle.fill") }
                    .disabled(model.isBusy || !canAppend || draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            if missingSelectedTask {
                Text("This selected task is unavailable in the latest snapshot. Refresh it or choose New task before sending.")
                    .font(.caption).foregroundStyle(.secondary)
            } else if !canAppend {
                Text("This task is finished and cannot accept more entries. Choose New task to start separate work.")
                    .font(.caption).foregroundStyle(.secondary)
            }
            voiceControls
        }
        .padding()
        .background(.regularMaterial)
    }

    @ViewBuilder
    private var voiceControls: some View {
        switch model.voiceStatus {
        case let .active(sessionID, generation):
            VStack(alignment: .leading, spacing: 8) {
                Label("Voice connected · generation \(generation)", systemImage: "waveform")
                    .font(.caption).foregroundStyle(.green)
                TextEditor(text: Binding(get: { model.voiceDraft }, set: model.updateVoiceDraft))
                    .frame(minHeight: 54, maxHeight: 100)
                    .overlay(alignment: .topLeading) {
                        if model.voiceDraft.isEmpty {
                            Text("Your speech transcript will appear here for review.").foregroundStyle(.secondary).padding(.top, 8).padding(.leading, 5)
                        }
                    }
                    .accessibilityLabel("Voice transcript draft")
                HStack {
                    Button("Use as task") { Task { await model.confirmVoiceDraftAsTask() } }
                        .buttonStyle(.borderedProminent)
                        .disabled(model.isBusy || model.voiceDraft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    Spacer()
                    Button("End voice", role: .destructive) { Task { await model.endVoiceSession() } }
                }
                Text("Transcript captions are drafts. Review them before Cylo saves a voice turn or creates a task.")
                    .font(.caption2).foregroundStyle(.secondary)
                    .textSelection(.enabled)
                Text(sessionID).font(.caption2).foregroundStyle(.tertiary).textSelection(.enabled)
            }
        case .connecting:
            Label("Connecting live voice…", systemImage: "waveform")
                .font(.caption).foregroundStyle(.orange)
        case let .unavailable(reason), let .interrupted(reason):
            VStack(alignment: .leading, spacing: 6) {
                Button { Task { await model.startVoice() } } label: { Label("Start live voice", systemImage: "mic") }
                    .disabled(model.isBusy)
                Text(reason).font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
            }
        case .idle:
            VStack(alignment: .leading, spacing: 6) {
                Button { Task { await model.startVoice() } } label: { Label("Start live voice", systemImage: "mic") }
                    .disabled(model.isBusy || model.voicePoolStatus?.blocked == true)
                if model.voicePoolStatus?.blocked == true {
                    Text("Voice account pool is quarantined pending verified upstream termination.")
                        .font(.caption).foregroundStyle(.orange)
                }
            }
        }
    }
}

private struct PendingCommandRows: View {
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
                    Button("Check status") { Task { await model.checkPendingCommand(summary) } }
                        .disabled(model.isBusy)
                }
            }
            .padding(.horizontal)
        }
    }

    private func statusText(_ state: PendingCommandSummary.State) -> String {
        switch state {
        case .waitingForConfirmation: "Cylo is waiting to confirm whether the server applied this action."
        case .checkingStatus: "Checking the saved action with the server…"
        case .acceptedAwaitingSnapshot: "The server accepted this action; the latest task state is still loading."
        case .needsReview: "The server state changed. Review it before choosing a new action."
        case .differentWorkspace: "This saved action belongs to a different workspace and was not retried."
        case .workspaceUnavailable: "The current workspace could not be confirmed. Refresh the server state before checking or retrying this action."
        }
    }
}

private struct TaskConversation: View {
    let task: TaskRecord
    @ObservedObject var model: CyloAppModel

    var body: some View {
        List {
            Section {
                LabeledContent("Status", value: task.status.label)
                LabeledContent("Project", value: model.projectName(task.projectId))
                Text(task.goal).textSelection(.enabled)
            }
            Section("Current result") {
                if let result = task.result, !result.isEmpty { Text(result).textSelection(.enabled) }
                else { Text("The server has not recorded a result yet.").foregroundStyle(.secondary) }
                if task.status == .awaitingUser {
                    Label("Your review is required before this goal is accepted.", systemImage: "person.crop.circle.badge.questionmark")
                        .foregroundStyle(.orange)
                }
            }
            ResultDecisionRows(task: task, model: model)
            ApprovalRows(approvals: model.approvals.filter { $0.taskId == task.id && $0.status == "pending" }, model: model)
            EvidenceRows(evidence: task.evidence)
            RunRows(runs: model.runs.filter { $0.taskId == task.id })
        }
        #if os(iOS)
        .listStyle(.insetGrouped)
        #endif
    }
}

private struct TasksView: View {
    @ObservedObject var model: CyloAppModel

    var body: some View {
        VStack(spacing: 8) {
            FreshnessBanner(model: model).padding(.horizontal)
            NoticeCard(message: model.notice).padding(.horizontal)
            PendingCommandRows(summaries: model.pendingCommandSummaries, model: model)
            NavigationSplitView {
                List(model.tasks) { task in
                    NavigationLink(value: task.id) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(task.title).font(.headline).lineLimit(2)
                            Text("\(task.status.label) · \(model.projectName(task.projectId))")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
                .overlay {
                    if model.tasks.isEmpty { ContentUnavailableView("No tasks yet", systemImage: "checklist") }
                }
                .navigationTitle("Tasks")
            } detail: {
                NavigationStack {
                    ContentUnavailableView("Choose a task", systemImage: "checklist", description: Text("Task status, approvals, results, and evidence come from the server snapshot."))
                }
            }
            .navigationDestination(for: String.self) { id in
                if let task = model.task(id: id) {
                    TaskDetailView(task: task, model: model)
                } else {
                    ContentUnavailableView("Task no longer exists", systemImage: "exclamationmark.triangle")
                }
            }
        }
    }
}

private struct TaskDetailView: View {
    let task: TaskRecord
    @ObservedObject var model: CyloAppModel
    @State private var showingStopConfirmation = false

    var body: some View {
        List {
            Section {
                LabeledContent("Status", value: task.status.label)
                LabeledContent("Project", value: model.projectName(task.projectId))
                Text(task.goal).textSelection(.enabled)
            }
            if task.status == .queued || task.status == .active || task.status == .waiting || task.status == .paused {
                Section {
                    Button("Request stop", role: .destructive) { showingStopConfirmation = true }
                } footer: {
                    Text("A request records intent. The server will confirm whether runtime termination succeeded.")
                }
            }
            Section("Result") {
                if let result = task.result, !result.isEmpty { Text(result).textSelection(.enabled) }
                else { Text("No result has been recorded.").foregroundStyle(.secondary) }
                if task.status == .awaitingUser { Text("This task is awaiting your review.").foregroundStyle(.orange) }
            }
            ResultDecisionRows(task: task, model: model)
            ApprovalRows(approvals: model.approvals.filter { $0.taskId == task.id && $0.status == "pending" }, model: model)
            EvidenceRows(evidence: task.evidence)
            RunRows(runs: model.runs.filter { $0.taskId == task.id })
        }
        .navigationTitle(task.title)
        .confirmationDialog("Request that Cylo stop this task?", isPresented: $showingStopConfirmation) {
            Button("Request stop", role: .destructive) { Task { await model.stop(task, reason: "Requested from Apple client") } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Cylo will report separately when the runtime confirms termination.")
        }
    }
}

private struct ResultDecisionRows: View {
    let task: TaskRecord
    @ObservedObject var model: CyloAppModel
    @State private var showingRejectConfirmation = false

    var body: some View {
        if task.canAcceptResult {
            Section("Review result") {
                if let run = model.resultRun(for: task) {
                    Text("Runtime termination is confirmed for run \(run.id).")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                    Button("Accept result") {
                        Task { await model.decideResult(for: task, run: run, decision: "accept") }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(model.isBusy)
                    Button("Reject result", role: .destructive) { showingRejectConfirmation = true }
                        .disabled(model.isBusy)
                    Text("Rejecting returns the task to waiting. It does not automatically retry the task.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                } else {
                    Text("The server marks this result as reviewable, but its confirmed run is not available in the current snapshot. Refresh before deciding.")
                        .foregroundStyle(.secondary)
                }
            }
            .confirmationDialog("Reject this result?", isPresented: $showingRejectConfirmation) {
                Button("Reject result", role: .destructive) {
                    guard let run = model.resultRun(for: task) else { return }
                    Task { await model.decideResult(for: task, run: run, decision: "reject") }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("The task will return to waiting. Cylo will not automatically retry it.")
            }
        }
    }
}

private struct ApprovalRows: View {
    let approvals: [Approval]
    @ObservedObject var model: CyloAppModel

    var body: some View {
        if !approvals.isEmpty {
            Section {
                ForEach(approvals) { approval in
                    VStack(alignment: .leading, spacing: 10) {
                        Text(approval.prompt).font(.headline)
                        if let digest = approval.requestedActionDigest {
                            Text("Requested action digest: \(digest)").font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
                        }
                        HStack {
                            Button("Approve") { Task { await model.decide(approval, decision: "approve") } }
                                .buttonStyle(.borderedProminent)
                            Button("Reject", role: .destructive) { Task { await model.decide(approval, decision: "reject") } }
                        }
                    }
                    .padding(.vertical, 6)
                }
            } header: {
                Text("Needs your decision")
            } footer: {
                Text("Only pending approvals returned by the server can be decided. An expired or stale approval must be refreshed.")
            }
        }
    }
}

private struct EvidenceRows: View {
    let evidence: [Evidence]
    var body: some View {
        Section("Evidence") {
            if evidence.isEmpty { Text("No evidence has been recorded.").foregroundStyle(.secondary) }
            ForEach(evidence) { item in
                VStack(alignment: .leading, spacing: 4) {
                    Text(item.summary)
                    HStack {
                        Text(item.kind)
                        Text("·")
                        Text(item.source)
                        if item.verified { Text("· verified") }
                    }
                    .font(.caption).foregroundStyle(.secondary)
                }
                .textSelection(.enabled)
            }
        }
    }
}

private struct RunRows: View {
    let runs: [Run]
    var body: some View {
        DisclosureGroup("Execution details") {
            if runs.isEmpty { Text("No run record is available.").foregroundStyle(.secondary) }
            ForEach(runs) { run in
                VStack(alignment: .leading, spacing: 4) {
                    Text(run.status.capitalized)
                    Text("Control: \(run.desiredControl) · version \(run.controlVersion)").font(.caption)
                    EvidenceRows(evidence: run.evidence)
                }
            }
        }
    }
}

private struct ProjectsView: View {
    @ObservedObject var model: CyloAppModel
    @State private var showingCreate = false

    var body: some View {
        VStack(spacing: 8) {
            FreshnessBanner(model: model).padding(.horizontal)
            NoticeCard(message: model.notice).padding(.horizontal)
            List(model.projects) { project in
                NavigationLink {
                    ProjectDetailView(project: project, model: model)
                } label: {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(project.name).font(.headline)
                        Text("\(project.status.capitalized) · \(model.tasks.filter { $0.projectId == project.id }.count) tasks")
                            .font(.caption).foregroundStyle(.secondary)
                        if let description = project.description { Text(description).font(.subheadline).foregroundStyle(.secondary) }
                    }
                }
            }
            .overlay { if model.projects.isEmpty { ContentUnavailableView("No projects yet", systemImage: "folder") } }
        }
        .navigationTitle("Projects")
        .toolbar { ToolbarItem(placement: .primaryAction) { Button("New project", systemImage: "plus") { showingCreate = true } } }
        .sheet(isPresented: $showingCreate) { CreateProjectSheet(model: model) }
    }
}

private struct ProjectDetailView: View {
    let project: Project
    @ObservedObject var model: CyloAppModel

    var body: some View {
        List {
            Section {
                Text(project.description ?? "No description")
                LabeledContent("Status", value: project.status.capitalized)
                LabeledContent("Version", value: String(project.version))
            }
            Section("Tasks") {
                let tasks = model.tasks.filter { $0.projectId == project.id }
                if tasks.isEmpty { Text("No tasks in this project.").foregroundStyle(.secondary) }
                ForEach(tasks) { task in
                    NavigationLink(task.title, value: task.id)
                    Text(task.status.label).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
        .navigationTitle(project.name)
        .navigationDestination(for: String.self) { id in
            if let task = model.task(id: id) { TaskDetailView(task: task, model: model) }
        }
    }
}

private struct CreateProjectSheet: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CyloAppModel
    @State private var name = ""
    @State private var details = ""

    var body: some View {
        NavigationStack {
            Form {
                TextField("Name", text: $name)
                TextField("Description (optional)", text: $details, axis: .vertical).lineLimit(3...6)
            }
            .navigationTitle("New project")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Create") {
                        Task { await model.createProject(name: name, description: details.isEmpty ? nil : details); dismiss() }
                    }.disabled(name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || model.isBusy)
                }
            }
        }
        .frame(minWidth: 340, minHeight: 260)
    }
}

private struct MoreView: View {
    @ObservedObject var model: CyloAppModel
    var body: some View {
        List {
            NavigationLink { NotificationInboxView(model: model) } label: { Label("Inbox", systemImage: "bell") }
            NavigationLink { MemoryView(model: model) } label: { Label("Memory", systemImage: "brain.head.profile") }
            NavigationLink { CapabilitiesView(model: model) } label: { Label("Capabilities", systemImage: "puzzlepiece.extension") }
            NavigationLink { SettingsView(model: model) } label: { Label("Settings", systemImage: "gearshape") }
        }
        .navigationTitle("More")
    }
}

private struct NotificationInboxView: View {
    @ObservedObject var model: CyloAppModel
    @State private var unreadOnly = false

    var body: some View {
        List {
            FreshnessBanner(model: model)
            Toggle("Unread only", isOn: $unreadOnly)
            if model.notifications.isEmpty {
                ContentUnavailableView("Inbox is empty", systemImage: "bell")
            }
            ForEach(model.notifications) { item in
                NavigationLink {
                    NotificationDetailView(notification: item, model: model)
                } label: {
                    VStack(alignment: .leading, spacing: 5) {
                        HStack {
                            Text(item.title).font(.headline)
                            Spacer()
                            if item.readAt == nil { Circle().fill(.blue).frame(width: 8, height: 8).accessibilityLabel("Unread") }
                        }
                        Text(item.body).font(.subheadline).foregroundStyle(.secondary).lineLimit(3)
                        Text("\(item.kind.replacingOccurrences(of: ".", with: " · ")) · \(item.createdAt)")
                            .font(.caption2).foregroundStyle(.tertiary)
                    }
                }
            }
        }
        .navigationTitle("Inbox")
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                if model.pushNotificationsEnabled {
                    Button("Disable push wakeups", systemImage: "bell.slash") {
                        Task { await model.disablePushNotifications() }
                    }
                } else {
                    Button("Enable push wakeups", systemImage: "bell.badge") {
                        Task { await model.enablePushNotifications() }
                    }
                }
                Button("Refresh", systemImage: "arrow.clockwise") { Task { await model.refreshNotifications(unreadOnly: unreadOnly) } }
            }
        }
        .task { await model.refreshNotifications(unreadOnly: unreadOnly) }
        .onChange(of: unreadOnly) { _, value in Task { await model.refreshNotifications(unreadOnly: value) } }
        .refreshable { await model.refreshNotifications(unreadOnly: unreadOnly) }
    }
}

private struct NotificationDetailView: View {
    let notification: NotificationRecord
    @ObservedObject var model: CyloAppModel

    var body: some View {
        Group {
            if notification.aggregateType == "task", let task = model.task(id: notification.aggregateId) {
                TaskDetailView(task: task, model: model)
            } else {
                List {
                    Section(notification.title) {
                        Text(notification.body).textSelection(.enabled)
                        LabeledContent("Kind", value: notification.kind)
                        LabeledContent("Record", value: "\(notification.aggregateType) · \(notification.aggregateId)")
                        LabeledContent("Source event", value: notification.sourceEventId).textSelection(.enabled)
                    }
                    if notification.readAt == nil {
                        Button("Mark as read") { Task { await model.markNotificationRead(notification) } }
                    }
                }
            }
        }
        .task {
            if notification.readAt == nil { await model.markNotificationRead(notification) }
        }
        .navigationTitle(notification.title)
    }
}

private struct MemoryView: View {
    @ObservedObject var model: CyloAppModel
    @State private var showingCreate = false
    @State private var query = ""

    var body: some View {
        List {
            FreshnessBanner(model: model)
            if !model.memoryMissing.isEmpty {
                Section("Source unavailable") { ForEach(model.memoryMissing, id: \.self) { Text($0).font(.caption).textSelection(.enabled) } }
            }
            if !model.memoryConflicts.isEmpty {
                Section("Conflicting records") { ForEach(model.memoryConflicts, id: \.self) { Text($0).foregroundStyle(.orange) } }
            }
            Section("Authoritative records") {
                if model.memoryClaims.isEmpty { ContentUnavailableView("No matching memories", systemImage: "brain.head.profile") }
                ForEach(model.memoryClaims) { claim in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(claim.subject).font(.headline)
                        Text(claim.content).textSelection(.enabled)
                        Text("\(claim.scope.kind.capitalized) · \(claim.authorityKind.replacingOccurrences(of: "_", with: " ")) · revision \(claim.revision)")
                            .font(.caption).foregroundStyle(.secondary)
                        ForEach(claim.sources) { source in
                            Text("Source: \(source.kind) · \(source.reference)").font(.caption2).foregroundStyle(.secondary).textSelection(.enabled)
                        }
                        Button("Delete memory", role: .destructive) { Task { await model.deleteMemory(claim) } }
                            .disabled(model.isBusy)
                    }.padding(.vertical, 4)
                }
            }
        }
        .searchable(text: $query, prompt: "Search memory")
        .onSubmit(of: .search) { Task { await model.refreshMemory(query: query) } }
        .task { await model.refreshMemory(query: query) }
        .toolbar { ToolbarItem(placement: .primaryAction) { Button("Remember", systemImage: "plus") { showingCreate = true } } }
        .sheet(isPresented: $showingCreate) { CreateMemorySheet(model: model) }
        .navigationTitle("Memory")
    }
}

private struct CreateMemorySheet: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CyloAppModel
    @State private var scope = "personal"
    @State private var scopeID = ""
    @State private var subject = ""
    @State private var key = ""
    @State private var content = ""

    var body: some View {
        NavigationStack {
            Form {
                Picker("Scope", selection: $scope) {
                    Text("Personal").tag("personal")
                    Text("Workspace").tag("workspace")
                    Text("Project").tag("project")
                    Text("Task").tag("task")
                }
                if scope == "project" || scope == "task" {
                    TextField(scope == "project" ? "Project ID" : "Task ID", text: $scopeID)
                }
                TextField("Subject", text: $subject)
                TextField("Memory key", text: $key)
                TextField("Remember", text: $content, axis: .vertical).lineLimit(4...10)
                Text("This creates a user-confirmed, source-linked record in the server authority.")
                    .font(.caption).foregroundStyle(.secondary)
            }
            .navigationTitle("Remember")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task {
                            await model.remember(scope: scope, scopeID: scopeID.isEmpty ? nil : scopeID, subject: subject, key: key, content: content)
                            dismiss()
                        }
                    }
                    .disabled(model.isBusy || subject.isEmpty || key.isEmpty || content.isEmpty || ((scope == "project" || scope == "task") && scopeID.isEmpty))
                }
            }
        }
        .frame(minWidth: 360, minHeight: 420)
    }
}

private struct CapabilitiesView: View {
    @ObservedObject var model: CyloAppModel
    @State private var showingReleaseForm = false
    @State private var showingDeploymentForm = false
    @State private var authorizationTarget: CapabilityDeployment?
    @State private var revocationTarget: CapabilityDeployment?
    @State private var showingRevocationConfirmation = false
    @State private var releaseFormEpoch: UInt64 = 0
    @State private var deploymentFormEpoch: UInt64 = 0
    @State private var authorizationEpoch: UInt64 = 0
    @State private var revocationEpoch: UInt64 = 0

    var body: some View {
        List {
            FreshnessBanner(model: model)
            Section {
                Text("Desired state records the requested outcome. Observed state reports the latest runner observation; a request is not complete until the observed state confirms it.")
                    .font(.footnote).foregroundStyle(.secondary)
            }
            Section("Deployments") {
                if model.capabilityDeployments.isEmpty {
                    ContentUnavailableView("No deployments", systemImage: "shippingbox", description: Text("Request a deployment from an admitted release."))
                }
                ForEach(model.capabilityDeployments) { deployment in
                    VStack(alignment: .leading, spacing: 8) {
                        Text(deployment.serviceId).font(.headline)
                        LabeledContent("Desired", value: deployment.desiredState.capitalized)
                        LabeledContent("Observed", value: deployment.observedState.capitalized)
                        LabeledContent("Operation", value: deployment.requestedOperation.capitalized)
                        LabeledContent("Version", value: String(deployment.version))
                        if !deployment.grantId.isEmpty {
                            LabeledContent("Grant", value: String(deployment.grantId.prefix(12)))
                        }
                        if !deployment.lastErrorCode.isEmpty {
                            Text("Latest error: \(deployment.lastErrorCode)").font(.caption).foregroundStyle(.orange)
                        }
                        Text("Updated \(deployment.updatedAt)").font(.caption2).foregroundStyle(.tertiary)
                        if let revokedAt = deployment.revokedAt {
                            VStack(alignment: .leading, spacing: 4) {
                                Label("Authorization revoked", systemImage: "hand.raised.slash")
                                    .foregroundStyle(.red)
                                Text("Revoked \(revokedAt)")
                                    .font(.caption)
                                if let revokedBy = deployment.revokedBy, !revokedBy.isEmpty {
                                    Text("By \(revokedBy)").font(.caption2).foregroundStyle(.secondary)
                                }
                                if let cleanupStatus = deployment.revocationCleanupStatus {
                                    switch cleanupStatus {
                                    case let .pending(observedState):
                                        Label("Stop cleanup pending · runner last observed \(observedState)", systemImage: "clock")
                                            .foregroundStyle(.orange)
                                    case .confirmed:
                                        Label("Runner stop cleanup confirmed", systemImage: "checkmark.circle")
                                            .foregroundStyle(.green)
                                    case let .unknown(observedState):
                                        Label("Stop cleanup status unknown · runner last observed \(observedState)", systemImage: "exclamationmark.triangle")
                                            .foregroundStyle(.red)
                                    case .uncertain:
                                        Label("Stop cleanup outcome is uncertain", systemImage: "exclamationmark.triangle")
                                            .foregroundStyle(.red)
                                    }
                                }
                            }
                            .font(.caption)
                            .accessibilityElement(children: .combine)
                        } else if !deployment.grantId.isEmpty {
                            Button("Revoke authorization", role: .destructive) {
                                revocationEpoch = model.connectionEpoch
                                revocationTarget = deployment
                                showingRevocationConfirmation = true
                            }
                            .disabled(model.isBusy)
                        }
                        if deployment.desiredState == "awaiting_authorization",
                           let release = model.capabilityReleases.first(where: { $0.digest == deployment.releaseDigest }) {
                            Button("Review authorization") {
                                authorizationEpoch = model.connectionEpoch
                                authorizationTarget = deployment
                            }
                                .disabled(model.isBusy || release.digest.isEmpty)
                        }
                    }
                    .padding(.vertical, 4)
                }
            }
            Section("Immutable releases") {
                if model.capabilityReleases.isEmpty {
                    ContentUnavailableView("No releases", systemImage: "archivebox", description: Text("Submit a content-addressed manifest to create the first release."))
                }
                ForEach(model.capabilityReleases) { release in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(release.manifest.artifactRef).font(.headline).textSelection(.enabled)
                        Text("Digest · \(release.digest)").font(.caption).textSelection(.enabled)
                        Text("Capabilities · \(release.manifest.capabilities.joined(separator: ", "))")
                            .font(.caption).foregroundStyle(.secondary)
                        Text("Admitted \(release.createdAt)").font(.caption2).foregroundStyle(.tertiary)
                    }
                    .padding(.vertical, 4)
                }
            }
        }
        .navigationTitle("Capabilities")
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                Button("Refresh", systemImage: "arrow.clockwise") { Task { await model.refreshCapabilities() } }
                Menu {
                    Button("Submit release…", systemImage: "plus.square") {
                        releaseFormEpoch = model.connectionEpoch
                        showingReleaseForm = true
                    }
                    Button("Request deployment…", systemImage: "shippingbox") {
                        deploymentFormEpoch = model.connectionEpoch
                        showingDeploymentForm = true
                    }
                        .disabled(model.capabilityReleases.isEmpty)
                } label: { Label("Actions", systemImage: "ellipsis.circle") }
            }
        }
        .task { await model.refreshCapabilities() }
        .refreshable { await model.refreshCapabilities() }
        .sheet(isPresented: $showingReleaseForm) { CreateCapabilityReleaseSheet(model: model, connectionEpoch: releaseFormEpoch) }
        .sheet(isPresented: $showingDeploymentForm) { RequestCapabilityDeploymentSheet(model: model, connectionEpoch: deploymentFormEpoch) }
        .sheet(item: $authorizationTarget) { deployment in
            if let release = model.capabilityReleases.first(where: { $0.digest == deployment.releaseDigest }) {
                AuthorizeCapabilityDeploymentSheet(model: model, deployment: deployment, release: release, connectionEpoch: authorizationEpoch)
            }
        }
        .onChange(of: model.connectionEpoch) { _, _ in
            showingReleaseForm = false
            showingDeploymentForm = false
            authorizationTarget = nil
            showingRevocationConfirmation = false
            revocationTarget = nil
        }
        .confirmationDialog(
            "Revoke authorization for \(revocationTarget?.serviceId ?? "this service")?",
            isPresented: $showingRevocationConfirmation
        ) {
            Button("Revoke authorization", role: .destructive) {
                guard let deployment = revocationTarget else { return }
                Task {
                    if await model.revokeCapabilityDeployment(deployment, expectedConnectionEpoch: revocationEpoch) {
                        revocationTarget = nil
                    }
                }
            }
            Button("Cancel", role: .cancel) { revocationTarget = nil }
        } message: {
            Text("Cylo will block new work immediately and queue stop cleanup. This action does not confirm that the runner has stopped the service; observed state will report that separately.")
        }
    }
}

private struct CreateCapabilityReleaseSheet: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CyloAppModel
    let connectionEpoch: UInt64
    @State private var artifactRef = "oci://registry.example/service:1"
    @State private var artifactDigest = "sha256:"
    @State private var entrypoint = "/app/service"
    @State private var capabilities = ""
    @State private var cpuMillis: Int64 = 1000
    @State private var memoryMiB: Int64 = 512
    @State private var diskMiB: Int64 = 1024
    @State private var timeoutSeconds: Int64 = 300
    @State private var concurrency = 1
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            Form {
                Section("Immutable artifact") {
                    TextField("Artifact reference (OCI or HTTPS)", text: $artifactRef)
                    TextField("Artifact digest (sha256:…)", text: $artifactDigest)
                        #if os(iOS)
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                        #endif
                    TextField("Entrypoint", text: $entrypoint)
                    TextField("Capability IDs, comma separated", text: $capabilities)
                    Text("Secret values cannot be entered here. Manifests may only refer to deployment-managed secret names.")
                        .font(.caption).foregroundStyle(.secondary)
                }
                Section("Maximum resource budget") {
                    LabeledContent("CPU (millicores)") {
                        TextField("1000", value: $cpuMillis, format: .number)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                    }
                    LabeledContent("Memory (MiB)") {
                        TextField("512", value: $memoryMiB, format: .number)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                    }
                    LabeledContent("Disk (MiB)") {
                        TextField("1024", value: $diskMiB, format: .number)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                    }
                    LabeledContent("Timeout (seconds)") {
                        TextField("300", value: $timeoutSeconds, format: .number)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                    }
                    LabeledContent("Concurrency") {
                        TextField("1", value: $concurrency, format: .number)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                    }
                    Text("Network access defaults to none. The server validates and canonicalizes this manifest before admission.")
                        .font(.caption).foregroundStyle(.secondary)
                }
                if let errorMessage {
                    Section("Submission failed") {
                        Text(errorMessage).foregroundStyle(.red).textSelection(.enabled)
                    }
                }
            }
            .navigationTitle("Submit release")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Submit") {
                        Task {
                            errorMessage = nil
                            if await model.submitCapabilityRelease(artifactRef: artifactRef, artifactDigest: artifactDigest, entrypoint: entrypoint, capabilities: capabilities, cpuMillis: cpuMillis, memoryBytes: memoryMiB * 1_048_576, diskBytes: diskMiB * 1_048_576, timeoutSeconds: timeoutSeconds, concurrency: concurrency, expectedConnectionEpoch: connectionEpoch) {
                                dismiss()
                            } else {
                                errorMessage = model.notice ?? "The server did not accept this release. Your entries are still here."
                            }
                        }
                    }
                    .disabled(model.isBusy || artifactRef.isEmpty || artifactDigest.isEmpty || entrypoint.isEmpty || capabilities.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || cpuMillis < 1 || memoryMiB < 1 || memoryMiB > Int64.max / 1_048_576 || diskMiB < 1 || diskMiB > Int64.max / 1_048_576 || timeoutSeconds < 1 || concurrency < 1)
                }
            }
        }
        .frame(minWidth: 380, minHeight: 520)
    }
}

private struct RequestCapabilityDeploymentSheet: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CyloAppModel
    let connectionEpoch: UInt64
    @State private var serviceID = ""
    @State private var releaseDigest = ""
    @State private var operation = "install"
    @State private var errorMessage: String?
    private let operations = ["install", "start", "stop", "pause", "resume", "update", "uninstall"]

    var body: some View {
        NavigationStack {
            Form {
                TextField("Service ID", text: $serviceID)
                Picker("Release", selection: $releaseDigest) {
                    ForEach(model.capabilityReleases) { release in
                        Text("\(release.manifest.artifactRef) · \(release.digest.prefix(12))").tag(release.digest)
                    }
                }
                Picker("Requested operation", selection: $operation) {
                    ForEach(operations, id: \.self) { Text($0.capitalized).tag($0) }
                }
                Text("This records desired work as awaiting authorization. The runner has not acted yet.")
                    .font(.caption).foregroundStyle(.secondary)
                if let current = model.capabilityDeployments.first(where: { $0.serviceId == serviceID }) {
                    Text("Current deployment version: \(current.version). The request will use this version and be rejected if it has changed on the server.")
                        .font(.caption).foregroundStyle(.secondary)
                }
                if let errorMessage {
                    Text(errorMessage).foregroundStyle(.red).textSelection(.enabled)
                }
            }
            .navigationTitle("Request deployment")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Request") {
                        Task {
                            errorMessage = nil
                            if await model.requestCapabilityDeployment(serviceID: serviceID, releaseDigest: releaseDigest, operation: operation, expectedConnectionEpoch: connectionEpoch) {
                                dismiss()
                            } else {
                                errorMessage = model.notice ?? "The server did not accept this deployment request. Your entries are still here."
                            }
                        }
                    }
                    .disabled(model.isBusy || serviceID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || releaseDigest.isEmpty)
                }
            }
        }
        .frame(minWidth: 360, minHeight: 300)
        .onAppear { if releaseDigest.isEmpty { releaseDigest = model.capabilityReleases.first?.digest ?? "" } }
    }
}

private struct AuthorizeCapabilityDeploymentSheet: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CyloAppModel
    let deployment: CapabilityDeployment
    let release: CapabilityRelease
    let connectionEpoch: UInt64
    @State private var expiresAt = Calendar.current.date(byAdding: .day, value: 7, to: .now) ?? .now
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            Form {
                Section("Authorization request") {
                    LabeledContent("Service", value: deployment.serviceId)
                    LabeledContent("Requested action", value: deployment.requestedOperation.capitalized)
                    LabeledContent("Release digest", value: deployment.releaseDigest).textSelection(.enabled)
                    Text("The grant covers this action at the manifest's maximum budget. Authorizing queues the request for the runner; it does not claim that the deployment succeeded.")
                        .font(.caption).foregroundStyle(.secondary)
                }
                DatePicker("Grant expires", selection: $expiresAt, in: Date.now.addingTimeInterval(60)..., displayedComponents: [.date, .hourAndMinute])
                if let errorMessage {
                    Text(errorMessage).foregroundStyle(.red).textSelection(.enabled)
                }
            }
            .navigationTitle("Authorize deployment")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Authorize & queue", role: .destructive) {
                        Task {
                            errorMessage = nil
                            if await model.authorizeCapabilityDeployment(deployment, release: release, expiresAt: expiresAt, expectedConnectionEpoch: connectionEpoch) {
                                dismiss()
                            } else {
                                errorMessage = model.notice ?? "The server did not accept this authorization. Your selection is still here."
                            }
                        }
                    }
                    .disabled(model.isBusy || expiresAt <= .now)
                }
            }
        }
        .frame(minWidth: 380, minHeight: 340)
    }
}

private struct SettingsView: View {
    @ObservedObject var model: CyloAppModel
    @State private var deviceName = "Apple device"
    @State private var bootstrapCode = ""
    @State private var showingRevoke: Device?
    @State private var exportScope = "personal"
    @State private var showingExportSave = false

    private var exportDocument: ScopedExportFileDocument {
        ScopedExportFileDocument(fileURL: model.exportArtifact?.fileURL ?? FileManager.default.temporaryDirectory)
    }

    var body: some View {
        Form {
            Section("Server") {
                TextField("Server URL", text: $model.serverAddress)
                    #if os(iOS)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.URL)
                    #endif
                Button("Save and connect") { Task { await model.updateServerAddress() } }
                FreshnessBanner(model: model)
            }
            Section("This device") {
                TextField("Device name", text: $deviceName)
                SecureField("One-use bootstrap code", text: $bootstrapCode)
                Button("Bootstrap first approved device") {
                    Task { await model.bootstrap(code: bootstrapCode, deviceName: deviceName); bootstrapCode = "" }
                }
                .disabled(bootstrapCode.isEmpty || deviceName.isEmpty || model.isBusy)
                Button("Request approval on this device") { Task { await model.requestPairing(deviceName: deviceName) } }
                if let pairing = model.pairingStatus {
                    LabeledContent("Pairing", value: pairing.status.capitalized)
                    LabeledContent("Expires", value: pairing.expiresAt)
                    Button("Check approval") { Task { await model.refreshPairing() } }
                }
            }
            Section("Devices") {
                if model.devices.isEmpty { Text("No device records in the server snapshot.").foregroundStyle(.secondary) }
                ForEach(model.devices) { device in
                    VStack(alignment: .leading, spacing: 8) {
                        LabeledContent(device.name, value: device.status.capitalized)
                        if device.status == "pending" {
                            Button("Approve device") { Task { await model.approve(device) } }
                        } else if device.status == "approved" {
                            Button("Revoke device", role: .destructive) { showingRevoke = device }
                        }
                    }
                }
            }
            Section("Your data") {
                Text("Prepare a copy of authorized Cylo records. Large artifact files, derived stores, and backups are not included.")
                    .foregroundStyle(.secondary)
                Picker("Data scope", selection: $exportScope) {
                    Text("Personal data").tag("personal")
                    Text("Workspace data I can access").tag("workspace")
                }
                Button("Prepare export") {
                    let epoch = model.connectionEpoch
                    Task { await model.prepareExport(scope: exportScope, expectedConnectionEpoch: epoch) }
                }
                .disabled(model.exportStatus == "Preparing the authorized export…" || model.exportArtifact != nil)
                if let artifact = model.exportArtifact {
                    LabeledContent("Prepared file", value: artifact.filename)
                    Button("Save export…") { showingExportSave = true }
                    Button("Discard export", role: .cancel) { model.cancelExport() }
                }
                if let status = model.exportStatus {
                    Text(status).font(.footnote).foregroundStyle(.secondary).textSelection(.enabled)
                }
            }
            if let notice = model.notice { Section("Latest update") { Text(notice).textSelection(.enabled) } }
        }
        .navigationTitle("Settings")
        .fileExporter(isPresented: $showingExportSave, document: exportDocument, contentType: .json, defaultFilename: model.exportArtifact?.filename ?? "cylo-export.json") { result in
            switch result {
            case .success: model.exportSaveFinished(saved: true)
            case let .failure(error): model.exportSaveFinished(saved: false, error: error)
            }
        }
        .onChange(of: model.connectionEpoch) { _, _ in showingExportSave = false }
        .onChange(of: model.exportArtifact?.fileURL) { _, fileURL in
            if fileURL == nil { showingExportSave = false }
        }
        .confirmationDialog("Revoke \(showingRevoke?.name ?? "this device")?", isPresented: Binding(get: { showingRevoke != nil }, set: { if !$0 { showingRevoke = nil } })) {
            if let device = showingRevoke {
                Button("Revoke device", role: .destructive) { Task { await model.revoke(device, reason: "Revoked in Cylo Settings"); showingRevoke = nil } }
            }
            Button("Cancel", role: .cancel) { showingRevoke = nil }
        } message: {
            Text("Revocation blocks future requests and disconnects its event stream. The server owns the revocation result.")
        }
    }
}

private struct ScopedExportFileDocument: FileDocument {
    static var readableContentTypes: [UTType] { [.json] }
    let fileURL: URL

    init(fileURL: URL) { self.fileURL = fileURL }

    init(configuration: ReadConfiguration) throws {
        throw CyloError.invalidResponse
    }

    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        FileWrapper(regularFileWithContents: try Data(contentsOf: fileURL, options: .mappedIfSafe))
    }
}
