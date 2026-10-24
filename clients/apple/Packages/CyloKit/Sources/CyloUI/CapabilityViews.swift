import CyloKit
import SwiftUI

struct CapabilitiesView: View {
    @ObservedObject var model: CyloAppModel
    @State private var showingReleaseForm = false
    @State private var showingDeploymentForm = false
    @State private var authorizationTarget: CapabilityDeployment?
    @State private var revocationTarget: CapabilityDeployment?
    @State private var showingRevocationConfirmation = false
    @State private var releaseFormEpoch: UInt64 = 0
    @State private var deploymentFormEpoch: UInt64 = 0
    @State private var authorizationEpoch: UInt64 = 0
    @State private var revocationEpoch: UInt64 = 0

    var body: some View {
        List {
            FreshnessBanner(model: model)
            Section(uiText("Deployments")) {
                if model.capabilityDeployments.isEmpty {
                    ContentUnavailableView(uiText("No deployments"), systemImage: "shippingbox")
                }
                ForEach(model.capabilityDeployments) { deployment in
                    VStack(alignment: .leading, spacing: 8) {
                        Text(deployment.serviceId).font(.headline)
                        LabeledContent(uiText("Desired"), value: uiText(deployment.desiredState.replacingOccurrences(of: "_", with: " ").capitalized))
                        LabeledContent(uiText("Observed"), value: uiText(deployment.observedState.replacingOccurrences(of: "_", with: " ").capitalized))
                        LabeledContent(uiText("Operation"), value: uiText(deployment.requestedOperation.replacingOccurrences(of: "_", with: " ").capitalized))
                        LabeledContent(uiText("Version"), value: String(deployment.version))
                        if !deployment.grantId.isEmpty {
                            LabeledContent(uiText("Grant"), value: String(deployment.grantId.prefix(12)))
                        }
                        if !deployment.lastErrorCode.isEmpty {
                            Text(uiFormat("Latest error: %@", deployment.lastErrorCode)).font(.caption).foregroundStyle(.orange)
                        }
                        Text(uiFormat("Updated %@", uiDate(deployment.updatedAt))).font(.caption2).foregroundStyle(.tertiary)
                        if let revokedAt = deployment.revokedAt {
                            VStack(alignment: .leading, spacing: 4) {
                                Label(uiText("Authorization revoked"), systemImage: "hand.raised.slash")
                                    .foregroundStyle(.red)
                                Text(uiFormat("Revoked %@", uiDate(revokedAt)))
                                    .font(.caption)
                                if let revokedBy = deployment.revokedBy, !revokedBy.isEmpty {
                                    Text(uiFormat("By %@", revokedBy)).font(.caption2).foregroundStyle(.secondary)
                                }
                                if let cleanupStatus = deployment.revocationCleanupStatus {
                                    switch cleanupStatus {
                                    case let .pending(observedState):
                                        Label(uiFormat("Stop cleanup pending · runner last observed %@", uiText(observedState.capitalized)), systemImage: "clock")
                                            .foregroundStyle(.orange)
                                    case .confirmed:
                                        Label(uiText("Runner stop cleanup confirmed"), systemImage: "checkmark.circle")
                                            .foregroundStyle(.green)
                                    case let .unknown(observedState):
                                        Label(uiFormat("Stop cleanup status unknown · runner last observed %@", uiText(observedState.capitalized)), systemImage: "exclamationmark.triangle")
                                            .foregroundStyle(.red)
                                    case .uncertain:
                                        Label(uiText("Stop cleanup outcome is uncertain"), systemImage: "exclamationmark.triangle")
                                            .foregroundStyle(.red)
                                    }
                                }
                            }
                            .font(.caption)
                            .accessibilityElement(children: .combine)
                        } else if !deployment.grantId.isEmpty {
                            Button(uiText("Revoke authorization"), role: .destructive) {
                                revocationEpoch = model.connectionEpoch
                                revocationTarget = deployment
                                showingRevocationConfirmation = true
                            }
                            .disabled(model.isBusy)
                        }
                        if deployment.desiredState == "awaiting_authorization",
                           let release = model.capabilityReleases.first(where: { $0.digest == deployment.releaseDigest }) {
                            Button(uiText("Review authorization")) {
                                authorizationEpoch = model.connectionEpoch
                                authorizationTarget = deployment
                            }
                                .disabled(model.isBusy || release.digest.isEmpty)
                        }
                    }
                    .padding(.vertical, 4)
                }
            }
            Section(uiText("Immutable releases")) {
                if model.capabilityReleases.isEmpty {
                    ContentUnavailableView(uiText("No releases"), systemImage: "archivebox")
                }
                ForEach(model.capabilityReleases) { release in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(release.manifest.artifactRef).font(.headline).textSelection(.enabled)
                        DisclosureGroup(uiText("Technical details")) {
                            Text(release.digest).font(.caption).textSelection(.enabled)
                            Text(release.manifest.capabilities.joined(separator: ", ")).font(.caption)
                            Text(release.createdAt).font(.caption2).foregroundStyle(.secondary)
                        }
                    }
                    .padding(.vertical, 4)
                }
            }
        }
        .navigationTitle(uiText("Capabilities"))
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                Button(uiText("Refresh"), systemImage: "arrow.clockwise") { Task { await model.refreshCapabilities() } }
                Menu {
                    Button(uiText("Submit release…"), systemImage: "plus.square") {
                        releaseFormEpoch = model.connectionEpoch
                        showingReleaseForm = true
                    }
                    Button(uiText("Request deployment…"), systemImage: "shippingbox") {
                        deploymentFormEpoch = model.connectionEpoch
                        showingDeploymentForm = true
                    }
                        .disabled(model.capabilityReleases.isEmpty)
                } label: { Label(uiText("Actions"), systemImage: "ellipsis.circle") }
            }
        }
        .task { await model.refreshCapabilities() }
        .refreshable { await model.refreshCapabilities() }
        .sheet(isPresented: $showingReleaseForm) { CreateCapabilityReleaseSheet(model: model, connectionEpoch: releaseFormEpoch) }
        .sheet(isPresented: $showingDeploymentForm) { RequestCapabilityDeploymentSheet(model: model, connectionEpoch: deploymentFormEpoch) }
        .sheet(item: $authorizationTarget) { deployment in
            if let release = model.capabilityReleases.first(where: { $0.digest == deployment.releaseDigest }) {
                AuthorizeCapabilityDeploymentSheet(model: model, deployment: deployment, release: release, connectionEpoch: authorizationEpoch)
            }
        }
        .onChange(of: model.connectionEpoch) { _, _ in
            showingReleaseForm = false
            showingDeploymentForm = false
            authorizationTarget = nil
            showingRevocationConfirmation = false
            revocationTarget = nil
        }
        .confirmationDialog(
            String.localizedStringWithFormat(uiText("Revoke authorization for %@?"), revocationTarget?.serviceId ?? uiText("this service")),
            isPresented: $showingRevocationConfirmation
        ) {
            Button(uiText("Revoke authorization"), role: .destructive) {
                guard let deployment = revocationTarget else { return }
                Task {
                    if await model.revokeCapabilityDeployment(deployment, expectedConnectionEpoch: revocationEpoch) {
                        revocationTarget = nil
                    }
                }
            }
            Button(uiText("Cancel"), role: .cancel) { revocationTarget = nil }
        } message: {
            Text(uiText("Cylo will block new work immediately and queue stop cleanup. This action does not confirm that the runner has stopped the service; observed state will report that separately."))
        }
    }
}

