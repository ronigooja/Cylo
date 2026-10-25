import CyloKit
import SwiftUI

/// Shared by the main window and the macOS Settings scene.
struct OwnerLoginView: View {
    @ObservedObject var model: CyloAppModel
    @State private var password = ""
    @State private var confirmation = ""
    @State private var setupCode = ""
    @State private var showingHelp = false

    private var setupRequired: Bool {
        if case let .signedOut(required) = model.authentication { return required }
        return false
    }
    private var canSubmit: Bool {
        guard !model.isBusy, !model.isConnecting, !password.isEmpty else { return false }
        if setupRequired {
            return !setupCode.isEmpty && password == confirmation && OwnerPasswordPolicy.acceptsSetup(password)
        }
        return true
    }
    private var deviceName: String {
        #if os(macOS)
        "Mac"
        #else
        "iPhone / iPad"
        #endif
    }

    var body: some View {
        VStack(spacing: 20) {
            Image(systemName: "bubble.left.and.bubble.right.fill")
                .font(.system(size: 36)).foregroundStyle(.tint).accessibilityHidden(true)
            Text(uiText("Cylo")).font(.largeTitle.weight(.semibold))
            switch model.authentication {
            case .checking:
                ProgressView(uiText("Connecting…"))
            case .unavailable:
                if let notice = model.notice { Text(notice).foregroundStyle(.secondary) }
                Button(uiText("Try again")) { Task { await model.reconnect() } }
                    .disabled(model.isConnecting)
            case .signedOut:
                VStack(alignment: .leading, spacing: 14) {
                    Text(uiText(setupRequired ? "Set owner password" : "Sign in")).font(.title2.weight(.semibold))
                    if setupRequired {
                        SecureField(uiText("Setup code"), text: $setupCode)
                            .textContentType(.oneTimeCode)
                    }
                    SecureField(uiText("Owner password"), text: $password)
                        .textContentType(setupRequired ? .newPassword : .password)
                    if setupRequired {
                        SecureField(uiText("Confirm password"), text: $confirmation)
                            .textContentType(.newPassword)
                        Text(uiText("Use at least 12 characters. You will use this password to sign in on your other devices."))
                            .font(.footnote).foregroundStyle(.secondary)
                    }
                    if let notice = model.notice {
                        Text(notice).font(.callout).foregroundStyle(.secondary)
                            .accessibilityIdentifier("owner-login-notice")
                    }
                    Button {
                        Task {
                            let accepted = await model.login(password: password, code: setupRequired ? setupCode : nil, deviceName: deviceName)
                            password = ""
                            confirmation = ""
                            if accepted { setupCode = "" }
                        }
                    } label: {
                        HStack {
                            if model.isBusy { ProgressView().controlSize(.small) }
                            Text(uiText(setupRequired ? "Set password and sign in" : "Sign in"))
                        }.frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent).disabled(!canSubmit)
                    Button(uiText("Need help signing in?")) { showingHelp = true }
                        .buttonStyle(.plain).font(.callout).foregroundStyle(.tint)
                }
                .textFieldStyle(.roundedBorder)
                .disabled(model.isConnecting)
            case .authenticated:
                EmptyView()
            }
            if model.authentication.deviceID == nil {
                CredentialStorageControls(model: model)
            }
        }
        .frame(maxWidth: 360)
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(.background)
        .alert(uiText("Sign-in help"), isPresented: $showingHelp) {
            Button(uiText("Done"), role: .cancel) {}
        } message: {
            Text(uiText("Use the owner password for this Cylo server. If you forgot it or need a setup code, contact your server administrator. Password recovery signs out every device, cancels their queued tasks, and requests their active tasks and runtimes to stop."))
        }
        .task {
            if case .checking = model.authentication { await model.start() }
        }
    }
}


struct CredentialStorageControls: View {
    @ObservedObject var model: CyloAppModel
    @State private var destination: CredentialStorageChoice?
    @State private var showingConfirmation = false

    var body: some View {
        DisclosureGroup(uiText("Advanced settings")) {
            Picker(uiText("Store device identity"), selection: Binding(
                get: { model.credentialStorageChoice ?? .keychain },
                set: { choice in
                    guard choice != model.credentialStorageChoice else { return }
                    destination = choice
                    showingConfirmation = true
                }
            )) {
                ForEach(CredentialStorageChoice.allCases) { choice in
                    Text(uiText(choice.title)).tag(choice)
                }
            }
            Text(uiText(model.credentialStorageChoice?.explanation ?? CredentialStorageChoice.keychain.explanation))
                .font(.footnote).foregroundStyle(.secondary)
            if let current = model.credentialStorageChoice {
                let other: CredentialStorageChoice = current == .keychain ? .privateFile : .keychain
                Button(uiFormat("Copy current identity to %@", uiText(other.title))) {
                    Task { await model.copyCredentialIdentity(to: other); await model.reconnect() }
                }.disabled(model.isBusy || model.isConnecting)
            }
        }
        .font(.callout)
        .disabled(model.isBusy || model.isConnecting)
        .confirmationDialog(uiText("Change credential storage?"), isPresented: $showingConfirmation) {
            if let destination {
                Button(uiText("Copy current identity and switch")) {
                    Task { await model.copyCredentialIdentity(to: destination); await model.reconnect() }
                }
                Button(uiText("Switch without copying")) {
                    Task { await model.selectCredentialStorage(destination); await model.reconnect() }
                }
            }
            Button(uiText("Cancel"), role: .cancel) {}
        } message: {
            Text(uiText("Copying requires access to the current store and leaves the original intact. Switching without copying keeps the existing identity where it is. You can sign in with your owner password in the selected store."))
        }
    }
}
