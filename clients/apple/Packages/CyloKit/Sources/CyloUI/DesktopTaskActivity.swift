#if os(macOS)
import CyloKit
import SwiftUI

struct DesktopTaskActivity: View {
    @ObservedObject var model: CyloAppModel
    let openTask: (String) -> Void
    let openTasks: () -> Void

    private var groups: TaskActivityGroups {
        TaskActivityGroups(tasks: model.tasks, approvals: model.approvals)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                if !groups.attention.isEmpty {
                    activitySection("Needs attention", tasks: groups.attention, limit: 6)
                }
                activitySection("In progress", tasks: groups.inProgress, limit: 8)
                if !groups.completed.isEmpty {
                    activitySection("Recently completed", tasks: groups.completed, limit: 4)
                }
            }
            .padding(.horizontal, 12).padding(.vertical, 20)
        }
        .background(.background)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(uiText("Task activity"))
    }

    private func activitySection(_ title: String, tasks: [TaskRecord], limit: Int) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(uiText(title)).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                .padding(.horizontal, 10).padding(.bottom, 4)
            if tasks.isEmpty {
                Text(uiText("No active tasks")).font(.caption).foregroundStyle(.tertiary)
                    .padding(.horizontal, 10).padding(.vertical, 6)
            }
            ForEach(tasks.prefix(limit)) { task in
                Button { openTask(task.id) } label: {
                    HStack(alignment: .top, spacing: 8) {
                        Image(systemName: taskStatusSymbol(task.status))
                            .font(.caption).foregroundStyle(taskStatusColor(task.status))
                            .frame(width: 16).padding(.top, 2).accessibilityHidden(true)
                        VStack(alignment: .leading, spacing: 5) {
                            Text(task.title).font(.callout.weight(.medium)).foregroundStyle(.primary)
                                .lineLimit(2).multilineTextAlignment(.leading)
                            Text(model.projectName(task.projectId)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            Text(uiText(task.status.label)).font(.caption).foregroundStyle(taskStatusColor(task.status))
                        }
                        Spacer(minLength: 0)
                    }
                    .padding(10).frame(maxWidth: .infinity, alignment: .leading)
                    .contentShape(Rectangle())
                }
                .buttonStyle(AssistantRowStyle())
            }
            if tasks.count > limit {
                Button(uiText("View all"), action: openTasks)
                    .font(.caption).buttonStyle(.borderless).padding(.horizontal, 10)
            }
        }
    }
}
#endif
