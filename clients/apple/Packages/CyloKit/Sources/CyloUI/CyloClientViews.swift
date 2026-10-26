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
    #if os(macOS)
    @State private var showTaskActivity = true
    @State private var sidebarQuery = ""
    #endif
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var sizeClass
    #endif
    public init(model: CyloAppModel) { self.model = model }

    public var body: some View {
        Group {
            if !model.authentication.isConnected {
                InstanceConnectionView(model: model)
            } else {
            #if os(iOS)
            if sizeClass == .compact { phoneNavigation } else { splitNavigation }
            #else
            splitNavigation
            #endif
            }
        }
        .sheet(isPresented: $showAccount) {
            NavigationStack {
                AccountView(model: model)
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) {
                            Button(uiText("Done")) { showAccount = false }
                        }
                    }
            }
            #if os(macOS)
            .frame(minWidth: 560, idealWidth: 640, minHeight: 520, idealHeight: 680)
            #endif
        }
        .sheet(isPresented: $showInbox) { NavigationStack { NotificationInboxView(model: model, openTask: { id in showInbox = false; area = .conversation; paths[.conversation] = [.task(id)] }).toolbar { ToolbarItem(placement: .confirmationAction) { Button(uiText("Done")) { showInbox = false } } } } }
        .sheet(isPresented: Binding(get: { model.pendingConnectionImport != nil }, set: { if !$0 { model.dismissConnectionImport() } })) {
            InstanceConnectionView(model: model)
        }
        .alert(uiText("Installation link"), isPresented: Binding(get: { model.connectionImportError != nil }, set: { if !$0 { model.dismissConnectionImportError() } })) {
            Button(uiText("Done"), role: .cancel) { model.dismissConnectionImportError() }
        } message: { Text(model.connectionImportError ?? "") }
        .onChange(of: model.connectionEpoch) { _, _ in
            resetTaskNavigation()
            if !model.authentication.isConnected { showAccount = false; showInbox = false }
        }
        .onChange(of: model.authentication) { _, state in
            if !state.isConnected { showAccount = false; showInbox = false }
        }
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
            #if os(macOS)
            desktopSidebar
            #else
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
            #endif
        } detail: {
            #if os(macOS)
            GeometryReader { geometry in
                HStack(spacing: 0) {
                    NavigationStack(path: path(for: area ?? .conversation)) { screen(area ?? .conversation) }
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        .toolbar {
                            utilityToolbar
                            ToolbarItem(placement: .primaryAction) {
                                Button { showTaskActivity.toggle() } label: {
                                    Label(uiText("Task activity"), systemImage: "sidebar.right")
                                }
                                .help(uiText("Task activity"))
                                .accessibilityValue(showTaskActivity ? uiText("Shown") : uiText("Hidden"))
                                .disabled(geometry.size.width < 960 || !isConversationWorkspace)
                            }
                        }
                    if showTaskActivity && geometry.size.width >= 960 && isConversationWorkspace {
                        Divider()
                        DesktopTaskActivity(model: model, openTask: openDesktopTask, openTasks: {
                            area = .tasks
                            paths[.tasks] = []
                        })
                            .frame(width: 272)
                    }
                }
            }
            #else
            NavigationStack(path: path(for: area ?? .conversation)) { screen(area ?? .conversation) }.toolbar { utilityToolbar }
            #endif
        }
    }
    #if os(macOS)
    private enum DesktopSelection: Hashable {
        case area(MainArea)
        case task(String)
        case project(String)
    }

    private var desktopSelection: Binding<DesktopSelection?> {
        Binding(get: {
            switch paths[area ?? .conversation]?.last {
            case let .task(id): return .task(id)
            case let .project(id): return .project(id)
            case nil: return .area(area ?? .conversation)
            }
        }, set: { selection in
            switch selection {
            case let .area(item): area = item; paths[item] = []
            case let .task(id): openDesktopTask(id)
            case let .project(id): area = .projects; paths[.projects] = [.project(id)]
            case nil: break
            }
        })
    }

    private var desktopSidebar: some View {
        List(selection: desktopSelection) {
            ForEach(MainArea.allCases) { item in
                Label(uiText(item.title), systemImage: item.symbol)
                    .padding(.vertical, 2).tag(DesktopSelection.area(item))
            }
            if !sidebarTasks.isEmpty {
                Section(uiText("Recent tasks")) {
                    ForEach(sidebarTasks.prefix(12)) { task in
                        HStack(alignment: .top, spacing: 8) {
                            Image(systemName: taskStatusSymbol(task.status))
                                .foregroundStyle(taskStatusColor(task.status)).frame(width: 16)
                                .accessibilityHidden(true)
                            Text(task.title).lineLimit(2)
                        }
                        .font(.callout).padding(.vertical, 3)
                        .accessibilityValue(uiText(task.status.label))
                        .tag(DesktopSelection.task(task.id))
                    }
                }
            }
            if !sidebarProjects.isEmpty {
                Section(uiText("Projects")) {
                    ForEach(sidebarProjects) { project in
                        Label(project.name, systemImage: "folder").lineLimit(1)
                            .tag(DesktopSelection.project(project.id))
                    }
                }
            }
        }
        .listStyle(.sidebar)
        .safeAreaInset(edge: .top, spacing: 0) {
            VStack(spacing: 12) {
                HStack(spacing: 9) {
                    Image(systemName: "bubble.left.and.bubble.right.fill")
                        .foregroundStyle(.tint).font(.title3)
                    Text(uiText("Cylo")).font(.title3.weight(.semibold))
                    Spacer()
                    Button { newConversation() } label: {
                        Label(uiText("New conversation"), systemImage: "square.and.pencil")
                            .labelStyle(.iconOnly).frame(width: 28, height: 28)
                    }.buttonStyle(.plain).help(uiText("New conversation"))
                }
                HStack(spacing: 6) {
                    Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
                    TextField(uiText("Search"), text: $sidebarQuery).textFieldStyle(.plain)
                    if !sidebarQuery.isEmpty {
                        Button { sidebarQuery = "" } label: {
                            Label(uiText("Clear search"), systemImage: "xmark.circle.fill").labelStyle(.iconOnly)
                        }.buttonStyle(.plain).foregroundStyle(.secondary)
                    }
                }
                .font(.callout).padding(7)
                .background(.background, in: RoundedRectangle(cornerRadius: 7))
            }.padding(.horizontal, 14).padding(.top, 10).padding(.bottom, 8)
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            VStack(spacing: 0) {
                Divider().padding(.horizontal, 12)
                Button { showAccount = true } label: {
                    HStack(spacing: 9) {
                        Image(systemName: "person.crop.circle").font(.title3)
                        Text(uiText("Account & Settings")).font(.callout)
                        Spacer()
                        Image(systemName: "gearshape").font(.callout)
                    }
                    .foregroundStyle(.secondary).padding(12).contentShape(Rectangle())
                }.buttonStyle(.plain)
            }
        }
        .navigationSplitViewColumnWidth(min: 200, ideal: 240, max: 300)
    }

    private var sidebarTasks: [TaskRecord] {
        model.tasks.filter { sidebarQuery.isEmpty || $0.title.localizedCaseInsensitiveContains(sidebarQuery) }
    }
    private var sidebarProjects: [Project] {
        model.projects.filter { sidebarQuery.isEmpty || $0.name.localizedCaseInsensitiveContains(sidebarQuery) }
    }
    private var isConversationWorkspace: Bool {
        if area == .conversation { return true }
        if case .task = paths[area ?? .conversation]?.last { return true }
        return false
    }
    private func openDesktopTask(_ id: String) {
        area = .conversation
        paths[.conversation] = [.task(id)]
    }
    #endif
    private func newConversation() {
        area = .conversation
        paths[.conversation] = []
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
        #if os(macOS)
        sidebarQuery = ""
        #endif
    }
    private func path(for item: MainArea) -> Binding<[WorkspaceRoute]> {
        Binding(get: { paths[item] ?? [] }, set: { paths[item] = $0 })
    }
    @ToolbarContentBuilder private var utilityToolbar: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            Button { newConversation() } label: { Label(uiText("New conversation"), systemImage: "square.and.pencil") }
                .keyboardShortcut("n", modifiers: .command)
            Button { showInbox = true } label: {
                Label(uiText("Notifications"), systemImage: "bell")
                    .overlay(alignment: .topTrailing) {
                        let count = model.notifications.filter { $0.readAt == nil }.count
                        if count > 0 { Text(count > 99 ? "99+" : count.formatted()).font(.system(size: 9, weight: .bold)).foregroundStyle(.white).padding(3).background(.red, in: Capsule()).offset(x: 8, y: -8).accessibilityHidden(true) }
                    }
            }.accessibilityValue(model.notifications.filter { $0.readAt == nil }.count.formatted())
            #if !os(macOS)
            Button { showAccount = true } label: { Label(uiText("Account & Settings"), systemImage: "person.crop.circle") }
            #endif
        }
    }
}
