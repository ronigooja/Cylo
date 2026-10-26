import CyloKit
import SwiftUI
import UniformTypeIdentifiers
import Vision
import ImageIO

/// The same connection editor is used at first launch and for explicit replacement.
struct InstanceConnectionView: View {
    @ObservedObject var model: CyloAppModel
    @Environment(\.dismiss) private var dismiss
    @State private var address = ""
    @State private var key = ""
    @State private var showingImageImport = false
    @State private var isDecoding = false
    @State private var confirmingClear = false
    @State private var importNotice: String?

    private var canConnect: Bool {
        !model.isBusy && !model.isConnecting && !isDecoding && (try? InstallationConnection(serverAddress: address, key: key)) != nil
    }
    private var replacing: Bool { model.authentication.isConnected }

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text(uiText(replacing ? "Replace instance key" : "Connect to Cylo")).font(.title2.weight(.semibold))
            TextField(uiText("Server URL"), text: $address)
                .disabled(replacing)
                .textContentType(.URL)
                .autocorrectionDisabled()
                #if os(iOS)
                .textInputAutocapitalization(.never)
                .keyboardType(.URL)
                #endif
            SecureField(uiText("Instance key"), text: $key)
                .autocorrectionDisabled()
                #if os(iOS)
                .textInputAutocapitalization(.never)
                #endif
            Button(uiText("Import QR image…"), systemImage: "qrcode") { showingImageImport = true }
                .disabled(isDecoding || model.isBusy || model.isConnecting)
            if !model.serverAddress.isEmpty && !replacing && model.pendingConnectionImport == nil {
                HStack {
                    Button(uiText("Reconnect")) { Task { await model.reconnect() } }
                    Button(uiText("Clear saved key…"), role: .destructive) { confirmingClear = true }
                }
                .disabled(model.isBusy || model.isConnecting)
            }
            if isDecoding { ProgressView(uiText("Reading QR code…")) }
            if address.lowercased().hasPrefix("http://") {
                Text(uiText("HTTP sends your instance key without encryption. Use only a trusted private connection."))
                    .font(.callout).foregroundStyle(.secondary)
            }
            if replacing {
                Text(uiText("Replacing the key clears queued local commands, caches, and drafts. Running server tasks continue."))
                    .font(.callout).foregroundStyle(.secondary)
            }
            if let message = importNotice ?? model.notice {
                Text(message).font(.callout).foregroundStyle(.secondary)
            }
            HStack {
                if replacing || model.pendingConnectionImport != nil {
                    Button(uiText("Cancel")) { model.dismissConnectionImport(); dismiss() }
                }
                Spacer()
                Button(uiText("Connect")) {
                    Task {
                        if await model.connect(serverAddress: address, key: key) {
                            key = ""
                            dismiss()
                        }
                    }
                }
                .buttonStyle(.borderedProminent).disabled(!canConnect)
                if model.isBusy || model.isConnecting { ProgressView().controlSize(.small) }
            }
        }
        .textFieldStyle(.roundedBorder)
        .padding(28)
        .frame(maxWidth: 460)
        .onAppear { loadDraft() }
        .onChange(of: model.pendingConnectionImport) { _, _ in loadDraft() }
        .onDisappear { key = "" }
        .confirmationDialog(uiText("Clear saved key?"), isPresented: $confirmingClear) {
            Button(uiText("Clear saved key"), role: .destructive) { Task { await model.clearCredential(); address = model.serverAddress; key = "" } }
            Button(uiText("Cancel"), role: .cancel) {}
        } message: {
            Text(uiText("The saved key, queued local commands, caches, and drafts will be removed from this app. The shared server key stays valid on other devices. Running server tasks continue."))
        }
        .fileImporter(isPresented: $showingImageImport, allowedContentTypes: [.image]) { result in
            switch result {
            case .failure: importNotice = uiText("The QR image could not be opened.")
            case .success(let url):
                isDecoding = true
                Task {
                    do {
                        let imported = try await Task.detached(priority: .userInitiated) { try InstallationQRImage.decode(url) }.value
                        address = imported.serverURL.absoluteString
                        key = try imported.key.bearerToken()
                        importNotice = nil
                    } catch { importNotice = uiText(error.localizedDescription) }
                    isDecoding = false
                }
            }
        }
    }

    private func loadDraft() {
        if let imported = model.pendingConnectionImport {
            address = imported.serverURL.absoluteString
            key = (try? imported.key.bearerToken()) ?? ""
        } else { address = model.serverAddress }
    }
}

/// Decode locally without uploading the image or automatically connecting to an imported address.
enum InstallationQRImage {
    static func decode(_ url: URL) throws -> InstallationConnection {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        let size = try url.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0
        guard size > 0, size <= 16 * 1024 * 1024,
              let source = CGImageSourceCreateWithURL(url as CFURL, nil),
              let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
              let width = properties[kCGImagePropertyPixelWidth] as? NSNumber,
              let height = properties[kCGImagePropertyPixelHeight] as? NSNumber,
              width.doubleValue * height.doubleValue <= 32_000_000,
              let image = CGImageSourceCreateImageAtIndex(source, 0, nil) else { throw ConnectionInputError.missingQRCode }
        let request = VNDetectBarcodesRequest()
        request.symbologies = [.qr]
        try VNImageRequestHandler(cgImage: image).perform([request])
        let payloads = Set((request.results ?? []).compactMap(\.payloadStringValue))
        guard !payloads.isEmpty else { throw ConnectionInputError.missingQRCode }
        guard payloads.count == 1, let payload = payloads.first else { throw ConnectionInputError.ambiguousQRCode }
        guard let link = URL(string: payload) else { throw ConnectionInputError.invalidLink }
        return try InstallationConnection(link: link)
    }
}
