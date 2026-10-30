#if os(macOS)
import ZoeKit

public enum DemoDocuments {
    public static func standard(_ type: String) -> ScreenDocument {
        let props: [String: JSONValue]
        switch type {
        case "metric": props = ["label": .string("Throughput"), "value": .number(42), "unit": .string("items"), "delta": .string("+8%")]
        case "timeline": props = ["events": .array([.object(["time": .string("09:00"), "title": .string("Started"), "detail": .string("Sample event")])])]
        case "steps": props = ["steps": .array([.object(["label": .string("One"), "state": .string("done")]), .object(["label": .string("Two"), "state": .string("current")])]), "current": .number(1), "fraction": .number(0.5)]
        case "decision": props = ["question": .string("Choose an option"), "options": .array([.object(["label": .string("First")]), .object(["label": .string("Second")])])]
        case "chart": props = ["series": .array([.object(["name": .string("Series"), "points": .array([.object(["x": .string("A"), "y": .number(2)]), .object(["x": .string("B"), "y": .number(5)])])])])]
        case "quote": props = ["text": .string("A concise sample quote."), "source": .string("Demo")]
        case "code": props = ["text": .string("value = 42"), "language": .string("text")]
        case "checklist": props = ["items": .array([.object(["label": .string("Review surface"), "done": .bool(true)]), .object(["label": .string("Try action"), "done": .bool(false)])])]
        default: props = ["text": .string("Sample \(type) component")]
        }
        return ScreenDocument(version: ComponentCatalog.version, revision: 1, root: ScreenNode(id: "demo.\(type)", type: type, props: props))
    }

    public static var canvas: ScreenDocument {
        ScreenDocument(version: ComponentCatalog.version, revision: 1, root: ScreenNode(id: "canvas", type: "canvas", children: [ScreenNode(id: "one", type: "text", props: ["text": .string("Canvas")]), ScreenNode(id: "two", type: "statusDot", props: ["label": .string("Layer")])]))
    }
}
#endif