struct CreateCapabilityReleaseSheet: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CyloAppModel
    let connectionEpoch: UInt64
    @State private var artifactRef = "oci://registry.example/service:1"
    @State private var artifactDigest = "sha256:"
    @State private var entrypoint = "/app/service"
    @State private var capabilities = ""
    @State private var cpuMillis: Int64 = 1000
    @State private var memoryMiB: Int64 = 512
    @State private var diskMiB: Int64 = 1024
    @State private var timeoutSeconds: Int64 = 300
    @State private var concurrency = 1
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            Form {
                Section(uiText("Immutable artifact")) {
                    TextField(uiText("Artifact reference (OCI or HTTPS)"), text: $artifactRef)
                    TextField(uiText("Artifact digest (sha256:…)"), text: $artifactDigest)
                        #if os(iOS)
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                        #endif
                    TextField(uiText("Entrypoint"), text: $entrypoint)
                    TextField(uiText("Capability IDs, comma separated"), text: $capabilities)
                    Text(uiText("Secret values cannot be entered here. Manifests may only refer to deployment-managed secret names."))
                        .font(.caption).foregroundStyle(.secondary)
                }
                Section(uiText("Maximum resource budget")) {
                    LabeledContent(uiText("CPU (millicores)")) {
                        TextField(uiText("1000"), value: $cpuMillis, format: .number)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                    }
                    LabeledContent(uiText("Memory (MiB)")) {
                        TextField(uiText("512"), value: $memoryMiB, format: .number)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                    }
                    LabeledContent(uiText("Disk (MiB)")) {
                        TextField(uiText("1024"), value: $diskMiB, format: .number)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                    }
                    LabeledContent(uiText("Timeout (seconds)")) {
                        TextField(uiText("300"), value: $timeoutSeconds, format: .number)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                    }
                    LabeledContent(uiText("Concurrency")) {
                        TextField(uiText("1"), value: $concurrency, format: .number)
                        #if os(iOS)
                        .keyboardType(.numberPad)
                        #endif
                    }
                    Text(uiText("Network access defaults to none. The server validates and canonicalizes this manifest before admission."))
                        .font(.caption).foregroundStyle(.secondary)
                }
                if let errorMessage {
                    Section(uiText("Submission failed")) {
                        Text(errorMessage).foregroundStyle(.red).textSelection(.enabled)
                    }
                }
            }
            .navigationTitle(uiText("Submit release"))
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(uiText("Cancel")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(uiText("Submit")) {
                        Task {
                            errorMessage = nil
                            if await model.submitCapabilityRelease(artifactRef: artifactRef, artifactDigest: artifactDigest, entrypoint: entrypoint, capabilities: capabilities, cpuMillis: cpuMillis, memoryBytes: memoryMiB * 1_048_576, diskBytes: diskMiB * 1_048_576, timeoutSeconds: timeoutSeconds, concurrency: concurrency, expectedConnectionEpoch: connectionEpoch) {
                                dismiss()
                            } else {
                                errorMessage = model.notice ?? uiText("The server did not accept this release. Your entries are still here.")
                            }
                        }
                    }
                    .disabled(model.isBusy || artifactRef.isEmpty || artifactDigest.isEmpty || entrypoint.isEmpty || capabilities.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || cpuMillis < 1 || memoryMiB < 1 || memoryMiB > Int64.max / 1_048_576 || diskMiB < 1 || diskMiB > Int64.max / 1_048_576 || timeoutSeconds < 1 || concurrency < 1)
                }
            }
        }
        .frame(idealWidth: 380, idealHeight: 520)
    }
}

