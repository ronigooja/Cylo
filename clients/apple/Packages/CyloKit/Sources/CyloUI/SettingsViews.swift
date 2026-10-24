import CyloKit
import SwiftUI

import UniformTypeIdentifiers

struct SettingsView: View {
    @ObservedObject var model: CyloAppModel
    @State private var deviceName = "Apple device"
    @State private var bootstrapCode = ""
    @State private var showingRevoke: Device?
    @State private var exportScope = "personal"
    @State private var showingExportSave = false
    @State private var validatingExportSave = false
    @State private var pendingCredentialStorageChoice: CredentialStorageChoice?
    @State private var showingCredentialStorageConfirmation = false

    private var exportDocument: ScopedExportFileDocument {
        ScopedExportFileDocument(fileURL: model.exportArtifact?.fileURL ?? FileManager.default.temporaryDirectory)
    }

    var body: some View {
        Form {
            if let notice = model.notice {
                Section(uiText("Connection status")) {
                    Text(notice).font(.callout).foregroundStyle(.secondary).textSelection(.enabled)
                }
            }
            Section(uiText("Server")) {
                TextField(uiText("Server URL"), text: $model.serverAddress)
                    #if os(iOS)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .keyboardType(.URL)
                    #endif
                Button(uiText("Save and connect")) { Task { await model.updateServerAddress() } }
                    .disabled(model.credentialStorageChoice == nil || model.isBusy)
                FreshnessBanner(model: model)
            }
            Section(uiText("Device authorization")) {
                TextField(uiText("Device name"), text: $deviceName)
                DisclosureGroup(uiText("First device setup")) {
                    SecureField(uiText("One-use bootstrap code"), text: $bootstrapCode)
                    Button(uiText("Bootstrap first approved device")) {
                        Task { if await model.bootstrap(code: bootstrapCode, deviceName: deviceName) { bootstrapCode = "" } }
                    }
                    .disabled(bootstrapCode.isEmpty || deviceName.isEmpty || model.isBusy || model.credentialStorageChoice == nil)
                }
                Button(uiText(model.pairingStatus?.status == "approved" ? "This device is approved" : "Request approval on this device")) {
                    Task { await model.requestPairing(deviceName: deviceName) }
                }
                .disabled(model.isBusy || model.pairingStatus?.status == "approved" || model.credentialStorageChoice == nil)
                if let pairing = model.pairingStatus {
                    LabeledContent(uiText("Pairing"), value: uiText(pairing.status.capitalized))
                    LabeledContent(uiText("Expires"), value: uiDate(pairing.expiresAt))
                    Button(uiText("Check approval")) { Task { await model.refreshPairing() } }
                }
            }
            Section(uiText("Credential storage")) {
                Picker(uiText("Store device identity"), selection: Binding(
                    get: { model.credentialStorageChoice?.rawValue ?? "" },
                    set: { rawValue in
                        guard let choice = CredentialStorageChoice(rawValue: rawValue) else { return }
                        if let current = model.credentialStorageChoice, current != choice {
                            pendingCredentialStorageChoice = choice
                            showingCredentialStorageConfirmation = true
                        } else {
                            Task { await model.selectCredentialStorage(choice) }
                        }
                    }
                )) {
                    Text(uiText("Choose storage…")).tag("")
                    ForEach(CredentialStorageChoice.allCases) { choice in
                        Text(uiText(choice.title)).tag(choice.rawValue)
                    }
                }
                Text(uiText(model.credentialStorageChoice?.explanation ?? "Choose credential storage before connecting or authorizing this device."))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                if model.credentialStorageChoice == .privateFile {
                    Text(uiText("A file identity is separate from any Keychain identity. To keep the same server registration, explicitly copy the existing identity while Keychain access is available. A new file identity may need approval from another approved device."))
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                if let current = model.credentialStorageChoice {
                    let destination: CredentialStorageChoice = current == .keychain ? .privateFile : .keychain
                    Button(String.localizedStringWithFormat(uiText("Copy current identity to %@"), uiText(destination.title))) {
                        Task { await model.copyCredentialIdentity(to: destination) }
                    }
                    .disabled(model.isBusy)
                }
            }
            Section(uiText("Devices")) {
                if model.devices.isEmpty { Text(uiText("No device records in the server snapshot.")).foregroundStyle(.secondary) }
                ForEach(model.devices) { device in
                    VStack(alignment: .leading, spacing: 8) {
                        LabeledContent(device.name, value: uiText(device.status.capitalized))
                        if device.status == "pending" {
                            Button(uiText("Approve device")) { Task { await model.approve(device) } }
                        } else if device.status == "approved" {
                            Button(uiText("Revoke device"), role: .destructive) { showingRevoke = device }
                        }
                    }
                }
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
                .disabled(model.exportStatus == "Preparing the authorized export…" || model.exportArtifact != nil)
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
        .navigationTitle(uiText("Settings"))
        .fileExporter(isPresented: $showingExportSave, document: exportDocument, contentType: .zip, defaultFilename: model.exportArtifact?.filename ?? "cylo-export.zip") { result in
            switch result {
            case .success: model.exportSaveFinished(saved: true)
            case let .failure(error): model.exportSaveFinished(saved: false, error: error)
            }
        }
        .onChange(of: model.connectionEpoch) { _, _ in showingExportSave = false }
        .onChange(of: model.serverAddress) { _, _ in model.reloadCredentialStorageChoiceForCurrentServer() }
        .onChange(of: model.exportArtifact?.fileURL) { _, fileURL in
            if fileURL == nil { showingExportSave = false }
        }
        .confirmationDialog(String.localizedStringWithFormat(uiText("Revoke %@?"), showingRevoke?.name ?? uiText("this device")), isPresented: Binding(get: { showingRevoke != nil }, set: { if !$0 { showingRevoke = nil } })) {
            if let device = showingRevoke {
                Button(uiText("Revoke device"), role: .destructive) { Task { await model.revoke(device, reason: "Revoked in Cylo Settings"); showingRevoke = nil } }
            }
            Button(uiText("Cancel"), role: .cancel) { showingRevoke = nil }
        } message: {
            Text(uiText("Revocation blocks future requests and disconnects its event stream. The server owns the revocation result."))
        }
        .confirmationDialog(uiText("Change credential storage?"), isPresented: $showingCredentialStorageConfirmation) {
            if let destination = pendingCredentialStorageChoice {
                Button(uiText("Copy current identity and switch")) {
                    Task { await model.copyCredentialIdentity(to: destination) }
                    pendingCredentialStorageChoice = nil
                }
                Button(uiText("Switch without copying")) {
                    Task { await model.selectCredentialStorage(destination) }
                    pendingCredentialStorageChoice = nil
                }
            }
            Button(uiText("Cancel"), role: .cancel) { pendingCredentialStorageChoice = nil }
        } message: {
            Text(uiText("Cylo will not copy or create an identity automatically. Copying requires access to the current store and leaves the original intact. Switching without copying keeps the existing identity where it is; the destination may need separate device approval."))
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
