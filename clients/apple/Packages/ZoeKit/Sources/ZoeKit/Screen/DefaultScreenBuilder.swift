import Foundation

public enum DefaultScreenBuilder {
    public static func build(tasks: [TaskRecord], approvals: [Approval], notifications: [NotificationRecord], revision: Int64) -> ScreenDocument {
        struct Item {
            let date: String
            let node: ScreenNode
        }
        var items: [Item] = []
        items += tasks.map { task in
            Item(date: task.updatedAt, node: item(id: "task.\(task.id)", title: task.title, status: task.status.label))
        }
        items += approvals.map { approval in
            Item(date: approval.createdAt, node: item(id: "approval.\(approval.id)", title: approval.prompt, status: approval.status))
        }
        items += notifications.map { notification in
            Item(date: notification.createdAt, node: item(id: "notification.\(notification.id)", title: notification.title, status: notification.kind))
        }
        let children = items.sorted {
            $0.date == $1.date ? $0.node.id < $1.node.id : $0.date > $1.date
        }.map(\.node)
        return ScreenDocument(version: ComponentCatalog.version, revision: revision,
                              root: ScreenNode(id: "root", type: "stack", children: children))
    }

    private static func item(id: String, title: String, status: String) -> ScreenNode {
        ScreenNode(id: id, type: "collection", children: [
            ScreenNode(id: "\(id).title", type: "text", props: ["text": .string(title)]),
            ScreenNode(id: "\(id).status", type: "statusDot", props: ["state": .string(status), "label": .string(status)])
        ])
    }
}
