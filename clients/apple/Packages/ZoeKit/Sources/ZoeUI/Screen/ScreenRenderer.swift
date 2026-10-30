#if os(macOS)
import SwiftUI
import ZoeKit
import Charts
import MapKit

public final class ScreenDataStore: ObservableObject {
    @Published public private(set) var sources: [String: JSONValue]

    public init(document: ScreenDocument = ScreenDocument(version: 1, revision: 0, root: ScreenNode(id: "empty", type: "group"), data: [:]), sources: [String: JSONValue] = [:]) {
        self.sources = document.data.merging(sources) { _, new in new }
    }

    public func replace(document: ScreenDocument) { sources = document.data }
    public func set(_ value: JSONValue, for key: String) { sources[key] = value }
    public func value(for key: String) -> JSONValue? { sources[key] }
}

public final class ScreenRenderContext: ObservableObject {
    public let store: ScreenDataStore
    public let perform: (ScreenAction) -> Void

    public init(store: ScreenDataStore, perform: @escaping (ScreenAction) -> Void = { _ in }) {
        self.store = store
        self.perform = perform
    }
}

public struct ScreenRenderer: View {
    public let document: ScreenDocument
    @ObservedObject private var store: ScreenDataStore
    @ObservedObject private var context: ScreenRenderContext
    @Environment(\.accessibilityReduceMotion) private var accessibilityReduceMotion

    public init(document: ScreenDocument, store: ScreenDataStore? = nil, perform: @escaping (ScreenAction) -> Void = { _ in }) {
        let actualStore = store ?? ScreenDataStore(document: document)
        self.document = document
        _store = ObservedObject(wrappedValue: actualStore)
        _context = ObservedObject(wrappedValue: ScreenRenderContext(store: actualStore, perform: perform))
    }

    public init(document: ScreenDocument, context: ScreenRenderContext) {
        self.document = document
        _store = ObservedObject(wrappedValue: context.store)
        _context = ObservedObject(wrappedValue: context)
    }

    public var body: some View {
        ScreenNodeView(node: document.root, context: context)
            .environment(\.screenReduceMotion, accessibilityReduceMotion)
            .environment(\.surfaceDensity, .regular)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .padding(ScreenStyle.padding(.regular))
    }
}

struct ScreenNodeView: View {
    let node: ScreenNode
    @ObservedObject var context: ScreenRenderContext
    @Environment(\.surfaceDensity) private var density
    @Environment(\.screenReduceMotion) private var reduceMotion
    @State private var expired = false

    private var resolved: ScreenNode { ComponentCatalog.resolve(node) }
    private var props: [String: JSONValue] {
        guard let bind = node.bind, case let .object(fields)? = context.store.value(for: bind) else { return resolved.props }
        return resolved.props.merging(fields) { _, new in new }
    }

    var body: some View {
        Group {
            if !expired { rendered }
        }
        .accessibilityElement(children: .contain)
        .transition(reduceMotion ? .identity : transition)
        .task(id: node.ttlSeconds) {
            guard let ttl = node.ttlSeconds, ttl > 0 else { return }
            try? await Task.sleep(for: .seconds(ttl))
            guard !Task.isCancelled else { return }
            expired = true
        }
    }

    @ViewBuilder private var rendered: some View {
        switch resolved.type {
        case "stack": stack
        case "grid": grid
        case "split": split
        case "group": group
        case "spacer": Spacer(minLength: dimension("minLength") ?? ScreenStyle.spacing(density))
        case "canvas": CanvasView(children: resolved.children, context: context)
        case "layer": ZStack { children }
        case "scroll": ScrollView(scrollAxis) { children }
        case "frame": children.frame(minWidth: dimension("minWidth"), maxWidth: dimension("maxWidth"), minHeight: dimension("minHeight"), maxHeight: dimension("maxHeight"))
        case "background": background
        case "title": textView(font: ScreenStyle.font(for: node.emphasis, level: int("level") ?? 2), label: string("text"))
        case "text": textView(font: ScreenStyle.font(for: node.emphasis), label: string("text"))
        case "richText": richText
        case "list": list
        case "table": table
        case "progress": progress
        case "statusDot": statusDot
        case "image": image
        case "icon": Image(systemName: string("name") ?? "questionmark.circle").accessibilityLabel(string("name") ?? "Icon")
        case "link": Link(string("label") ?? string("url") ?? "Link", destination: URL(string: string("url") ?? "") ?? URL(string: "about:blank")!)
        case "countdown": countdown
        case "divider": Divider()
        case "button": Button(string("label") ?? "Continue") { submit(node.action) }.buttonStyle(.borderedProminent)
        case "choice": ChoiceView(node: resolved, context: context)
        case "input": InputView(node: resolved, context: context)
        case "toggle": ToggleView(node: resolved, context: context)
        case "metric", "timeline", "steps", "decision", "compare", "collection", "article", "chart", "place", "media", "notice", "facts", "status", "form", "quote", "code", "gauge", "calendar", "gallery", "ranking", "checklist": StandardComponentView(node: resolved, context: context)
        default: fallback
        }
    }

