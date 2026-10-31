import Foundation

public struct ScreenNodePatch: Equatable, Sendable {
    public var id: String
    public var type: String?
    public var props: [String: JSONValue]?
    public var children: [ScreenNode]?
    public var emphasis: ScreenEmphasis?
    public var tone: ScreenTone?
    public var ttlSeconds: Double??
    public var transition: String??
    public var action: ScreenAction??
    public var bind: String??

    public init(id: String, type: String? = nil, props: [String: JSONValue]? = nil,
                children: [ScreenNode]? = nil, emphasis: ScreenEmphasis? = nil,
                tone: ScreenTone? = nil, ttlSeconds: Double?? = nil,
                transition: String?? = nil, action: ScreenAction?? = nil, bind: String?? = nil) {
        self.id = id; self.type = type; self.props = props; self.children = children
        self.emphasis = emphasis; self.tone = tone; self.ttlSeconds = ttlSeconds
        self.transition = transition; self.action = action; self.bind = bind
    }
}

public struct ScreenDocumentPatch: Equatable, Sendable {
    public enum Operation: Equatable, Sendable {
        case replace(id: String, node: ScreenNode)
        case update(ScreenNodePatch)
        case remove(id: String)

        public static func patch(_ patch: ScreenNodePatch) -> Operation { .update(patch) }
        public static func update(id: String, node: ScreenNode) -> Operation { .replace(id: id, node: node) }
    }

    public var revision: Int64
    public var operations: [Operation]

    public init(revision: Int64, operations: [Operation] = []) {
        self.revision = revision
        self.operations = operations
    }
}

public extension ScreenDocument {
    func applying(_ patch: ScreenDocumentPatch) -> ScreenDocument? {
        guard patch.revision > revision else { return nil }
        var result = self
        for operation in patch.operations {
            switch operation {
            case let .replace(id, node):
                _ = replace(id: id, with: node, in: &result.root)
            case let .update(nodePatch):
                _ = update(nodePatch, in: &result.root)
            case let .remove(id):
                _ = remove(id: id, from: &result.root)
            }
        }
        result.revision = patch.revision
        return result
    }

    private func replace(id: String, with node: ScreenNode, in target: inout ScreenNode) -> Bool {
        if target.id == id { target = node; return true }
        for index in target.children.indices {
            var child = target.children[index]
            if replace(id: id, with: node, in: &child) {
                target.children[index] = child
                return true
            }
        }
        return false
    }

    private func update(_ patch: ScreenNodePatch, in target: inout ScreenNode) -> Bool {
        if target.id == patch.id {
            if let value = patch.type { target.type = value }
            if let value = patch.props { target.props = value }
            if let value = patch.children { target.children = value }
            if let value = patch.emphasis { target.emphasis = value }
            if let value = patch.tone { target.tone = value }
            if let value = patch.ttlSeconds { target.ttlSeconds = value }
            if let value = patch.transition { target.transition = value }
            if let value = patch.action { target.action = value }
            if let value = patch.bind { target.bind = value }
            return true
        }
        for index in target.children.indices {
            var child = target.children[index]
            if update(patch, in: &child) {
                target.children[index] = child
                return true
            }
        }
        return false
    }

    private func remove(id: String, from target: inout ScreenNode) -> Bool {
        if target.id == id {
            target = ScreenNode(id: target.id, type: "stack")
            return true
        }
        if let index = target.children.firstIndex(where: { $0.id == id }) {
            target.children.remove(at: index)
            return true
        }
        for index in target.children.indices {
            var child = target.children[index]
            if remove(id: id, from: &child) {
                target.children[index] = child
                return true
            }
        }
        return false
    }
}
