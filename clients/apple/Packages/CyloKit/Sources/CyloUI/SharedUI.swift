import CyloKit
import SwiftUI

func uiText(_ key: String) -> String { NSLocalizedString(key, bundle: .module, comment: "") }

func uiFormat(_ key: String, _ arguments: CVarArg...) -> String {
    String(format: uiText(key), locale: Locale.current, arguments: arguments)
}

/// Only exceptional connectivity is surfaced in the workspace; protocol detail stays in settings.
struct FreshnessBanner: View {
    @ObservedObject var model: CyloAppModel
    @State private var showingReason = false
    var body: some View {
        switch model.freshness {
        case .current: EmptyView()
        case .loading: ProgressView(uiText("Connecting…")).font(.caption)
        case let .offline(reason, _), let .stale(reason, _):
            HStack {
                Button { showingReason = true } label: { Label(uiText("Connection unavailable"), systemImage: "wifi.exclamationmark") }.buttonStyle(.plain)
                Spacer()
                Button(uiText("Reconnect")) { Task { await model.reconnect() } }
            }
                .font(.caption).foregroundStyle(.secondary)
                .help(reason)
                .alert(uiText("Connection unavailable"), isPresented: $showingReason) {
                    Button(uiText("OK"), role: .cancel) {}
                } message: { Text(reason) }
        }
    }
}

struct NoticeCard: View {
    let message: String?
    var body: some View {
        if let message, !message.isEmpty { Text(message).font(.callout).foregroundStyle(.red).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading) }
    }
}


func uiDate(_ timestamp: String) -> String {
    let formatter = ISO8601DateFormatter()
    formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    let precise = formatter.date(from: timestamp)
    formatter.formatOptions = [.withInternetDateTime]
    guard let date = precise ?? formatter.date(from: timestamp) else { return timestamp }
    return date.formatted(date: .abbreviated, time: .shortened)
}

func uiTaskCount(_ count: Int) -> String {
    String.localizedStringWithFormat(uiText("%lld tasks"), Int64(count))
}

/// Preserve source text while giving fenced code its own selectable, horizontal viewport.
struct MessageBody: View {
    let content: String
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ForEach(Array(content.components(separatedBy: "```").enumerated()), id: \.offset) { index, segment in
                if index.isMultiple(of: 2) {
                    if !segment.isEmpty { Text(segment).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading) }
                } else {
                    ScrollView(.horizontal) {
                        Text(codeText(segment))
                            .font(.system(.body, design: .monospaced))
                            .textSelection(.enabled)
                            .fixedSize(horizontal: true, vertical: false)
                            .padding(12)
                    }
                    .background(.quaternary, in: RoundedRectangle(cornerRadius: 10))
                }
            }
        }
    }
    private func codeText(_ segment: String) -> String {
        let lines = segment.split(separator: "\n", omittingEmptySubsequences: false)
        if lines.count > 1, let first = lines.first, !first.isEmpty,
           first.allSatisfy({ $0.isLetter || $0.isNumber || ".+-#".contains($0) }) {
            return lines.dropFirst().joined(separator: "\n").trimmingCharacters(in: .newlines)
        }
        return segment.trimmingCharacters(in: .newlines)
    }
}
