import ZoeKit
import SwiftUI
import UniformTypeIdentifiers
import Vision
import ImageIO

/// The same connection editor is used at first launch and for explicit replacement.
struct InstanceConnectionView: View {
    @ObservedObject var model: ZoeAppModel
    @Environment(\.dismiss) private var dismiss
    @State private var address = ""
    @State private var key = ""
    @State private var showingImageImport = false
    #if os(iOS)
    @State private var showingCamera = false
    @State private var scannedConnection: InstallationConnection?
    #endif
    @State private var isDecoding = false
    @State private var confirmingClear = false
    @State private var confirmingHTTP = false
    @State private var insecureImport: InstallationConnection?
    @State private var importNotice: String?

    private var canConnect: Bool {
        !model.isBusy && !model.isConnecting && !isDecoding && (try? InstallationConnection(serverAddress: address, key: key)) != nil
    }
    private var replacing: Bool { !model.serverAddress.isEmpty }

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text(uiText(replacing ? "Replace instance key" : "Connect to Zoe")).font(.title2.weight(.semibold))
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
            #if os(iOS)
            Button(uiText("Scan QR code"), systemImage: "qrcode.viewfinder") { showingCamera = true }
                .disabled(isDecoding || model.isBusy || model.isConnecting)
            #endif
            Button(uiText("Import QR image…"), systemImage: "qrcode") { showingImageImport = true }
                .disabled(isDecoding || model.isBusy || model.isConnecting)
            if replacing && !model.authentication.isConnected {
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
        .onDisappear { key = ""; insecureImport = nil }
        .confirmationDialog(uiText("Clear saved key?"), isPresented: $confirmingClear) {
            Button(uiText("Clear saved key"), role: .destructive) { Task { await model.clearCredential(); address = model.serverAddress; key = "" } }
            Button(uiText("Cancel"), role: .cancel) {}
        } message: {
            Text(uiText("The saved key, queued local commands, caches, and drafts will be removed from this app. The shared server key stays valid on other devices. Running server tasks continue."))
        }
        .alert(uiText("Connect without encryption?"), isPresented: $confirmingHTTP, presenting: insecureImport) { imported in
            Button(uiText("Connect")) {
                insecureImport = nil
                Task { await connectImported(imported, allowHTTP: true) }
            }
            Button(uiText("Cancel"), role: .cancel) { insecureImport = nil }
        } message: { imported in
            Text(imported.serverURL.absoluteString + "\n\n" + uiText("HTTP sends your instance key without encryption. Use only a trusted private connection."))
        }
        #if os(iOS)
        .sheet(isPresented: $showingCamera, onDismiss: {
            if let imported = scannedConnection {
                scannedConnection = nil
                Task { await connectImported(imported) }
            }
        }) {
            InstallationCameraScanner { result in
                showingCamera = false
                switch result {
                case .success(let imported): scannedConnection = imported
                case .failure(let error): importNotice = uiText(error.localizedDescription)
                }
            }
        }
        #endif
        .fileImporter(isPresented: $showingImageImport, allowedContentTypes: [.image]) { result in
            switch result {
            case .failure: importNotice = uiText("The QR image could not be opened.")
            case .success(let url):
                isDecoding = true
                Task {
                    do {
                        let imported = try await Task.detached(priority: .userInitiated) { try InstallationQRImage.decode(url) }.value
                        await connectImported(imported)
                    } catch { importNotice = uiText(error.localizedDescription) }
                    isDecoding = false
                }
            }
        }
    }

    @MainActor
    private func connectImported(_ imported: InstallationConnection, allowHTTP: Bool = false) async {
        if imported.serverURL.scheme == "http", !allowHTTP {
            insecureImport = imported
            confirmingHTTP = true
            return
        }
        // Keep the locked address visible if the imported code targets another server.
        if !replacing || imported.serverURL.absoluteString == model.serverAddress {
            address = imported.serverURL.absoluteString
            key = (try? imported.key.bearerToken()) ?? ""
        }
        importNotice = nil
        if await model.connectImportedConnection(imported) {
            key = ""
            dismiss()
        }
    }

    private func loadDraft() {
        if let imported = model.pendingConnectionImport {
            address = replacing ? model.serverAddress : imported.serverURL.absoluteString
            key = imported.serverURL.absoluteString == address ? ((try? imported.key.bearerToken()) ?? "") : ""
            if imported.serverURL.absoluteString != address {
                importNotice = uiText("Clear the saved key before connecting to a different server.")
            }
        } else { address = model.serverAddress }
    }
}

/// Decode locally without uploading or retaining the image.
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
        return try InstallationConnection.decodeQRPayloads((request.results ?? []).compactMap(\.payloadStringValue))
    }
}