    @ViewBuilder private var children: some View {
        ForEach(resolved.children) { child in ScreenNodeView(node: child, context: context) }
    }

    private var transition: AnyTransition {
        switch node.transition?.lowercased() {
        case "slide": return .slide
        case "scale": return .scale
        case "move": return .move(edge: .bottom)
        default: return .opacity
        }
    }

    private var stack: some View {
        let axis = string("axis")?.lowercased()
        return Group {
            if axis == "horizontal" || axis == "h" { HStack(alignment: alignment, spacing: dimension("spacing") ?? ScreenStyle.spacing(density)) { children } }
            else { VStack(alignment: horizontalAlignment, spacing: dimension("spacing") ?? ScreenStyle.spacing(density)) { children } }
        }
    }

    private var grid: some View {
        let columns = max(1, int("columns") ?? 2)
        return LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: ScreenStyle.spacing(density)), count: columns), spacing: ScreenStyle.spacing(density)) { children }
    }

    private var split: some View {
        let ratio = CGFloat(max(0.1, min(0.9, number("ratio") ?? 0.5)))
        return GeometryReader { proxy in
            if (string("orientation") ?? "horizontal").lowercased() == "vertical" {
                VStack(spacing: 0) { childAt(0).frame(height: proxy.size.height * ratio); Divider(); childAt(1).frame(maxHeight: .infinity) }
            } else {
                HStack(spacing: 0) { childAt(0).frame(width: proxy.size.width * ratio); Divider(); childAt(1).frame(maxWidth: .infinity) }
            }
        }
        .frame(minHeight: 80)
    }

    private var group: some View {
        GroupBox(string("label") ?? "") { VStack(alignment: .leading, spacing: ScreenStyle.spacing(density)) { children } }
    }

    private var background: some View {
        let opacity = number("opacity") ?? 1
        return ZStack {
            MaterialView(material: string("material") ?? "content", opacity: opacity)
            if let gradient = props["gradient"]?.stringValue, !gradient.isEmpty { LinearGradient(colors: [.clear, ScreenStyle.tone(node.tone).opacity(0.12)], startPoint: .topLeading, endPoint: .bottomTrailing) }
            children
        }
        .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    private func textView(font: Font, label: String?) -> some View {
        Text(label ?? "").font(font).foregroundStyle(ScreenStyle.tone(node.tone)).textSelection(.enabled).accessibilityLabel(label ?? "")
    }

    private var richText: some View {
        let markdown = string("markdown") ?? ""
        return Group { if let value = try? AttributedString(markdown: markdown) { Text(value) } else { Text(markdown) } }.textSelection(.enabled)
    }

    private var list: some View {
        let items = array("items")
        return VStack(alignment: .leading, spacing: ScreenStyle.spacing(density) / 2) {
            ForEach(Array(items.enumerated()), id: \.offset) { _, item in Text(item.displayString).frame(maxWidth: .infinity, alignment: .leading) }
        }
    }

    private var table: some View {
        let columns = array("columns").map(\.displayString)
        let rows = array("rows")
        return VStack(alignment: .leading, spacing: 0) {
            HStack { ForEach(columns, id: \.self) { Text($0).font(.headline).frame(maxWidth: .infinity, alignment: .leading) } }
            Divider()
            ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                HStack { ForEach(row.arrayValue ?? [], id: \.displayString) { Text($0.displayString).frame(maxWidth: .infinity, alignment: .leading) } }
                    .padding(.vertical, 4)
            }
        }
    }

    private var progress: some View {
        VStack(alignment: .leading) { if let label = string("label") { Text(label).font(.caption) }; ProgressView(value: number("value") ?? 0).tint(ScreenStyle.tone(node.tone)) }
    }

    private var statusDot: some View {
        Label(string("label") ?? string("state") ?? "", systemImage: "circle.fill").foregroundStyle(ScreenStyle.tone(node.tone)).font(ScreenStyle.font(for: node.emphasis))
    }

    private var image: some View {
        AsyncImage(url: URL(string: string("url") ?? "")) { phase in
            switch phase { case .success(let image): image.resizable().scaledToFit(); case .failure: Image(systemName: "photo"); default: ProgressView() }
        }.accessibilityLabel(string("alt") ?? "Image")
    }

    private var countdown: some View {
        if let until = date("until") { return AnyView(Text(timerInterval: Date.now...until, countsDown: true).monospacedDigit().accessibilityLabel(string("label") ?? "Countdown")) }
        return AnyView(Text(string("label") ?? ""))
    }

    private var fallback: some View {
        VStack(alignment: .leading, spacing: 5) { Text(resolved.type).font(.caption).foregroundStyle(.secondary); if let text = props["text"]?.displayString { Text(text) }; children }
    }

    private func childAt(_ index: Int) -> some View { ScreenNodeView(node: resolved.children.indices.contains(index) ? resolved.children[index] : ScreenNode(id: "empty", type: "spacer"), context: context) }
    private func submit(_ action: ScreenAction?, extra: [String: JSONValue] = [:]) { guard var action else { return }; action.args.merge(extra) { _, new in new }; context.perform(action) }
    private func value(_ key: String) -> JSONValue? { props[key] }
    private func string(_ key: String) -> String? { value(key)?.stringValue }
    private func number(_ key: String) -> Double? { value(key)?.numberValue }
    private func dimension(_ key: String) -> CGFloat? { number(key).map { CGFloat($0) } }
    private func int(_ key: String) -> Int? { number(key).map(Int.init) }
    private func array(_ key: String) -> [JSONValue] { value(key)?.arrayValue ?? [] }
    private func date(_ key: String) -> Date? { value(key)?.stringValue.flatMap { ISO8601DateFormatter().date(from: $0) } }
    private var horizontalAlignment: HorizontalAlignment { switch string("alignment")?.lowercased() { case "center": return .center; case "trailing", "right": return .trailing; default: return .leading } }
    private var alignment: VerticalAlignment { switch string("alignment")?.lowercased() { case "center": return .center; case "bottom": return .bottom; default: return .top } }
    private var scrollAxis: Axis.Set { switch string("axis")?.lowercased() { case "horizontal", "x": return .horizontal; case "both": return [.horizontal, .vertical]; default: return .vertical } }
}

