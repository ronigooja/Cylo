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
            #if os(macOS)
            macSidebar
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
            NavigationStack(path: path(for: area ?? .conversation)) { screen(area ?? .conversation) }.toolbar { utilityToolbar }
        }
    }
    #if os(macOS)
    private var macSidebar: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                Image(systemName: "bubble.left.and.bubble.right.fill")
                    .font(.system(size: 20, weight: .medium)).foregroundStyle(.tint)
                Text(uiText("Cylo")).font(.system(size: 21, weight: .semibold, design: .rounded))
            }
            .padding(.horizontal, 20).padding(.top, 18).padding(.bottom, 22)

            Button { newConversation() } label: {
                Label(uiText("New conversation"), systemImage: "square.and.pencil")
                    .font(.system(size: 13, weight: .medium))
                    .frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 5)
            }
            .buttonStyle(.bordered).controlSize(.large)
            .padding(.horizontal, 16).padding(.bottom, 20)

            ScrollView {
                VStack(alignment: .leading, spacing: 4) {
                    ForEach(MainArea.allCases) { item in
                        sidebarButton(selected: area == item && (paths[item] ?? []).isEmpty) {
                            area = item
                            paths[item] = []
                        } label: {
                            Label(uiText(item.title), systemImage: item.symbol)
                                .font(.system(size: 13, weight: .medium))
                        }
                    }
                    if !model.tasks.isEmpty {
                        Text(uiText("Recent tasks"))
                            .font(.system(size: 11, weight: .semibold)).foregroundStyle(.secondary)
                            .padding(.horizontal, 12).padding(.top, 24).padding(.bottom, 7)
                        ForEach(model.tasks.prefix(6)) { task in
                            sidebarButton(selected: selectedSidebarTask == task.id) {
                                area = .conversation
                                paths[.conversation] = [.task(task.id)]
                            } label: {
                                HStack(alignment: .top, spacing: 10) {
                                    Image(systemName: "bubble.left").foregroundStyle(.secondary).frame(width: 16)
                                    Text(task.title).lineLimit(2).multilineTextAlignment(.leading)
                                }.font(.system(size: 13))
                            }
                        }
                    }
                }.padding(.horizontal, 12)
            }
            Divider().padding(.horizontal, 16)
            Button { showAccount = true } label: {
                HStack(spacing: 10) {
                    Image(systemName: "person.crop.circle").font(.system(size: 24)).foregroundStyle(.secondary)
                    Text(uiText("Account & Settings")).font(.system(size: 12, weight: .medium))
                    Spacer(minLength: 0)
                    Image(systemName: "gearshape").font(.system(size: 12)).foregroundStyle(.secondary)
                }
                .padding(12).contentShape(Rectangle())
            }.buttonStyle(.plain).padding(8)
        }
        .navigationSplitViewColumnWidth(min: 220, ideal: 250, max: 300)
    }
    private var selectedSidebarTask: String? {
        guard let route = paths[area ?? .conversation]?.last, case let .task(id) = route else { return nil }
        return id
    }
    private func sidebarButton<Content: View>(selected: Bool, action: @escaping () -> Void, @ViewBuilder label: () -> Content) -> some View {
        Button(action: action) {
            label()
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 12).padding(.vertical, 10)
                .contentShape(RoundedRectangle(cornerRadius: 8))
        }
        .buttonStyle(.plain)
        .foregroundStyle(selected ? Color.accentColor : Color.primary)
        .background(selected ? Color.accentColor.opacity(0.12) : Color.clear, in: RoundedRectangle(cornerRadius: 8))
        .accessibilityAddTraits(selected ? .isSelected : [])
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
