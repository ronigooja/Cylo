import CyloKit
import SwiftUI

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
                if !model.tasks.isEmpty {
                    Picker("Continue a task", selection: $selectedTaskID) {
                        Text("Choose a task").tag(String?.none)
                        ForEach(model.tasks) { task in Text(task.title).tag(Optional(task.id)) }
                    }
                    .pickerStyle(.menu)
                    .padding(.horizontal)
                }
            }
            composer
        }
        .navigationTitle("Conversation")
    }

    private var composer: some View {
        VStack(alignment: .leading, spacing: 8) {
            if model.task(id: selectedTaskID) == nil, !model.projects.isEmpty {
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
                Button {
                    let message = draft.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !message.isEmpty else { return }
                    draft = ""
                    Task {
                        if let task = model.task(id: selectedTaskID) {
                            await model.append(to: task, content: message)
                        } else {
                            await model.createTask(goal: message, title: String(message.prefix(72)), projectID: projectID.isEmpty ? nil : projectID)
                        }
                    }
                } label: { Label("Send", systemImage: "arrow.up.circle.fill") }
                    .disabled(model.isBusy || draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            Label("Live voice is unavailable until the pinned WebRTC transport is built and configured.", systemImage: "mic.slash")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
        .padding()
        .background(.regularMaterial)
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
            NavigationLink { MemoryView(model: model) } label: { Label("Memory", systemImage: "brain.head.profile") }
            NavigationLink { CapabilitiesView(model: model) } label: { Label("Capabilities", systemImage: "puzzlepiece.extension") }
            NavigationLink { SettingsView(model: model) } label: { Label("Settings", systemImage: "gearshape") }
        }
        .navigationTitle("More")
    }
}

private struct MemoryView: View {
    @ObservedObject var model: CyloAppModel
    var body: some View {
        List {
            FreshnessBanner(model: model)
            ContentUnavailableView("Memory service unavailable", systemImage: "brain.head.profile", description: Text("This server has not reported a connected memory authority. Memory will appear here only from an authoritative server response."))
        }
        .navigationTitle("Memory")
    }
}

private struct CapabilitiesView: View {
    @ObservedObject var model: CyloAppModel
    var body: some View {
        List {
            FreshnessBanner(model: model)
            ContentUnavailableView("Capabilities unavailable", systemImage: "puzzlepiece.extension", description: Text("No capability inventory has been returned by this server."))
        }
        .navigationTitle("Capabilities")
    }
}

private struct SettingsView: View {
    @ObservedObject var model: CyloAppModel
    @State private var deviceName = "Apple device"
    @State private var bootstrapCode = ""
    @State private var showingRevoke: Device?

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
            if let notice = model.notice { Section("Latest update") { Text(notice).textSelection(.enabled) } }
        }
        .navigationTitle("Settings")
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
