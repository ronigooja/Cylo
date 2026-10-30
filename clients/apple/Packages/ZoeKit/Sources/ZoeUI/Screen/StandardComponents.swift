#if os(macOS)
import Foundation
import SwiftUI
import Charts
import MapKit
import ZoeKit

struct StandardComponentView: View {
    let node: ScreenNode
    @ObservedObject var context: ScreenRenderContext
    @Environment(\.surfaceDensity) private var density

    private var props: [String: JSONValue] { node.props }
    private func value(_ key: String) -> JSONValue? { props[key] }
    private func string(_ key: String) -> String? { value(key)?.stringValue }
    private func number(_ key: String) -> Double? { value(key)?.numberValue }
    private func array(_ key: String) -> [JSONValue] { value(key)?.arrayValue ?? [] }
    private func object(_ key: String) -> [[String: JSONValue]] { array(key).compactMap(\.objectValue) }
    private func send(_ action: ScreenAction?, args: [String: JSONValue] = [:]) { guard let action else { return }; var merged = action.args; merged.merge(args) { _, new in new }; context.perform(ScreenAction(intent: action.intent, args: merged)) }

    var body: some View {
        Group {
            switch node.type {
            case "metric": metric
            case "timeline": timeline
            case "steps": steps
            case "decision": decision
            case "compare": compare
            case "collection": collection
            case "article": article
            case "chart": chart
            case "place": place
            case "map": map
            case "media": media
            case "notice": notice
            case "facts": facts
            case "status": status
            case "form": form
            case "quote": quote
            case "code": code
            case "gauge": gauge
            case "calendar": calendar
            case "gallery": gallery
            case "ranking": ranking
            case "checklist": checklist
            default: EmptyView()
            }
        }
        .accessibilityElement(children: .contain)
    }

