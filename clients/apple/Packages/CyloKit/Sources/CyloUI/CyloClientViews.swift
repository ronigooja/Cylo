import CyloKit
import SwiftUI

enum MainArea: String, CaseIterable, Identifiable {
    case conversation, tasks, projects
    var id: String { rawValue }
    var title: String { switch self { case .conversation: "Conversation"; case .tasks: "Tasks"; case .projects: "Projects" } }
    var symbol: String { switch self { case .conversation: "bubble.left.and.bubble.right"; case .tasks: "checklist"; case .projects: "folder" } }
}

enum WorkspaceRoute: Hashable {
    case task(String)
    case project(String)
}

struct OpenTaskKey: EnvironmentKey {
    static let defaultValue: (String) -> Void = { _ in }
}
extension EnvironmentValues {
    var openTask: (String) -> Void {
        get { self[OpenTaskKey.self] }
        set { self[OpenTaskKey.self] = newValue }
    }
}

public struct CyloRootView: View {
    @ObservedObject private var model: CyloAppModel
    @State private var area: MainArea? = .conversation
    @State private var paths: [MainArea: [WorkspaceRoute]] = [:]
    @State private var contextRevision = UUID()
    @State private var showAccount = false
    @State private var showInbox = false
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var sizeClass
    #endif
    public init(model: CyloAppModel) { self.model = model }

    public var body: some View {
        Group {
            #if os(iOS)
            if sizeClass == .compact { phoneNavigation } else { splitNavigation }
            #else
            splitNavigation
            #endif
        }
        .sheet(isPresented: $showAccount) { NavigationStack { AccountView(model: model).toolbar { ToolbarItem(placement: .confirmationAction) { Button(uiText("Done")) { showAccount = false } } } } }
        .sheet(isPresented: $showInbox) { NavigationStack { NotificationInboxView(model: model, openTask: { id in showInbox = false; area = .conversation; paths[.conversation] = [.task(id)] }).toolbar { ToolbarItem(placement: .confirmationAction) { Button(uiText("Done")) { showInbox = false } } } } }
        .onChange(of: model.connectionEpoch) { _, _ in resetTaskNavigation() }
        .onChange(of: model.snapshot?.authority) { old, new in if let old, let new, old.instanceLineageId != new.instanceLineageId || old.generation != new.generation { resetTaskNavigation() } }
        .task { await model.start() }
    }
    private var phoneNavigation: some View {
        TabView(selection: Binding(get: { area ?? .conversation }, set: { area = $0 })) {
            ForEach(MainArea.allCases) { item in
                NavigationStack(path: path(for: item)) { screen(item) }.toolbar { utilityToolbar }
                    .tabItem { Label(uiText(item.title), systemImage: item.symbol) }.tag(item)
            }
        }
    }
    private var splitNavigation: some View {
        NavigationSplitView {
            List(selection: $area) {
                ForEach(MainArea.allCases) { item in Label(uiText(item.title), systemImage: item.symbol).tag(item) }
                Section(uiText("Recent tasks")) {
                    ForEach(model.tasks.prefix(6)) { task in
                        Button { area = .conversation; paths[.conversation] = [.task(task.id)] } label: {
                            Text(task.title).lineLimit(2).foregroundStyle(.primary)
                        }.buttonStyle(.plain)
                    }
                }
            }
            .navigationTitle(uiText("Cylo"))
            .navigationSplitViewColumnWidth(min: 220, ideal: 240)
            .safeAreaInset(edge: .bottom) {
                Button { showAccount = true } label: { Label(uiText("Account & Settings"), systemImage: "person.crop.circle").frame(maxWidth: .infinity, alignment: .leading) }.buttonStyle(.plain).padding()
            }
        } detail: {
            NavigationStack(path: path(for: area ?? .conversation)) { screen(area ?? .conversation) }.toolbar { utilityToolbar }
        }
    }
    @ViewBuilder private func screen(_ item: MainArea) -> some View {
        Group {
            switch item {
            case .conversation: ConversationView(model: model)
            case .tasks: TasksView(model: model)
            case .projects: ProjectsView(model: model)
            }
        }
        .navigationDestination(for: WorkspaceRoute.self) { route in
            switch route {
            case let .task(id): TaskDetailView(taskID: id, model: model)
            case let .project(id):
                if let project = model.projects.first(where: { $0.id == id }) { ProjectDetailView(project: project, model: model) }
                else { ContentUnavailableView(uiText("Project unavailable"), systemImage: "folder.badge.questionmark") }
            }
        }
        .id(contextRevision)
        .environment(\.openTask, { id in paths[item, default: []].append(.task(id)) })
    }
    private func resetTaskNavigation() {
        contextRevision = UUID()
        paths = [:]
        area = .conversation
        showInbox = false
    }
    private func path(for item: MainArea) -> Binding<[WorkspaceRoute]> {
        Binding(get: { paths[item] ?? [] }, set: { paths[item] = $0 })
    }
    @ToolbarContentBuilder private var utilityToolbar: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            Button {
                area = .conversation
                paths[.conversation] = []
            } label: { Label(uiText("New conversation"), systemImage: "square.and.pencil") }
                .keyboardShortcut("n", modifiers: .command)
            Button { showInbox = true } label: {
                Label(uiText("Notifications"), systemImage: "bell")
                    .overlay(alignment: .topTrailing) {
                        let count = model.notifications.filter { $0.readAt == nil }.count
                        if count > 0 { Text(count > 99 ? "99+" : count.formatted()).font(.system(size: 9, weight: .bold)).foregroundStyle(.white).padding(3).background(.red, in: Capsule()).offset(x: 8, y: -8).accessibilityHidden(true) }
                    }
            }.accessibilityValue(model.notifications.filter { $0.readAt == nil }.count.formatted())
            Button { showAccount = true } label: { Label(uiText("Account & Settings"), systemImage: "person.crop.circle") }
        }
    }
}
