#if os(macOS)
import ZoeKit

/// Sample documents for visual review only; never shown as real content.
public enum DemoDocuments {
    private static func s(_ value: String) -> JSONValue { .string(value) }
    private static func n(_ value: Double) -> JSONValue { .number(value) }
    private static func o(_ fields: [String: JSONValue]) -> JSONValue { .object(fields) }
    private static func a(_ values: [JSONValue]) -> JSONValue { .array(values) }

    private static let photo = "https://picsum.photos/seed/zoe/400/260"

    public static func props(for type: String) -> [String: JSONValue] {
        switch type {
        case "metric": return ["label": s("Throughput"), "value": n(42), "unit": s("items/h"), "delta": s("+8%")]
        case "timeline": return ["events": a([
            o(["time": s("09:00"), "title": s("Started"), "detail": s("Sample event")]),
            o(["time": s("09:12"), "title": s("Checked"), "detail": s("Second sample")]),
            o(["time": s("09:30"), "title": s("Finished")])])]
        case "steps": return ["steps": a([o(["label": s("One"), "state": s("done")]), o(["label": s("Two"), "state": s("current")]), o(["label": s("Three"), "state": s("pending")])]), "current": n(1), "fraction": n(0.5)]
        case "decision": return ["question": s("Choose an option"), "options": a([o(["label": s("First")]), o(["label": s("Second")])])]
        case "compare": return ["items": a([s("Alpha"), s("Beta")]), "dimensions": a([s("Speed"), s("Cost")]),
                                "values": o(["Alpha": o(["Speed": s("Fast"), "Cost": s("High")]), "Beta": o(["Speed": s("Slow"), "Cost": s("Low")])])]
        case "collection": return ["layout": s("cards"), "items": a([s("First item"), s("Second item"), s("Third item"), s("Fourth item")])]
        case "article": return ["title": s("Sample article"), "body": s("A short **markdown** introduction."),
                                "sections": a([o(["title": s("Section"), "body": s("Section body text.")])]), "sources": a([s("Source one")])]
        case "chart": return ["series": a([o(["name": s("Series"), "points": a([
            o(["x": s("A"), "y": n(2)]), o(["x": s("B"), "y": n(5)]), o(["x": s("C"), "y": n(3)]), o(["x": s("D"), "y": n(7)])])])])]
        case "place": return ["name": s("Sample place"), "coordinate": o(["latitude": n(31.23), "longitude": n(121.47)]), "annotations": a([s("Annotation")])]
        case "map": return ["points": a([o(["name": s("Point"), "coordinate": o(["latitude": n(31.23), "longitude": n(121.47)])])])]
        case "media": return ["kind": s("image"), "url": s(photo), "caption": s("Sample caption")]
        case "notice": return ["text": s("Sample notice text")]
        case "facts": return ["pairs": a([o(["key": s("Key"), "value": s("Value")]), o(["key": s("Other"), "value": s("Thing")])])]
        case "status": return ["state": s("Running"), "description": s("Sample description"), "updatedAt": s("just now")]
        case "form": return ["fields": a([o(["key": s("a"), "label": s("Field A")]), o(["key": s("b"), "label": s("Field B")])])]
        case "quote": return ["text": s("A concise sample quote."), "source": s("Demo")]
        case "code": return ["text": s("value = 42"), "language": s("text")]
        case "gauge": return ["value": n(65), "min": n(0), "max": n(100), "zones": a([o(["label": s("Normal")])])]
        case "calendar": return ["range": s("This week"), "events": a([o(["date": s("Mon"), "title": s("Event one")]), o(["date": s("Wed"), "title": s("Event two")])])]
        case "gallery": return ["items": a([o(["url": s(photo)]), o(["url": s(photo)]), o(["url": s(photo)])])]
        case "ranking": return ["items": a([o(["label": s("First"), "score": n(9)]), o(["label": s("Second"), "score": n(7)])])]
        case "checklist": return ["items": a([o(["label": s("Done item"), "done": .bool(true)]), o(["label": s("Open item"), "done": .bool(false)])])]
        default: return ["text": s("Sample \(type)")]
        }
    }

    public static func standard(_ type: String) -> ScreenDocument {
        ScreenDocument(version: ComponentCatalog.version, revision: 1, root: ScreenNode(id: "demo.\(type)", type: type, props: props(for: type)))
    }

    /// One scrolling surface listing every standard component.
    public static var gallery: ScreenDocument {
        let types = ["metric", "gauge", "status", "notice", "decision", "steps", "timeline", "checklist", "ranking", "compare",
                     "facts", "collection", "article", "quote", "code", "chart", "calendar", "place", "map", "media", "gallery", "form"]
        let sections = types.map { type in
            ScreenNode(id: "section.\(type)", type: "group", props: ["label": s(type)], children: [
                ScreenNode(id: "demo.\(type)", type: type, props: props(for: type))])
        }
        let root = ScreenNode(id: "scroll", type: "scroll", children: [
            ScreenNode(id: "stack", type: "stack", props: ["axis": s("vertical"), "spacing": n(20)], children: sections)])
        return ScreenDocument(version: ComponentCatalog.version, revision: 1, root: root)
    }

    public static var canvas: ScreenDocument {
        ScreenDocument(version: ComponentCatalog.version, revision: 1, root: ScreenNode(id: "canvas", type: "canvas", children: [
            ScreenNode(id: "one", type: "title", props: ["text": s("Canvas"), "level": n(1)], emphasis: .high),
            ScreenNode(id: "two", type: "statusDot", props: ["label": s("Layer"), "state": s("ok")], tone: .success)]))
    }
}
#endif