    private var metric: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(string("label") ?? "Metric").font(.caption).foregroundStyle(.secondary)
            HStack(alignment: .lastTextBaseline, spacing: 5) { Text(value("value")?.displayString ?? "—").font(density == .minimal ? .title3 : .largeTitle).fontWeight(.semibold); if let unit = string("unit") { Text(unit).foregroundStyle(.secondary) } }
            if let delta = value("delta")?.displayString { Label(delta, systemImage: delta.hasPrefix("-") ? "arrow.down.right" : "arrow.up.right").font(.caption).foregroundStyle(delta.hasPrefix("-") ? .red : .green) }
        }.padding(ScreenStyle.padding(density)).background(.quaternary.opacity(0.45), in: RoundedRectangle(cornerRadius: 10))
    }

    private var timeline: some View {
        VStack(alignment: .leading, spacing: ScreenStyle.spacing(density)) {
            ForEach(Array(object("events").enumerated()), id: \.offset) { _, event in
                HStack(alignment: .top, spacing: 9) { Circle().fill(ScreenStyle.tone(node.tone)).frame(width: 7, height: 7).padding(.top, 5); VStack(alignment: .leading) { Text(event["title"]?.displayString ?? ""); if let time = event["time"]?.displayString { Text(time).font(.caption).foregroundStyle(.secondary) }; if let detail = event["detail"]?.displayString { Text(detail).font(.subheadline).foregroundStyle(.secondary) } } }
            }
        }
    }

    private var steps: some View {
        VStack(alignment: .leading, spacing: 8) { ForEach(Array(object("steps").enumerated()), id: \.offset) { index, step in Label(step["label"]?.displayString ?? "", systemImage: step["state"]?.displayString == "done" || index < (Int(number("current") ?? -1)) ? "checkmark.circle.fill" : "circle").foregroundStyle(index < (Int(number("current") ?? -1)) ? .green : .secondary) }; if let fraction = number("fraction") { ProgressView(value: fraction) } }
    }

    private var decision: some View {
        VStack(alignment: .leading, spacing: ScreenStyle.spacing(density)) { Text(string("question") ?? "").font(.headline); ForEach(Array(object("options").enumerated()), id: \.offset) { _, option in Button(option["label"]?.displayString ?? "Choose") { var args = option["args"]?.objectValue ?? [:]; args["option"] = .string(option["label"]?.displayString ?? ""); send(node.action, args: args) }.buttonStyle(.bordered) } }
    }

    private var compare: some View {
        let items = array("items").map(\.displayString)
        let dimensions = array("dimensions").map(\.displayString)
        let values = value("values")?.objectValue ?? [:]
        return VStack(alignment: .leading) { HStack { Text("").frame(width: 100); ForEach(items, id: \.self) { Text($0).font(.caption).frame(maxWidth: .infinity) } }; ForEach(dimensions, id: \.self) { dimension in HStack { Text(dimension).font(.caption).frame(width: 100, alignment: .leading); ForEach(items, id: \.self) { item in Text(values[item]?.objectValue?[dimension]?.displayString ?? "—").frame(maxWidth: .infinity) } } } }
    }

    private var collection: some View {
        let items = array("items")
        let layout = string("layout")?.lowercased() ?? "list"
        return Group { if layout == "grid" || layout == "cards" { LazyVGrid(columns: [GridItem(.adaptive(minimum: 120))]) { ForEach(Array(items.enumerated()), id: \.offset) { _, item in Text(item.displayString).frame(maxWidth: .infinity, minHeight: 52).background(.quaternary, in: RoundedRectangle(cornerRadius: 8)) } } } else { VStack(alignment: .leading) { ForEach(Array(items.enumerated()), id: \.offset) { _, item in Text(item.displayString).frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 4); Divider() } } } }
    }

    private var article: some View {
        VStack(alignment: .leading, spacing: ScreenStyle.spacing(density)) { if let title = string("title") { Text(title).font(.title2.bold()) }; if let body = string("body") { MarkdownText(body) }; ForEach(Array(object("sections").enumerated()), id: \.offset) { _, section in VStack(alignment: .leading) { Text(section["title"]?.displayString ?? "").font(.headline); MarkdownText(section["body"]?.displayString ?? "") } }; if !array("sources").isEmpty { Text("Sources").font(.caption.bold()); ForEach(array("sources"), id: \.displayString) { Text($0.displayString).font(.caption).foregroundStyle(.secondary) } } }
    }

    private var chart: some View {
        Chart { ForEach(Array(object("series").enumerated()), id: \.offset) { _, series in ForEach(Array((series["points"]?.arrayValue ?? []).enumerated()), id: \.offset) { _, point in LineMark(x: .value("x", point["x"]?.displayString ?? point["label"]?.displayString ?? ""), y: .value("y", point["y"]?.numberValue ?? point["value"]?.numberValue ?? 0)).foregroundStyle(by: .value("series", series["name"]?.displayString ?? "Series")) } } }.frame(minHeight: density == .minimal ? 90 : 180)
    }

    private var place: some View {
        VStack(alignment: .leading) { Label(string("name") ?? "Place", systemImage: "mappin.and.ellipse"); if let coordinate = value("coordinate")?.objectValue { Text("\(coordinate["latitude"]?.displayString ?? ""), \(coordinate["longitude"]?.displayString ?? "")").font(.caption).foregroundStyle(.secondary) }; ForEach(array("annotations"), id: \.displayString) { Text($0.displayString).font(.caption) } }
    }

    private var map: some View {
        Map { ForEach(Array(array("points").enumerated()), id: \.offset) { _, point in
            if let coordinate = point.objectValue?["coordinate"]?.objectValue,
               let latitude = coordinate["latitude"]?.numberValue,
               let longitude = coordinate["longitude"]?.numberValue {
                Marker(point["name"]?.displayString ?? "Place", coordinate: CLLocationCoordinate2D(latitude: latitude, longitude: longitude))
            }
        } }.frame(minHeight: density == .minimal ? 100 : 220).clipShape(RoundedRectangle(cornerRadius: 10))
    }

    private var media: some View {
        VStack(alignment: .leading) { if string("kind") == "image" { AsyncImage(url: URL(string: string("url") ?? "")) { phase in if case .success(let image) = phase { image.resizable().scaledToFit() } else { ProgressView() } } }; if let caption = string("caption") { Text(caption).font(.caption).foregroundStyle(.secondary) } }
    }

    private var notice: some View {
        HStack(alignment: .top) { Image(systemName: "exclamationmark.triangle.fill"); Text(string("text") ?? ""); Spacer(); if let action = node.action { Button("Open") { send(action) }.buttonStyle(.bordered) } }.padding(ScreenStyle.padding(density)).foregroundStyle(ScreenStyle.tone(node.tone)).background(ScreenStyle.tone(node.tone).opacity(0.12), in: RoundedRectangle(cornerRadius: 10))
    }

    private var facts: some View {
        VStack(alignment: .leading) { ForEach(Array(object("pairs").enumerated()), id: \.offset) { _, pair in LabeledContent(pair["key"]?.displayString ?? "", value: pair["value"]?.displayString ?? "") }; ForEach(Array(object("groups").enumerated()), id: \.offset) { _, group in Text(group["title"]?.displayString ?? "").font(.headline); ForEach(group["pairs"]?.arrayValue ?? [], id: \.displayString) { pair in LabeledContent(pair["key"]?.displayString ?? "", value: pair["value"]?.displayString ?? "") } } }
    }

    private var status: some View { Label { VStack(alignment: .leading) { Text(string("state") ?? "").font(.headline); if let description = string("description") { Text(description).font(.caption).foregroundStyle(.secondary) }; if let updatedAt = string("updatedAt") { Text(updatedAt).font(.caption2).foregroundStyle(.secondary) } } } icon: { Circle().fill(ScreenStyle.tone(node.tone)).frame(width: 10, height: 10) } }

    private var form: some View { VStack(alignment: .leading, spacing: 8) { ForEach(Array(object("fields").enumerated()), id: \.offset) { _, field in TextField(field["label"]?.displayString ?? field["key"]?.displayString ?? "", text: .constant("")) }; Button("Submit") { send(node.action) }.buttonStyle(.borderedProminent) } }

    private var quote: some View { VStack(alignment: .leading) { Text("“\(string("text") ?? "")”").italic(); if let source = string("source") { Text("— \(source)").font(.caption).foregroundStyle(.secondary) } }.padding(.leading, 12).overlay(alignment: .leading) { Rectangle().fill(ScreenStyle.tone(node.tone)).frame(width: 3) } }

    private var code: some View { Text(string("text") ?? "").font(.system(.body, design: .monospaced)).textSelection(.enabled).padding(ScreenStyle.padding(density)).background(.quaternary, in: RoundedRectangle(cornerRadius: 8)) }

    private var gauge: some View { let min = number("min") ?? 0; let max = number("max") ?? 1; let fraction = max > min ? ((number("value") ?? min) - min) / (max - min) : 0; return VStack(alignment: .leading) { Gauge(value: fraction) { Text(value("value")?.displayString ?? "") } currentValueLabel: { Text("\(Int(fraction * 100))%") }.tint(ScreenStyle.tone(node.tone)); ForEach(Array(object("zones").enumerated()), id: \.offset) { _, zone in Text(zone["label"]?.displayString ?? "").font(.caption) } } }

    private var calendar: some View { VStack(alignment: .leading) { if let range = string("range") { Label(range, systemImage: "calendar") }; ForEach(Array(object("events").enumerated()), id: \.offset) { _, event in HStack { Text(event["date"]?.displayString ?? event["time"]?.displayString ?? "").font(.caption).foregroundStyle(.secondary); Text(event["title"]?.displayString ?? "Event") } } } }

    private var gallery: some View { LazyVGrid(columns: [GridItem(.adaptive(minimum: 100))]) { ForEach(Array(object("items").enumerated()), id: \.offset) { _, item in AsyncImage(url: URL(string: item["url"]?.displayString ?? "")) { phase in if case .success(let image) = phase { image.resizable().scaledToFill().frame(minHeight: 90).clipped() } else { Color.secondary.opacity(0.15).frame(minHeight: 90) } } } } }

    private var ranking: some View { VStack(alignment: .leading) { ForEach(Array(object("items").enumerated()), id: \.offset) { index, item in HStack { Text("\(index + 1)").foregroundStyle(.secondary).frame(width: 24); Text(item["label"]?.displayString ?? ""); Spacer(); Text(item["score"]?.displayString ?? "") } } } }

    private var checklist: some View { VStack(alignment: .leading) { ForEach(Array(object("items").enumerated()), id: \.offset) { _, item in Label(item["label"]?.displayString ?? "", systemImage: item["done"]?.boolValue == true ? "checkmark.square.fill" : "square").foregroundStyle(item["done"]?.boolValue == true ? .green : .primary) } } }
}

private struct MarkdownText: View {
    let value: String
    var body: some View { if let attributed = try? AttributedString(markdown: value) { Text(attributed).textSelection(.enabled) } else { Text(value).textSelection(.enabled) } }
}

private extension JSONValue {
    var stringValue: String? { if case let .string(value) = self { return value }; return nil }
    var numberValue: Double? { if case let .number(value) = self { return value }; return nil }
    var boolValue: Bool? { if case let .bool(value) = self { return value }; return nil }
    var arrayValue: [JSONValue]? { if case let .array(value) = self { return value }; return nil }
    var objectValue: [String: JSONValue]? { if case let .object(value) = self { return value }; return nil }
    subscript(key: String) -> JSONValue? { objectValue?[key] }
    var displayString: String { switch self { case .null: return ""; case .bool(let value): return value ? "true" : "false"; case .number(let value): return String(value); case .string(let value): return value; case .array(let values): return values.map(\.displayString).joined(separator: ", "); case .object(let values): return values.map { "\($0.key): \($0.value.displayString)" }.joined(separator: ", ") } }
}
#endif
