import ZoeKit
import SwiftUI

import UniformTypeIdentifiers

public struct ZoeSettingsView: View {
    @ObservedObject var model: ZoeAppModel
    @State private var showingConnectionEditor = false
    @State private var exportScope = "personal"
    @State private var showingExportSave = false
    @State private var validatingExportSave = false
    @State private var showingClearCredential = false

    public init(model: ZoeAppModel) {
        self.model = model
    }

    private var exportDocument: ScopedExportFileDocument {
        ScopedExportFileDocument(fileURL: model.exportArtifact?.fileURL ?? FileManager.default.temporaryDirectory)
    }

    private var voiceRecoveryHint: String? {
        guard case .unavailable = model.voiceStatus,
              model.voicePoolStatus?.blocked != true else { return nil }
        return uiText("Check microphone permission and system audio settings, then reconnect.")
    }

    public var body: some View {
        Group {
            if model.authentication.isConnected { settingsForm }
            else { InstanceConnectionView(model: model) }
        }
    }

    private var settingsForm: some View {
        Form {
            Section(uiText("Connection")) {
                LabeledContent(uiText("Server"), value: model.serverAddress)
                if model.isConnecting {
                    ProgressView(uiText("Connecting…"))
                } else if case .current = model.freshness {
                    Label(uiText("Connected"), systemImage: "checkmark.circle.fill")
                        .foregroundStyle(.green)
                }
                if let notice = model.notice {
                    Text(notice).font(.callout).foregroundStyle(.secondary).textSelection(.enabled)
                }
                Button(uiText("Connect")) { Task { await model.reconnect() } }
                    .disabled(model.isConnecting || model.isBusy)
            }
            Section {
                Button(uiText("Replace instance key…")) { showingConnectionEditor = true }
                    .disabled(model.isBusy || model.isConnecting)
                Button(uiText("Clear saved key…"), role: .destructive) { showingClearCredential = true }
                    .disabled(model.isBusy || model.isConnecting)
            }
            Section(uiText("Notifications")) {
                if model.pushNotificationsEnabled {
                    Button(uiText("Disable push wakeups"), systemImage: "bell.slash") {
                        Task { await model.disablePushNotifications() }
                    }
                } else {
                    Button(uiText("Enable push wakeups"), systemImage: "bell.badge") {
                        Task { await model.enablePushNotifications() }
                    }
                }
            }
            Section(uiText("Voice")) {
                Toggle(uiText("Continuous voice"), isOn: Binding(
                    get: { model.continuousVoiceEnabled },
                    set: { model.setContinuousVoiceEnabled($0) }
                ))
                Text(uiText("When enabled, Zoe attempts to maintain an authorized voice session after connection. Microphone permission, system audio policy, and explicit transcript submission still apply."))
                    .font(.footnote).foregroundStyle(.secondary)
                switch model.voiceStatus {
                case .active: Label(uiText("Voice active"), systemImage: "waveform").foregroundStyle(.green)
                case .connecting: Label(uiText("Connecting voice…"), systemImage: "arrow.triangle.2.circlepath")
                case let .reconnecting(reason): Label(reason, systemImage: "arrow.triangle.2.circlepath")
                case let .interrupted(reason), let .unavailable(reason): Text(reason).foregroundStyle(.orange)
                case .stopped: Label(uiText("Voice stopped"), systemImage: "stop.circle")
                case .idle: EmptyView()
                }
                if let hint = voiceRecoveryHint {
                    Text(hint)
                        .font(.footnote).foregroundStyle(.secondary)
                }
            }
            Section(uiText("Your data")) {
                Text(uiText("Prepare a copy of authorized Zoe records. Large artifact files, derived stores, and backups are not included."))
                    .foregroundStyle(.secondary)
                Picker(uiText("Data scope"), selection: $exportScope) {
                    Text(uiText("Personal data")).tag("personal")
                    Text(uiText("Workspace data I can access")).tag("workspace")
                }
                Button(uiText("Prepare export")) {
                    let epoch = model.connectionEpoch
                    Task { await model.prepareExport(scope: exportScope, expectedConnectionEpoch: epoch) }
                }
                .disabled(model.exportStatus == uiText("Preparing the authorized export…") || model.exportArtifact != nil)
                if let artifact = model.exportArtifact {
                    LabeledContent(uiText("Prepared file"), value: artifact.filename)
                    Button(uiText("Save export…")) {
                        let epoch = model.connectionEpoch
                        validatingExportSave = true
                        Task {
                            let valid = await model.validateExportForSave(expectedConnectionEpoch: epoch)
                            validatingExportSave = false
                            if valid && epoch == model.connectionEpoch && model.exportArtifact != nil {
                                showingExportSave = true
                            }
                        }
                    }
                    .disabled(validatingExportSave)
                    Button(uiText("Discard export"), role: .cancel) { model.cancelExport() }
                }
                if let status = model.exportStatus {
                    Text(status).font(.footnote).foregroundStyle(.secondary).textSelection(.enabled)
                }
            }
        }
        .formStyle(.grouped)
        .navigationTitle(uiText("Settings"))
        .fileExporter(isPresented: $showingExportSave, document: exportDocument, contentType: .zip, defaultFilename: model.exportArtifact?.filename ?? "zoe-export.zip") { result in
            switch result {
            case .success: model.exportSaveFinished(saved: true)
            case let .failure(error): model.exportSaveFinished(saved: false, error: error)
            }
        }
        .onChange(of: model.connectionEpoch) { _, _ in showingExportSave = false }
        .onChange(of: model.exportArtifact?.fileURL) { _, fileURL in
            if fileURL == nil { showingExportSave = false }
        }
        .sheet(isPresented: $showingConnectionEditor) { InstanceConnectionView(model: model) }
        .confirmationDialog(uiText("Clear saved key?"), isPresented: $showingClearCredential) {
            Button(uiText("Clear saved key"), role: .destructive) { Task { await model.clearCredential() } }
            Button(uiText("Cancel"), role: .cancel) {}
        } message: {
            Text(uiText("The saved key, queued local commands, caches, and drafts will be removed from this app. The shared server key stays valid on other devices. Running server tasks continue."))
        }
    }
}

struct ScopedExportFileDocument: FileDocument {
    static var readableContentTypes: [UTType] { [.zip] }
    let fileURL: URL

    init(fileURL: URL) { self.fileURL = fileURL }

    init(configuration: ReadConfiguration) throws {
        throw ZoeError.invalidResponse
    }

    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        FileWrapper(regularFileWithContents: try Data(contentsOf: fileURL, options: .mappedIfSafe))
    }
}