private struct ChoiceView: View {
    let node: ScreenNode; @ObservedObject var context: ScreenRenderContext; @State private var selected: String
    init(node: ScreenNode, context: ScreenRenderContext) { self.node = node; self.context = context; _selected = State(initialValue: node.props["selected"]?.stringValue ?? "") }
    var body: some View { VStack(alignment: .leading) { ForEach(node.props["options"]?.arrayValue ?? [], id: \.displayString) { option in Button { selected = option["value"]?.stringValue ?? option["label"]?.stringValue ?? option.displayString; var args = node.action?.args ?? [:]; args["value"] = .string(selected); if let action = node.action { context.perform(ScreenAction(intent: action.intent, args: args)) } } label: { Label(option["label"]?.stringValue ?? option.displayString, systemImage: selected == (option["value"]?.stringValue ?? option["label"]?.stringValue) ? "checkmark.circle.fill" : "circle") } } } }
}

private struct InputView: View {
    let node: ScreenNode; @ObservedObject var context: ScreenRenderContext; @State private var value: String
    init(node: ScreenNode, context: ScreenRenderContext) { self.node = node; self.context = context; _value = State(initialValue: node.props["value"]?.stringValue ?? "") }
    var body: some View { HStack { TextField(node.props["placeholder"]?.stringValue ?? node.props["label"]?.stringValue ?? "", text: $value); Button("Submit") { submit() } }.onSubmit(submit) }
    private func submit() { guard let action = node.action else { return }; var args = action.args; args["value"] = .string(value); context.perform(ScreenAction(intent: action.intent, args: args)) }
}

private struct ToggleView: View {
    let node: ScreenNode; @ObservedObject var context: ScreenRenderContext; @State private var value: Bool
    init(node: ScreenNode, context: ScreenRenderContext) { self.node = node; self.context = context; _value = State(initialValue: node.props["value"]?.boolValue ?? false) }
    var body: some View { Toggle(node.props["label"]?.stringValue ?? "", isOn: Binding(get: { value }, set: { value = $0; submit() })) }
    private func submit() { guard let action = node.action else { return }; var args = action.args; args["value"] = .bool(value); context.perform(ScreenAction(intent: action.intent, args: args)) }
}

private struct CanvasView: View {
    let children: [ScreenNode]; @ObservedObject var context: ScreenRenderContext
    var body: some View { CanvasLayout { ForEach(children) { child in ScreenNodeView(node: child, context: context) } } }
}

private struct CanvasLayout: Layout {
    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize { CGSize(width: proposal.width ?? 320, height: proposal.height ?? 240) }
    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) { for (index, subview) in subviews.enumerated() { let x = CGFloat(index % 4) / 4 * bounds.width; let y = CGFloat(index / 4) / max(1, CGFloat((subviews.count + 3) / 4)) * bounds.height; subview.place(at: CGPoint(x: bounds.minX + x, y: bounds.minY + y), proposal: ProposedViewSize(width: bounds.width / 4, height: bounds.height / 2)) } }
}

private extension JSONValue {
    var stringValue: String? { if case let .string(value) = self { return value }; return nil }
    var numberValue: Double? { screenNumber }
    var boolValue: Bool? { if case let .bool(value) = self { return value }; return nil }
    var arrayValue: [JSONValue]? { if case let .array(value) = self { return value }; return nil }
    var objectValue: [String: JSONValue]? { if case let .object(value) = self { return value }; return nil }
    subscript(key: String) -> JSONValue? { objectValue?[key] }
    var displayString: String { switch self { case .null: return ""; case .bool(let value): return value ? "true" : "false"; case .number(let value): return String(value); case .integer(let value): return String(value); case .string(let value): return value; case .array(let values): return values.map(\.displayString).joined(separator: ", "); case .object(let values): return values.map { "\($0.key): \($0.value.displayString)" }.joined(separator: ", ") } }
}
#endif
