import CyloKit
import SwiftUI

struct ProjectsView: View {
    @ObservedObject var model: CyloAppModel
    @State private var showingCreate = false

    var body: some View {
        VStack(spacing: 8) {
            FreshnessBanner(model: model).padding(.horizontal)
            List(model.projects) { project in
                NavigationLink(value: WorkspaceRoute.project(project.id)) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(project.name).font(.headline)
                        Text("\(uiText(project.status.capitalized)) · \(uiTaskCount(model.tasks.filter { $0.projectId == project.id }.count))")
                            .font(.caption).foregroundStyle(.secondary)
                        if let description = project.description { Text(description).font(.subheadline).foregroundStyle(.secondary) }
                    }
                }
            }
            .overlay { if model.projects.isEmpty { ContentUnavailableView(uiText("No projects yet"), systemImage: "folder") } }
        }
        .navigationTitle(uiText("Projects"))
        .toolbar { ToolbarItem(placement: .primaryAction) { Button(uiText("New project"), systemImage: "plus") { showingCreate = true } } }
        .sheet(isPresented: $showingCreate) { CreateProjectSheet(model: model) }
    }
}

struct ProjectDetailView: View {
    let project: Project
    @ObservedObject var model: CyloAppModel
    @Environment(\.openTask) private var openTask
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                if let description = project.description, !description.isEmpty { Text(description).foregroundStyle(.secondary) }
                TaskComposer(model: model, taskID: nil, projectID: project.id, accepted: openTask)
                Text(uiText("Tasks")).font(.headline)
                let tasks = model.tasks.filter { $0.projectId == project.id }
                if tasks.isEmpty { ContentUnavailableView(uiText("No tasks yet"), systemImage: "checklist") }
                ForEach(tasks) { task in NavigationLink(value: WorkspaceRoute.task(task.id)) { TaskSummary(task: task, model: model) }.buttonStyle(.plain); Divider() }
            }.frame(maxWidth: 760).padding(24).frame(maxWidth: .infinity)
        }
        .navigationTitle(project.name)
    }
}

struct CreateProjectSheet: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CyloAppModel
    @State private var name = ""
    @State private var details = ""
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                TextField(uiText("Name"), text: $name)
                TextField(uiText("Description (optional)"), text: $details, axis: .vertical).lineLimit(3...6)
                NoticeCard(message: error)
            }
            .navigationTitle(uiText("New project"))
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(uiText("Cancel")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(uiText("Create")) {
                        Task {
                            error = nil
                            if await model.createProject(name: name, description: details.isEmpty ? nil : details) { dismiss() }
                            else { error = model.notice ?? uiText("The project was not accepted. Your entries are still here.") }
                        }
                    }.disabled(name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || model.isBusy)
                }
            }
        }
        .frame(idealWidth: 340, idealHeight: 260)
    }
}

struct AccountView: View {
    @ObservedObject var model: CyloAppModel
    var body: some View {
        List {
            NavigationLink { MemoryView(model: model) } label: { Label(uiText("Memory"), systemImage: "brain.head.profile") }
            NavigationLink { CapabilitiesView(model: model) } label: { Label(uiText("Capabilities"), systemImage: "puzzlepiece.extension") }
            NavigationLink { CyloSettingsView(model: model) } label: { Label(uiText("Settings"), systemImage: "gearshape") }
        }
        .navigationTitle(uiText("Account & Settings"))
    }
}

struct NotificationInboxView: View {
    @ObservedObject var model: CyloAppModel
    var openTask: ((String) -> Void)? = nil
    @State private var unreadOnly = false

    var body: some View {
        List {
            FreshnessBanner(model: model)
            Toggle(uiText("Unread only"), isOn: $unreadOnly)
            if model.notifications.isEmpty {
                ContentUnavailableView(uiText("Inbox is empty"), systemImage: "bell")
            }
            ForEach(model.notifications) { item in
                if item.aggregateType == "task", let openTask {
                    Button {
                        if item.readAt == nil { Task { await model.markNotificationRead(item) } }
                        openTask(item.aggregateId)
                    } label: { notificationRow(item) }.buttonStyle(.plain)
                } else {
                    NavigationLink { NotificationDetailView(notification: item, model: model) } label: { notificationRow(item) }
                }
            }
        }
        .navigationTitle(uiText("Inbox"))
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                Button(uiText("Refresh"), systemImage: "arrow.clockwise") { Task { await model.refreshNotifications(unreadOnly: unreadOnly) } }
            }
        }
        .task { await model.refreshNotifications(unreadOnly: unreadOnly) }
        .onChange(of: unreadOnly) { _, value in Task { await model.refreshNotifications(unreadOnly: value) } }
        .refreshable { await model.refreshNotifications(unreadOnly: unreadOnly) }
    }
    private func notificationRow(_ item: NotificationRecord) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack {
                Text(item.title).font(.headline)
                Spacer()
                if item.readAt == nil { Circle().fill(.blue).frame(width: 8, height: 8).accessibilityLabel(uiText("Unread")) }
            }
            Text(item.body).font(.subheadline).foregroundStyle(.secondary).lineLimit(3)
            Text(uiDate(item.createdAt)).font(.caption2).foregroundStyle(.tertiary)
        }.padding(.vertical, 4)
    }
}

