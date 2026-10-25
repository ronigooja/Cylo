import CyloKit
import SwiftUI

import UniformTypeIdentifiers

public struct CyloSettingsView: View {
    @ObservedObject var model: CyloAppModel
    @State private var showingRevoke: Device?
    @State private var exportScope = "personal"
    @State private var showingExportSave = false
    @State private var validatingExportSave = false
    @State private var showingSignOut = false

    public init(model: CyloAppModel) {
        self.model = model
    }

    private var exportDocument: ScopedExportFileDocument {
        ScopedExportFileDocument(fileURL: model.exportArtifact?.fileURL ?? FileManager.default.temporaryDirectory)
    }

    public var body: some View {
        Group {
            if model.authentication.deviceID != nil { settingsForm }
            else { OwnerLoginView(model: model) }
        }
    }

    private var settingsForm: some View {
        Form {
            Section(uiText("Connection")) {
                LabeledContent(uiText("Server"), value: "debian.local:8787")
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
                CredentialStorageControls(model: model)
            }
            Section(uiText("Devices")) {
                if model.devices.isEmpty { Text(uiText("No device records in the server snapshot.")).foregroundStyle(.secondary) }
                ForEach(model.devices) { device in
                    VStack(alignment: .leading, spacing: 8) {
                        LabeledContent(device.name + " · " + String(device.id.suffix(6)), value: uiText(device.status.capitalized))
                        if device.id == model.authentication.deviceID {
                            Label(uiText("This device"), systemImage: "checkmark.circle")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                        LabeledContent(uiText("Last seen"), value: device.lastSeenAt.map(uiDate) ?? uiText("Never"))
                            .font(.caption).foregroundStyle(.secondary)
                        if device.status == "approved" {
                            Button(uiText("Revoke device"), role: .destructive) { showingRevoke = device }
                                .disabled(model.isBusy)
                        }
                    }
                }
            }
            Section {
                Button(uiText("Sign out"), role: .destructive) { showingSignOut = true }
                    .disabled(model.isBusy)
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
            Section(uiText("Your data")) {
                Text(uiText("Prepare a copy of authorized Cylo records. Large artifact files, derived stores, and backups are not included."))
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
        .fileExporter(isPresented: $showingExportSave, document: exportDocument, contentType: .zip, defaultFilename: model.exportArtifact?.filename ?? "cylo-export.zip") { result in
            switch result {
            case .success: model.exportSaveFinished(saved: true)
            case let .failure(error): model.exportSaveFinished(saved: false, error: error)
            }
        }
        .onChange(of: model.connectionEpoch) { _, _ in showingExportSave = false }
        .onChange(of: model.exportArtifact?.fileURL) { _, fileURL in
            if fileURL == nil { showingExportSave = false }
        }
        .confirmationDialog(String.localizedStringWithFormat(uiText("Revoke %@?"), showingRevoke?.name ?? uiText("this device")), isPresented: Binding(get: { showingRevoke != nil }, set: { if !$0 { showingRevoke = nil } })) {
            if let device = showingRevoke {
                Button(uiText("Revoke device"), role: .destructive) { Task { await model.revoke(device, reason: "Revoked in Cylo Settings"); showingRevoke = nil } }
            }
            Button(uiText("Cancel"), role: .cancel) { showingRevoke = nil }
        } message: {
            Text(uiText("Revoking this device signs it out, cancels its queued tasks, and requests its active tasks and runtimes to stop. Stop confirmation may require server cleanup.") +
                 (model.devices.filter { $0.status == "approved" }.count == 1
                  ? "\n\n" + uiText("This is your last signed-in device. You can sign in again with your owner password; if you forget it, contact your server administrator.") : ""))
        }
        .confirmationDialog(uiText("Sign out?"), isPresented: $showingSignOut) {
            Button(uiText("Sign out"), role: .destructive) { Task { await model.signOut() } }
            Button(uiText("Cancel"), role: .cancel) {}
        } message: {
            Text(uiText("This device will be signed out, its queued tasks cancelled, and its active tasks and runtimes requested to stop. Local drafts will be cleared."))
        }
    }
}

struct ScopedExportFileDocument: FileDocument {
    static var readableContentTypes: [UTType] { [.zip] }
    let fileURL: URL

    init(fileURL: URL) { self.fileURL = fileURL }

    init(configuration: ReadConfiguration) throws {
        throw CyloError.invalidResponse
    }

    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        FileWrapper(regularFileWithContents: try Data(contentsOf: fileURL, options: .mappedIfSafe))
    }
}
