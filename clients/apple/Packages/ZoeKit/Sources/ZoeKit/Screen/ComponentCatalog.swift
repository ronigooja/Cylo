import Foundation

public enum ComponentCatalog {
    public static let version: Int = 1

    public static let descriptors: [ComponentDescriptor] = [
        ComponentDescriptor(type: "stack", purpose: "Ordered layout of children", fallback: "stack"),
        ComponentDescriptor(type: "grid", purpose: "Grid layout", fallback: "stack"),
        ComponentDescriptor(type: "split", purpose: "Split layout", fallback: "stack"),
        ComponentDescriptor(type: "group", purpose: "Labeled group of children", fallback: "stack"),
        ComponentDescriptor(type: "spacer", purpose: "Flexible or minimum space", fallback: "spacer"),
        ComponentDescriptor(type: "canvas", purpose: "Positioned children", fallback: "stack"),
        ComponentDescriptor(type: "layer", purpose: "Children stacked on top of one another", fallback: "stack"),
        ComponentDescriptor(type: "scroll", purpose: "Scrollable children", fallback: "stack"),
        ComponentDescriptor(type: "frame", purpose: "Size-constrained children", fallback: "stack"),
        ComponentDescriptor(type: "background", purpose: "Background around children", fallback: "stack"),
        ComponentDescriptor(type: "title", purpose: "A heading", fallback: "text"),
        ComponentDescriptor(type: "text", purpose: "Plain text", fallback: "text"),
        ComponentDescriptor(type: "richText", purpose: "Markdown text", fallback: "text"),
        ComponentDescriptor(type: "list", purpose: "A list of values", fallback: "text"),
        ComponentDescriptor(type: "table", purpose: "Rows and columns of values", fallback: "text"),
        ComponentDescriptor(type: "progress", purpose: "Progress toward a value", fallback: "text"),
        ComponentDescriptor(type: "statusDot", purpose: "A compact state indicator", fallback: "text"),
        ComponentDescriptor(type: "image", purpose: "An image resource", fallback: "text"),
        ComponentDescriptor(type: "icon", purpose: "A named icon", fallback: "text"),
        ComponentDescriptor(type: "link", purpose: "A labeled URL", fallback: "text"),
        ComponentDescriptor(type: "countdown", purpose: "Time remaining until a date", fallback: "text"),
        ComponentDescriptor(type: "divider", purpose: "A visual divider", fallback: "text"),
        ComponentDescriptor(type: "button", purpose: "An action control", fallback: "text"),
        ComponentDescriptor(type: "choice", purpose: "A choice among options", fallback: "text"),
        ComponentDescriptor(type: "input", purpose: "A text or typed input", fallback: "text"),
        ComponentDescriptor(type: "toggle", purpose: "A boolean choice", fallback: "text"),
        ComponentDescriptor(type: "metric", purpose: "A value with trend", fallback: "text"),
        ComponentDescriptor(type: "timeline", purpose: "Time-ordered events", fallback: "text"),
        ComponentDescriptor(type: "steps", purpose: "Staged progress", fallback: "text"),
        ComponentDescriptor(type: "decision", purpose: "A choice needing an answer", fallback: "text"),
        ComponentDescriptor(type: "compare", purpose: "Several objects by dimension", fallback: "text"),
        ComponentDescriptor(type: "collection", purpose: "Same-kind items", fallback: "stack"),
        ComponentDescriptor(type: "article", purpose: "Long text", fallback: "text"),
        ComponentDescriptor(type: "chart", purpose: "Numeric series", fallback: "text"),
        ComponentDescriptor(type: "place", purpose: "A location", fallback: "text"),
        ComponentDescriptor(type: "media", purpose: "Image, audio, or video", fallback: "text"),
        ComponentDescriptor(type: "notice", purpose: "A one-off alert", fallback: "text"),
        ComponentDescriptor(type: "facts", purpose: "Key-value detail", fallback: "text"),
        ComponentDescriptor(type: "status", purpose: "Live state", fallback: "text"),
        ComponentDescriptor(type: "form", purpose: "User-filled fields", fallback: "text"),
        ComponentDescriptor(type: "quote", purpose: "Cited or quoted text", fallback: "text"),
        ComponentDescriptor(type: "code", purpose: "Short literal text", fallback: "text"),
        ComponentDescriptor(type: "gauge", purpose: "A value within a range", fallback: "text"),
        ComponentDescriptor(type: "calendar", purpose: "Dates and events", fallback: "text"),
        ComponentDescriptor(type: "map", purpose: "Several places or a route", fallback: "text"),
        ComponentDescriptor(type: "gallery", purpose: "Many images", fallback: "text"),
        ComponentDescriptor(type: "ranking", purpose: "Ordered scored items", fallback: "text"),
        ComponentDescriptor(type: "checklist", purpose: "Items with done state", fallback: "text")
    ]

    private static let knownTypes = Set(descriptors.map(\.type))

    public static func resolve(_ node: ScreenNode) -> ScreenNode {
        let resolvedChildren = node.children.map(resolve)
        guard !knownTypes.contains(node.type) else {
            var result = node
            result.children = resolvedChildren
            return result
        }

        var fallbackChildren: [ScreenNode] = node.props.keys.sorted().map { key in
            ScreenNode(id: "\(node.id).prop.\(key)", type: "text",
                       props: ["text": .string("\(key): \(jsonText(node.props[key]!))")])
        }
        fallbackChildren += resolvedChildren.enumerated().flatMap { offset, child in
            readableLines(child).enumerated().map { lineOffset, line in
                ScreenNode(id: "\(node.id).child.\(offset).\(lineOffset)", type: "text",
                           props: ["text": .string(line)])
            }
        }
        var result = node
        result.type = "stack"
        result.props = [:]
        result.children = fallbackChildren
        return result
    }

    private static func jsonText(_ value: JSONValue) -> String {
        switch value {
        case .null: "null"
        case let .bool(value): String(value)
        case let .number(value): String(value)
        case let .integer(value): String(value)
        case let .string(value): value
        case let .array(values): "[\(values.map(jsonText).joined(separator: ", "))]"
        case let .object(values): "{\(values.keys.sorted().map { "\($0): \(jsonText(values[$0]!))" }.joined(separator: ", "))}"
        }
    }

    private static func jsonText(_ value: [String: JSONValue]) -> String {
        jsonText(.object(value))
    }

    private static func readableLines(_ node: ScreenNode) -> [String] {
        var lines = node.props.keys.sorted().map { "\($0): \(jsonText(node.props[$0]!))" }
        if lines.isEmpty { lines.append(node.type) }
        for child in node.children {
            lines += readableLines(child).map { "\(child.type): \($0)" }
        }
        return lines
    }
}
