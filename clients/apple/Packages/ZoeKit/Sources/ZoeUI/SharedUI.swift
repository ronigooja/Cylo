import ZoeKit
import SwiftUI

func uiText(_ key: String) -> String { NSLocalizedString(key, bundle: .module, comment: "") }

func uiFormat(_ key: String, _ arguments: CVarArg...) -> String {
    String(format: uiText(key), locale: Locale.current, arguments: arguments)
}

/// Only exceptional connectivity is surfaced in the workspace; protocol detail stays in settings.
struct FreshnessBanner: View {
    @ObservedObject var model: ZoeAppModel
    var body: some View {
        switch model.freshness {
        case .current: EmptyView()
        case .loading: ProgressView(uiText("Connecting…")).font(.caption)
        case let .offline(reason, _), let .stale(reason, _):
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Label(uiText("Connection unavailable"), systemImage: "wifi.exclamationmark")
                    Spacer()
                    Button(uiText("Reconnect")) { Task { await model.reconnect() } }
                }
                Text(reason).textSelection(.enabled)
            }
                .font(.caption).foregroundStyle(.secondary)
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

/// Display authoritative message content without adding a source-code viewer.
struct MessageBody: View {
    let content: String
    var body: some View {
        Text(content)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

func taskStatusSymbol(_ status: TaskStatus) -> String {
    switch status {
    case .queued: "clock"
    case .active: "circle.dotted.circle"
    case .waiting: "hourglass"
    case .awaitingUser: "person.crop.circle.badge.exclamationmark"
    case .paused: "pause.circle"
    case .complete: "checkmark.circle"
    case .failed: "exclamationmark.circle"
    case .canceled: "xmark.circle"
    }
}

func taskStatusColor(_ status: TaskStatus) -> Color {
    switch status {
    case .active: .accentColor
    case .awaitingUser, .waiting: .orange
    case .complete: .green
    case .failed: .red
    case .queued, .paused, .canceled: .secondary
    }
}

#if os(macOS)
struct AssistantRowStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        Row(configuration: configuration)
    }

    private struct Row: View {
        let configuration: ButtonStyleConfiguration
        @State private var hovering = false

        var body: some View {
            configuration.label
                .background(Color.primary.opacity(configuration.isPressed ? 0.08 : hovering ? 0.04 : 0), in: RoundedRectangle(cornerRadius: 8))
                .onHover { hovering = $0 }
        }
    }
}
#endif