struct NotificationDetailView: View {
    let notification: NotificationRecord
    @ObservedObject var model: CyloAppModel

    var body: some View {
        Group {
            if notification.aggregateType == "task", let task = model.task(id: notification.aggregateId) {
                TaskDetailView(taskID: task.id, model: model)
            } else {
                List {
                    Section(notification.title) {
                        Text(notification.body).textSelection(.enabled)
                        LabeledContent(uiText("Kind"), value: notification.kind)
                        LabeledContent(uiText("Record"), value: "\(notification.aggregateType) · \(notification.aggregateId)")
                        LabeledContent(uiText("Source event"), value: notification.sourceEventId).textSelection(.enabled)
                    }
                    if notification.readAt == nil {
                        Button(uiText("Mark as read")) { Task { await model.markNotificationRead(notification) } }
                    }
                }
            }
        }
        .task {
            if notification.readAt == nil { await model.markNotificationRead(notification) }
        }
    }
}

struct MemoryView: View {
    @ObservedObject var model: CyloAppModel
    @State private var showingCreate = false
    @State private var query = ""

    var body: some View {
        List {
            FreshnessBanner(model: model)
            if !model.memoryMissing.isEmpty {
                Section(uiText("Source unavailable")) { ForEach(model.memoryMissing, id: \.self) { Text($0).font(.caption).textSelection(.enabled) } }
            }
            if !model.memoryConflicts.isEmpty {
                Section(uiText("Conflicting records")) { ForEach(model.memoryConflicts, id: \.self) { Text($0).foregroundStyle(.orange) } }
            }
            Section(uiText("Authoritative records")) {
                if model.memoryClaims.isEmpty { ContentUnavailableView(uiText("No matching memories"), systemImage: "brain.head.profile") }
                ForEach(model.memoryClaims) { claim in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(claim.subject).font(.headline)
                        Text(claim.content).textSelection(.enabled)
                        Text("\(claim.scope.kind.capitalized) · \(claim.authorityKind.replacingOccurrences(of: "_", with: " ")) · revision \(claim.revision)")
                            .font(.caption).foregroundStyle(.secondary)
                        ForEach(claim.sources) { source in
                            Text("Source: \(source.kind) · \(source.reference)").font(.caption2).foregroundStyle(.secondary).textSelection(.enabled)
                        }
                        Button(uiText("Delete memory"), role: .destructive) { Task { await model.deleteMemory(claim) } }
                            .disabled(model.isBusy)
                    }.padding(.vertical, 4)
                }
            }
        }
        .searchable(text: $query, prompt: uiText("Search memory"))
        .onSubmit(of: .search) { Task { await model.refreshMemory(query: query) } }
        .task { await model.refreshMemory(query: query) }
        .toolbar { ToolbarItem(placement: .primaryAction) { Button(uiText("Remember"), systemImage: "plus") { showingCreate = true } } }
        .sheet(isPresented: $showingCreate) { CreateMemorySheet(model: model) }
        .navigationTitle(uiText("Memory"))
    }
}

struct CreateMemorySheet: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CyloAppModel
    @State private var scope = "personal"
    @State private var scopeID = ""
    @State private var subject = ""
    @State private var key = "memory-" + UUID().uuidString.lowercased()
    @State private var content = ""
    @State private var error: String?

    var body: some View {
        NavigationStack {
            Form {
                Picker(uiText("Scope"), selection: $scope) {
                    Text(uiText("Personal")).tag("personal")
                    Text(uiText("Workspace")).tag("workspace")
                    Text(uiText("Project")).tag("project")
                    Text(uiText("Task")).tag("task")
                }
                if scope == "project" || scope == "task" {
                    Picker(uiText(scope == "project" ? "Project" : "Task"), selection: $scopeID) {
                        Text(uiText("Choose…")).tag("")
                        if scope == "project" { ForEach(model.projects) { Text($0.name).tag($0.id) } }
                        else { ForEach(model.tasks) { Text($0.title).tag($0.id) } }
                    }
                }
                TextField(uiText("Subject"), text: $subject)
                DisclosureGroup(uiText("Advanced")) { TextField(uiText("Memory key"), text: $key) }
                TextField(uiText("Remember"), text: $content, axis: .vertical).lineLimit(4...10)
                NoticeCard(message: error)
            }
            .navigationTitle(uiText("Remember"))
        .onChange(of: scope) { _, _ in scopeID = "" }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(uiText("Cancel")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(uiText("Save")) {
                        Task {
                            error = nil
                            if await model.remember(scope: scope, scopeID: scopeID.isEmpty ? nil : scopeID, subject: subject, key: key, content: content) { dismiss() }
                            else { error = model.notice ?? uiText("The memory was not accepted. Your entries are still here.") }
                        }
                    }
                    .disabled(model.isBusy || subject.isEmpty || key.isEmpty || content.isEmpty || ((scope == "project" || scope == "task") && scopeID.isEmpty))
                }
            }
        }
        .frame(idealWidth: 360, idealHeight: 420)
    }
}