struct RequestCapabilityDeploymentSheet: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CyloAppModel
    let connectionEpoch: UInt64
    @State private var serviceID = ""
    @State private var releaseDigest = ""
    @State private var operation = "install"
    @State private var errorMessage: String?
    private let operations = ["install", "start", "stop", "pause", "resume", "update", "uninstall"]

    var body: some View {
        NavigationStack {
            Form {
                TextField(uiText("Service ID"), text: $serviceID)
                Picker(uiText("Release"), selection: $releaseDigest) {
                    ForEach(model.capabilityReleases) { release in
                        Text("\(release.manifest.artifactRef) · \(release.digest.prefix(12))").tag(release.digest)
                    }
                }
                Picker(uiText("Requested operation"), selection: $operation) {
                    ForEach(operations, id: \.self) { Text(uiText($0.capitalized)).tag($0) }
                }
                Text(uiText("This records desired work as awaiting authorization. The runner has not acted yet."))
                    .font(.caption).foregroundStyle(.secondary)
                if let current = model.capabilityDeployments.first(where: { $0.serviceId == serviceID }) {
                    Text(String.localizedStringWithFormat(uiText("Current deployment version: %lld. The server will reject this request if the version changes."), current.version))
                        .font(.caption).foregroundStyle(.secondary)
                }
                if let errorMessage {
                    Text(errorMessage).foregroundStyle(.red).textSelection(.enabled)
                }
            }
            .navigationTitle(uiText("Request deployment"))
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(uiText("Cancel")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(uiText("Request")) {
                        Task {
                            errorMessage = nil
                            if await model.requestCapabilityDeployment(serviceID: serviceID, releaseDigest: releaseDigest, operation: operation, expectedConnectionEpoch: connectionEpoch) {
                                dismiss()
                            } else {
                                errorMessage = model.notice ?? uiText("The server did not accept this deployment request. Your entries are still here.")
                            }
                        }
                    }
                    .disabled(model.isBusy || serviceID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || releaseDigest.isEmpty)
                }
            }
        }
        .frame(idealWidth: 360, idealHeight: 300)
        .onAppear { if releaseDigest.isEmpty { releaseDigest = model.capabilityReleases.first?.digest ?? "" } }
    }
}

struct AuthorizeCapabilityDeploymentSheet: View {
    @Environment(\.dismiss) private var dismiss
    @ObservedObject var model: CyloAppModel
    let deployment: CapabilityDeployment
    let release: CapabilityRelease
    let connectionEpoch: UInt64
    @State private var expiresAt = Calendar.current.date(byAdding: .day, value: 7, to: .now) ?? .now
    @State private var errorMessage: String?

    var body: some View {
        NavigationStack {
            Form {
                Section(uiText("Authorization request")) {
                    LabeledContent(uiText("Service"), value: deployment.serviceId)
                    LabeledContent(uiText("Requested action"), value: uiText(deployment.requestedOperation.replacingOccurrences(of: "_", with: " ").capitalized))
                    LabeledContent(uiText("Release digest"), value: deployment.releaseDigest).textSelection(.enabled)
                    Text(uiText("The grant covers this action at the manifest's maximum budget. Authorizing queues the request for the runner; it does not claim that the deployment succeeded."))
                        .font(.caption).foregroundStyle(.secondary)
                }
                DatePicker("Grant expires", selection: $expiresAt, in: Date.now.addingTimeInterval(60)..., displayedComponents: [.date, .hourAndMinute])
                if let errorMessage {
                    Text(errorMessage).foregroundStyle(.red).textSelection(.enabled)
                }
            }
            .navigationTitle(uiText("Authorize deployment"))
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(uiText("Cancel")) { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button(uiText("Authorize & queue"), role: .destructive) {
                        Task {
                            errorMessage = nil
                            if await model.authorizeCapabilityDeployment(deployment, release: release, expiresAt: expiresAt, expectedConnectionEpoch: connectionEpoch) {
                                dismiss()
                            } else {
                                errorMessage = model.notice ?? uiText("The server did not accept this authorization. Your selection is still here.")
                            }
                        }
                    }
                    .disabled(model.isBusy || expiresAt <= .now)
                }
            }
        }
        .frame(idealWidth: 380, idealHeight: 340)
    }
}
