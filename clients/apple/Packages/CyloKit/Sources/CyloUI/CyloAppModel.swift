import Combine
import CryptoKit
import CyloKit
import Foundation
import WebRTC

@MainActor
public final class CyloAppModel: ObservableObject {
    @Published public var serverAddress: String
    @Published public private(set) var snapshot: Snapshot?
    @Published public private(set) var freshness: ClientFreshness = .loading
    @Published public private(set) var notice: String?
    @Published public private(set) var isBusy = false
    @Published public private(set) var pairingID: String?
    @Published public private(set) var pairingStatus: PairingStatus?
    @Published public private(set) var memoryClaims: [MemoryClaim] = []
    @Published public private(set) var memoryMissing: [String] = []
    @Published public private(set) var memoryConflicts: [String] = []
    @Published public private(set) var notifications: [NotificationRecord] = []
    @Published public private(set) var pushNotificationsEnabled: Bool
    @Published public private(set) var capabilityReleases: [CapabilityRelease] = []
    @Published public private(set) var capabilityDeployments: [CapabilityDeployment] = []
    @Published public private(set) var connectionEpoch: UInt64 = 0
    @Published public private(set) var voiceConfig: VoiceConfig?
    @Published public private(set) var voicePoolStatus: VoicePoolStatus?
    @Published public private(set) var voiceStatus: VoiceIntegrationStatus = .idle
    @Published public var voiceDraft = ""
    @Published public private(set) var voiceDraftRevision: Int64 = 0
    @Published public private(set) var exportArtifact: ScopedExportArtifact?
    @Published public private(set) var exportStatus: String?

    private let defaults: UserDefaults
    private var api: APIClient?
    private var identity: DeviceIdentityStore?
    private var journal: CommandJournal?
    private var coordinator: CommandCoordinator?
    private var eventTask: Task<Void, Never>?
    private var snapshotCache: SnapshotCache?
    private var streamBackoff: UInt64 = 1
    private var connectionGeneration = ConnectionGeneration()
    private var exportDownloadTask: Task<ScopedExportArtifact, Error>?
    private var exportRequestID: UUID?
    private var memoryRefreshSequence: UInt64 = 0
    private var memoryRefreshInFlight = false
    private var pendingMemoryRefreshQuery: String?
#if DEBUG
    var capabilityRevocationSubmitOverrideForTesting: ((Int64, JSONValue) async -> Bool)?
    var capabilityRevocationRefreshOverrideForTesting: (() async -> Void)?
    var exportDownloadOverrideForTesting: ((String) async throws -> ScopedExportArtifact)?
    var memorySearchOverrideForTesting: ((String) async throws -> MemorySearchResult)?
    var snapshotFetchOverrideForTesting: (() async throws -> Snapshot)?

    func setCapabilityDeploymentsForTesting(_ deployments: [CapabilityDeployment]) {
        capabilityDeployments = deployments
    }

    func setExportArtifactForTesting(_ artifact: ScopedExportArtifact?) {
        exportArtifact = artifact
    }

    func setMemoryClaimsForTesting(_ claims: [MemoryClaim]) {
        memoryClaims = claims
    }

    func handleEventForTesting(_ envelope: EventEnvelope, expectedConnectionEpoch: UInt64? = nil) async {
        await handle(.event(envelope), expectedGeneration: expectedConnectionEpoch ?? connectionGeneration.value, sourceAPI: nil)
    }

    func refreshSnapshotForTesting() async throws {
        try await refreshSnapshot()
    }

    func recoverPendingCommandsForTesting(api: APIClient, journal: CommandJournal, expectedGeneration: UInt64? = nil) async {
        self.api = api
        let testCoordinator = CommandCoordinator(api: api, journal: journal)
        coordinator = testCoordinator
        await recoverPendingCommands(expectedGeneration: expectedGeneration ?? connectionGeneration.value, sourceAPI: api, coordinator: testCoordinator)
    }
#endif
    private var configuredServerURL: URL?
    private var voiceTransport: LibWebRTCTransport?
    private var voiceAudioSession: VoiceAudioSessionAdapter?
    private var voiceSession: VoiceSessionResponse?
    private var voiceAPI: APIClient?
    private var voiceTurnID: String?
    private var voiceProviderMessageID: String?
    private var voiceHeartbeatTask: Task<Void, Never>?
    private var hasLoadedNotificationInbox = false
    private var pendingAPNSTokenHex: String?
    private var lastRegisteredAPNSTokenHex: String?
    private var tokenRegistrationInFlight: String?
    private var registeredPushTokenID: String?
    private var pushIdentityFingerprint: String?
    private var pushPreferenceEpoch: UInt64 = 0
    private var canRegisterPushToken = false

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        serverAddress = defaults.string(forKey: "cylo.serverAddress") ?? ""
        pushNotificationsEnabled = defaults.bool(forKey: "cylo.apns.optIn")
        if let url = URL(string: serverAddress), url.host != nil,
           let device = try? DeviceIdentityStore(serverURL: url).loadOrCreate(),
           let cache = try? SnapshotCache(serverURL: url, deviceToken: device.deviceToken), let cached = try? cache.load() {
            configuredServerURL = url
            pushIdentityFingerprint = Self.pushIdentityFingerprint(deviceToken: device.deviceToken)
            registeredPushTokenID = defaults.string(forKey: pushRegistrationKey(for: url, identityFingerprint: pushIdentityFingerprint ?? ""))
            snapshotCache = cache
            snapshot = cached
            freshness = .stale(reason: "Showing the last saved server snapshot.", cursor: cached.eventCursor)
        } else {
            snapshotCache = nil
        }
        RemotePushBridge.shared.onToken = { [weak self] token in
            Task { @MainActor [weak self] in self?.receiveAPNSToken(token) }
        }
        RemotePushBridge.shared.onWake = { [weak self] in
            await self?.refreshAfterRemoteWake()
        }
        RemotePushBridge.shared.onFailure = { [weak self] in
            Task { @MainActor [weak self] in self?.notice = "Remote notifications are unavailable on this installation. The Cylo inbox remains available." }
        }
    }

    deinit { eventTask?.cancel() }

    public var projects: [Project] { snapshot?.projects ?? [] }
    public var tasks: [TaskRecord] { snapshot?.tasks ?? [] }
    public var approvals: [Approval] { snapshot?.approvals ?? [] }
    public var devices: [Device] { snapshot?.devices ?? [] }
    public var runs: [Run] { snapshot?.runs ?? [] }
    public var canEndVoiceSession: Bool { voiceSession != nil }

    public func start() async {
        var generation = connectionGeneration.value
        do {
            let configuredAPI = try configure()
            generation = connectionGeneration.value
            if pushNotificationsEnabled { NativeNotificationDelivery.shared.registerForRemoteNotifications() }
            try await refreshSnapshot()
            guard generation == connectionGeneration.value else { return }
            await refreshMemory()
            guard generation == connectionGeneration.value else { return }
            await refreshNotifications()
            guard generation == connectionGeneration.value else { return }
            if let coordinator { await recoverPendingCommands(expectedGeneration: generation, sourceAPI: configuredAPI, coordinator: coordinator) }
            guard generation == connectionGeneration.value else { return }
            startEventLoop()
        } catch {
            if generation == connectionGeneration.value { setOffline(error) }
        }
    }

    public func reconnect() async { await start() }

    public func updateServerAddress() async {
        cancelExport(status: nil)
        clearVisibleMemory()
        connectionGeneration.advance()
        connectionEpoch = connectionGeneration.value
        capabilityReleases = []
        capabilityDeployments = []
        await endVoiceSession()
        if let url = URL(string: serverAddress), url.host != nil { connectionGeneration.switchOrigin(to: url) }
        else { connectionGeneration.advance() }
        connectionEpoch = connectionGeneration.value
        defaults.set(serverAddress.trimmingCharacters(in: .whitespacesAndNewlines), forKey: "cylo.serverAddress")
        eventTask?.cancel()
        eventTask = nil
        snapshot = nil
        freshness = .loading
        await start()
    }

    public func bootstrap(code: String, deviceName: String) async {
        var generation = connectionGeneration.value
        do {
            let api = try configure()
            generation = connectionGeneration.value
            guard let identity else { throw CyloError.missingCredential }
            _ = try identity.loadOrCreate()
            _ = try await api.bootstrap(code: code, deviceName: deviceName, identity: identity.loadOrCreate())
            guard generation == connectionGeneration.value else { return }
            notice = "This device is registered. Loading the server snapshot…"
            try await refreshSnapshot()
            guard generation == connectionGeneration.value else { return }
            startEventLoop()
        } catch {
            if generation == connectionGeneration.value { notice = userMessage(error) }
        }
    }

    public func requestPairing(deviceName: String) async {
        var generation = connectionGeneration.value
        do {
            let api = try configure()
            generation = connectionGeneration.value
            guard let identity else { throw CyloError.missingCredential }
            let result = try await api.requestPairing(deviceName: deviceName, identity: identity.loadOrCreate())
            guard generation == connectionGeneration.value else { return }
            pairingID = result.pairingId
            defaults.set(result.pairingId, forKey: pairingKey)
            let status = try await api.pairingStatus(id: result.pairingId, identity: identity.loadOrCreate())
            guard generation == connectionGeneration.value else { return }
            pairingStatus = status
            notice = "Pairing request sent. An approved device must approve this device before it can read Cylo data."
        } catch { if generation == connectionGeneration.value { notice = userMessage(error) } }
    }

    public func refreshPairing() async {
        var generation = connectionGeneration.value
        do {
            let api = try configure()
            generation = connectionGeneration.value
            guard let id = pairingID ?? defaults.string(forKey: pairingKey), let identity else { return }
            let status = try await api.pairingStatus(id: id, identity: identity.loadOrCreate())
            guard generation == connectionGeneration.value else { return }
            pairingID = id
            pairingStatus = status
            if pairingStatus?.status == "approved" {
                notice = "This device was approved. Loading the server snapshot…"
                try await refreshSnapshot()
                guard generation == connectionGeneration.value else { return }
                startEventLoop()
            }
        } catch { if generation == connectionGeneration.value { notice = userMessage(error) } }
    }

    public func createProject(name: String, description: String?) async {
        var payload: [String: JSONValue] = ["name": .string(name)]
        if let description, !description.isEmpty { payload["description"] = .string(description) }
        await send(kind: "project.create", payload: .object(payload))
    }

    public func remember(scope: String, scopeID: String?, subject: String, key: String, content: String) async {
        var scopePayload: [String: JSONValue] = ["kind": .string(scope)]
        if let scopeID { scopePayload["id"] = .string(scopeID) }
        let payload: JSONValue = .object([
            "scope": .object(scopePayload),
            "subject": .string(subject),
            "key": .string(key),
            "content": .string(content),
            "source_kind": .string("user_note")
        ])
        await send(kind: "memory.remember", payload: payload)
        await refreshMemory()
    }

    public func deleteMemory(_ memory: MemoryClaim) async {
        let payload: JSONValue = .object([
            "memory_id": .string(memory.id),
            "expected_revision": .integer(memory.revision),
            "reason": .string("user_requested")
        ])
        await send(kind: "memory.delete", expectedVersion: memory.revision, payload: payload)
        await refreshMemory()
    }

    public func markNotificationRead(_ notification: NotificationRecord) async {
        guard notification.readAt == nil else { return }
        await send(kind: "notification.mark_read", payload: .object(["notification_id": .string(notification.id)]))
        await refreshNotifications()
    }

    public func enablePushNotifications() async {
        pushPreferenceEpoch &+= 1
        let preferenceEpoch = pushPreferenceEpoch
        do {
            guard try await NativeNotificationDelivery.shared.requestPermission() else {
                notice = "System notification permission is disabled. The Cylo inbox remains available."
                return
            }
            guard preferenceEpoch == pushPreferenceEpoch else { return }
            pushNotificationsEnabled = true
            defaults.set(true, forKey: "cylo.apns.optIn")
            NativeNotificationDelivery.shared.registerForRemoteNotifications()
            await registerPendingAPNSToken()
            if pendingAPNSTokenHex == nil {
                notice = "Push wakeups are enabled. Waiting for APNs registration."
            } else if lastRegisteredAPNSTokenHex == pendingAPNSTokenHex {
                notice = "Push wakeups are enabled. Notification content remains in the Cylo inbox."
            }
        } catch {
            notice = "System notification permission could not be requested: \(error.localizedDescription)"
        }
    }

    public func disablePushNotifications() async {
        pushPreferenceEpoch &+= 1
        pushNotificationsEnabled = false
        defaults.set(false, forKey: "cylo.apns.optIn")
        NativeNotificationDelivery.shared.unregisterFromRemoteNotifications()
        pendingAPNSTokenHex = nil
        lastRegisteredAPNSTokenHex = nil
        if registeredPushTokenID != nil || tokenRegistrationInFlight != nil {
            defaults.set(true, forKey: pushRevocationPendingKey)
        }
        guard registeredPushTokenID != nil else {
            notice = "Push wakeups are disabled on this device."
            return
        }
        defaults.set(true, forKey: pushRevocationPendingKey)
        await revokePendingPushToken()
    }

    public func refreshAfterRemoteWake() async {
        guard canRegisterPushToken, api != nil else { return }
        do {
            try await refreshSnapshot()
            await refreshNotifications()
            await refreshMemory()
        } catch {
            if snapshot == nil { setOffline(error) }
            else { setStale("A push wakeup arrived, but the latest server state could not be loaded: \(userMessage(error))") }
        }
    }

    public func refreshNotifications(unreadOnly: Bool = false) async {
        guard let api else { return }
        let generation = connectionGeneration.value
        do {
            let result = try await api.notifications(unreadOnly: unreadOnly)
            guard generation == connectionGeneration.value, api === self.api else { return }
            let oldIDs = Set(notifications.map(\.id))
            notifications = result.notifications
            if hasLoadedNotificationInbox {
                for item in result.notifications where item.readAt == nil && !oldIDs.contains(item.id) {
                    await NativeNotificationDelivery.shared.deliver(item)
                }
            }
            hasLoadedNotificationInbox = true
        } catch {
            guard generation == connectionGeneration.value else { return }
            notice = "The durable notification inbox could not be refreshed: \(userMessage(error))"
        }
    }

    public func refreshMemory(query: String = "") async {
        #if DEBUG
        let testSearch = memorySearchOverrideForTesting
        #else
        let testSearch: ((String) async throws -> MemorySearchResult)? = nil
        #endif
        guard api != nil || testSearch != nil else { return }
        if memoryRefreshInFlight {
            memoryRefreshSequence &+= 1
            pendingMemoryRefreshQuery = query
            return
        }
        memoryRefreshInFlight = true
        defer { memoryRefreshInFlight = false }

        var nextQuery: String? = query
        while let currentQuery = nextQuery {
            nextQuery = nil
            memoryRefreshSequence &+= 1
            let requestSequence = memoryRefreshSequence
            let generation = connectionGeneration.value
            let capturedAPI = api
            do {
                let result: MemorySearchResult
                if let testSearch { result = try await testSearch(currentQuery) }
                else if let capturedAPI { result = try await capturedAPI.searchMemory(query: currentQuery) }
                else { throw CyloError.missingCredential }
                guard requestSequence == memoryRefreshSequence,
                      generation == connectionGeneration.value,
                      testSearch != nil || capturedAPI === self.api else {
                    nextQuery = pendingMemoryRefreshQuery
                    pendingMemoryRefreshQuery = nil
                    continue
                }
                memoryClaims = result.items
                memoryMissing = result.missing
                memoryConflicts = result.conflicts
            } catch {
                guard requestSequence == memoryRefreshSequence,
                      generation == connectionGeneration.value,
                      testSearch != nil || capturedAPI === self.api else {
                    nextQuery = pendingMemoryRefreshQuery
                    pendingMemoryRefreshQuery = nil
                    continue
                }
                if let failure = error as? CyloError,
                   case let .http(status, _, _, _) = failure, status == 401 || status == 403 {
                    clearMemoryAndPreparedExport(status: "Memory access was denied; cached memory and the prepared export were cleared.")
                } else {
                    notice = "Memory could not be refreshed: \(userMessage(error))"
                }
            }
            nextQuery = pendingMemoryRefreshQuery
            pendingMemoryRefreshQuery = nil
        }
    }

    public func refreshCapabilities() async {
        guard let api else { return }
        let generation = connectionGeneration.value
        do {
            async let releases = api.capabilityReleases()
            async let deployments = api.capabilityDeployments()
            let (nextReleases, nextDeployments) = try await (releases, deployments)
            guard generation == connectionGeneration.value, api === self.api else { return }
            capabilityReleases = nextReleases.items
            capabilityDeployments = nextDeployments.items
        } catch {
            guard generation == connectionGeneration.value else { return }
            notice = "Capability inventory could not be refreshed: \(userMessage(error))"
        }
    }

    public func submitCapabilityRelease(artifactRef: String, artifactDigest: String, entrypoint: String, capabilities: String, cpuMillis: Int64, memoryBytes: Int64, diskBytes: Int64, timeoutSeconds: Int64, concurrency: Int, expectedConnectionEpoch: UInt64) async -> Bool {
        guard expectedConnectionEpoch == connectionEpoch else {
            notice = "The server connection changed. Reopen this form and review its values before submitting."
            return false
        }
        let manifest: JSONValue = .object([
            "schema_version": .integer(1),
            "artifact_ref": .string(artifactRef),
            "artifact_digest": .string(artifactDigest),
            "entrypoint": .string(entrypoint),
            "capabilities": .array(capabilities.split(separator: ",").map { .string($0.trimmingCharacters(in: .whitespacesAndNewlines)) }.filter { if case .string(let value) = $0 { return !value.isEmpty }; return false }),
            "budget": .object([
                "cpu_millis": .integer(cpuMillis),
                "memory_bytes": .integer(memoryBytes),
                "disk_bytes": .integer(diskBytes),
                "timeout_seconds": .integer(timeoutSeconds),
                "concurrency": .integer(Int64(concurrency))
            ]),
            "network": .object(["mode": .string("none")])
        ])
        guard await send(kind: "capability.release.submit", payload: .object(["manifest": manifest])) else { return false }
        await refreshCapabilities()
        return true
    }

    public func requestCapabilityDeployment(serviceID: String, releaseDigest: String, operation: String, expectedConnectionEpoch: UInt64) async -> Bool {
        guard expectedConnectionEpoch == connectionEpoch,
              capabilityReleases.contains(where: { $0.digest == releaseDigest }) else {
            notice = "The server connection or release selection changed. Reopen this form and review the current inventory."
            return false
        }
        let payload: JSONValue = .object([
            "service_id": .string(serviceID),
            "release_digest": .string(releaseDigest),
            "operation": .string(operation)
        ])
        let expectedVersion = capabilityDeployments.version(forServiceID: serviceID)
        guard await send(kind: "capability.deployment.request", expectedVersion: expectedVersion, payload: payload) else { return false }
        await refreshCapabilities()
        return true
    }

    public func revokeCapabilityDeployment(_ deployment: CapabilityDeployment, expectedConnectionEpoch: UInt64) async -> Bool {
        guard expectedConnectionEpoch == connectionEpoch else { return false }
        guard let current = capabilityDeployments.first(where: { $0.deploymentId == deployment.deploymentId }),
              current.version == deployment.version,
              current.revokedAt == nil,
              !current.grantId.isEmpty else {
            await refreshRevocationInventory()
            guard expectedConnectionEpoch == connectionEpoch else { return false }
            notice = "This deployment changed or its authorization is already revoked. The latest capability inventory is being loaded."
            return false
        }

        let payload: JSONValue = .object(["deployment_id": .string(deployment.deploymentId)])
        let accepted: Bool
#if DEBUG
        if let submit = capabilityRevocationSubmitOverrideForTesting {
            accepted = await submit(deployment.version, payload)
        } else {
            accepted = await send(kind: "capability.deployment.revoke", expectedVersion: deployment.version, payload: payload)
        }
#else
        accepted = await send(kind: "capability.deployment.revoke", expectedVersion: deployment.version, payload: payload)
#endif
        guard expectedConnectionEpoch == connectionEpoch else { return false }
        guard accepted else {
            await refreshRevocationInventory()
            guard expectedConnectionEpoch == connectionEpoch else { return false }
            if let refreshed = capabilityDeployments.first(where: { $0.deploymentId == deployment.deploymentId }),
               refreshed.version != deployment.version {
                notice = "The deployment version changed. The latest capability inventory has been loaded; review it before trying again."
            }
            return false
        }

        await refreshRevocationInventory()
        guard expectedConnectionEpoch == connectionEpoch else { return false }
        notice = "Authorization revoked. Stop cleanup status is shown in the capability inventory."
        return true
    }

    private func refreshRevocationInventory() async {
#if DEBUG
        if let refresh = capabilityRevocationRefreshOverrideForTesting {
            await refresh()
            return
        }
#endif
        await refreshCapabilities()
    }

    public func authorizeCapabilityDeployment(_ deployment: CapabilityDeployment, release: CapabilityRelease, scopeKind: String = "workspace", scopeID: String? = nil, expiresAt: Date, expectedConnectionEpoch: UInt64) async -> Bool {
        guard expectedConnectionEpoch == connectionEpoch,
              capabilityDeployments.contains(where: { $0.deploymentId == deployment.deploymentId && $0.version == deployment.version }),
              capabilityReleases.contains(where: { $0.digest == release.digest && $0.digest == deployment.releaseDigest }) else {
            notice = "The server connection or deployment version changed. Reopen this request and review the current inventory."
            return false
        }
        var payload: [String: JSONValue] = [
            "deployment_id": .string(deployment.deploymentId),
            "scope_kind": .string(scopeKind),
            "actions": .array([.string(deployment.requestedOperation)]),
            "budget": .object([
                "cpu_millis": .integer(release.manifest.budget.cpuMillis),
                "memory_bytes": .integer(release.manifest.budget.memoryBytes),
                "disk_bytes": .integer(release.manifest.budget.diskBytes),
                "timeout_seconds": .integer(release.manifest.budget.timeoutSeconds),
                "concurrency": .integer(Int64(release.manifest.budget.concurrency))
            ]),
            "expires_at": .string(ISO8601DateFormatter().string(from: expiresAt))
        ]
        if let scopeID, !scopeID.isEmpty { payload["scope_id"] = .string(scopeID) }
        guard await send(kind: "capability.deployment.authorize", expectedVersion: deployment.version, payload: .object(payload)) else { return false }
        await refreshCapabilities()
        return true
    }

    public func refreshVoiceReadiness() async {
        guard let api = try? configure() else { return }
        let generation = connectionGeneration.value
        do {
            async let config = api.voiceConfig()
            async let pool = api.voicePoolStatus()
            let (nextConfig, nextPool) = try await (config, pool)
            guard generation == connectionGeneration.value, api === self.api else { return }
            voiceConfig = nextConfig
            voicePoolStatus = nextPool
            if voiceSession == nil { voiceStatus = .idle }
        } catch {
            guard generation == connectionGeneration.value else { return }
            voiceStatus = .unavailable(reason: userMessage(error))
        }
    }

    public func startVoice(voice: String? = nil, voiceMode: String? = nil, languageCode: String? = nil) async {
        guard voiceSession == nil else { return }
        let generation: UInt64
        let api: APIClient
        do {
            api = try configure()
            generation = connectionGeneration.value
        } catch {
            voiceStatus = .unavailable(reason: userMessage(error))
            return
        }

        var audio: VoiceAudioSessionAdapter?
        var transport: LibWebRTCTransport?
        var allocatedSession: VoiceSessionResponse?
        voiceStatus = .connecting
        do {
            let config = try await api.voiceConfig()
            guard generation == connectionGeneration.value else { throw CancellationError() }
            let pool = try await api.voicePoolStatus()
            guard generation == connectionGeneration.value else { throw CancellationError() }
            voiceConfig = config
            voicePoolStatus = pool
            guard !pool.blocked else { throw CyloError.http(status: 409, code: pool.reasonCode, message: "The voice account pool is quarantined until an operator confirms termination.", details: nil) }

            #if os(iOS)
            audio = AppleMobileAudioSessionAdapter()
            #elseif os(macOS)
            audio = AppleDesktopAudioSessionAdapter()
            #endif
            try audio?.activate()
            let iceServers = config.webRTC.iceServers.map { RTCIceServer(urlStrings: $0.urls) }
            transport = try LibWebRTCTransport(iceServers: iceServers, dataChannelLabel: config.webRTC.dataChannel.label)
            transport?.onDataMessage = { [weak self] data in
                Task { @MainActor in self?.receiveVoiceData(data, generation: generation) }
            }

            let offer = try await transport!.createOffer()
            guard generation == connectionGeneration.value else { throw CancellationError() }
            let created = try await api.createVoiceSession(
                requestID: UUID().uuidString.lowercased(),
                offerSDP: offer,
                voice: voice ?? config.defaults.voice,
                voiceMode: voiceMode ?? config.defaults.voiceMode,
                languageCode: languageCode ?? config.defaults.languageCode
            )
            allocatedSession = created
            guard generation == connectionGeneration.value else { throw CancellationError() }
            guard created.state == "active", !created.answerSdp.isEmpty else { throw CyloError.invalidResponse }
            try await transport!.applyAnswer(created.answerSdp)
            guard generation == connectionGeneration.value else { throw CancellationError() }
            try await transport!.waitUntilConnected()
            guard generation == connectionGeneration.value else { throw CancellationError() }
            let confirmed = try await api.confirmVoiceConnected(sessionID: created.voiceSessionId, generation: created.generation)
            guard generation == connectionGeneration.value else { throw CancellationError() }
            guard confirmed.ok else { throw CyloError.invalidResponse }

            voiceSession = created
            voiceAPI = api
            voiceTransport = transport
            voiceAudioSession = audio
            voiceTurnID = nil
            voiceProviderMessageID = nil
            voiceDraft = ""
            voiceDraftRevision = 0
            voiceStatus = .active(sessionID: created.voiceSessionId, mediaGeneration: created.generation)
            startVoiceHeartbeat(sessionID: created.voiceSessionId, api: api, generation: generation)
        } catch {
            await transport?.close()
            try? audio?.deactivate()
            var remainsQuarantined = false
            if let allocatedSession {
                let release = try? await api.hangupVoiceSession(sessionID: allocatedSession.voiceSessionId)
                remainsQuarantined = release == nil || release?.poolBlocked == true
                if remainsQuarantined {
                    let latestPool = try? await api.voicePoolStatus()
                    guard generation == connectionGeneration.value, api === self.api else { return }
                    voicePoolStatus = latestPool
                }
            }
            if generation == connectionGeneration.value {
                if remainsQuarantined, let allocatedSession {
                    voiceSession = allocatedSession
                    voiceAPI = api
                    voiceStatus = .interrupted(reason: "Voice setup failed and upstream termination is unverified; the pool remains isolated. Retry release after local media has closed. \(userMessage(error))")
                } else {
                    voiceStatus = .unavailable(reason: userMessage(error))
                }
            }
        }
    }

    public func endVoiceSession() async {
        voiceHeartbeatTask?.cancel()
        voiceHeartbeatTask = nil
        let session = voiceSession
        let api = voiceAPI
        await voiceTransport?.close()
        voiceTransport = nil
        try? voiceAudioSession?.deactivate()
        voiceAudioSession = nil
        guard let session, let api else {
            voiceSession = nil
            if case .unavailable = voiceStatus {} else { voiceStatus = .idle }
            return
        }
        let generation = connectionGeneration.value
        do {
            let result = try await api.hangupVoiceSession(sessionID: session.voiceSessionId)
            guard generation == connectionGeneration.value, voiceSession?.voiceSessionId == session.voiceSessionId else { return }
            let latestPool = result.poolBlocked == true ? try? await api.voicePoolStatus() : nil
            guard generation == connectionGeneration.value, voiceSession?.voiceSessionId == session.voiceSessionId else { return }
            if result.poolBlocked == true {
                voicePoolStatus = latestPool
                voiceStatus = .interrupted(reason: "Local media is closed. The upstream account pool remains quarantined until termination is verified.")
                voiceSession = nil
                voiceAPI = nil
            } else {
                voiceStatus = .idle
                voiceSession = nil
                voiceAPI = nil
            }
        } catch {
            guard generation == connectionGeneration.value, voiceSession?.voiceSessionId == session.voiceSessionId else { return }
            voiceStatus = .interrupted(reason: "Local media is closed, but server release is unconfirmed: \(userMessage(error))")
        }
    }

    public func confirmVoiceDraftAsTask(title: String? = nil) async {
        guard let session = voiceSession, let coordinator, !voiceDraft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              let turnID = voiceTurnID, voiceDraftRevision > 0 else { return }
        let generation = connectionGeneration.value
        let revision = voiceDraftRevision
        let workspaceID = snapshot?.devices.first?.workspaceId
        let text = voiceDraft.trimmingCharacters(in: .whitespacesAndNewlines)
        var commitFields: [String: JSONValue] = [
            "session_id": .string(session.voiceSessionId),
            "generation": .integer(session.generation),
            "turn_id": .string(turnID),
            "revision": .integer(revision),
            "content": .string(text),
            "user_confirmed": .bool(true)
        ]
        if let voiceProviderMessageID { commitFields["provider_message_id"] = .string(voiceProviderMessageID) }
        let commit = JSONValue.object(commitFields)
        isBusy = true
        defer { isBusy = false }
        do {
            _ = try await coordinator.submit(kind: "voice.turn.commit", payload: commit, workspaceID: workspaceID)
            guard generation == connectionGeneration.value, voiceSession?.voiceSessionId == session.voiceSessionId else { return }
            let taskTitle = title?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false ? title! : String(text.prefix(72))
            let sourceReference: JSONValue = .object([
                "kind": .string("voice_turn"),
                "voice_session_id": .string(session.voiceSessionId),
                "generation": .integer(session.generation),
                "turn_id": .string(turnID),
                "revision": .integer(revision)
            ])
            let createTask: JSONValue = .object([
                "title": .string(taskTitle),
                "goal": .string(text),
                "source": .string("voice"),
                "source_reference": sourceReference,
                "initial_message": .string(text)
            ])
            let taskReceipt = try await coordinator.submit(kind: "task.create", payload: createTask, workspaceID: workspaceID).receipt
            guard generation == connectionGeneration.value, voiceSession?.voiceSessionId == session.voiceSessionId else { return }
            guard let taskID = taskReceipt.resourceId, let version = taskReceipt.resourceVersion else { throw CyloError.invalidResponse }
            guard version > 0 else { throw CyloError.invalidResponse }
            notice = "Voice turn confirmed and linked to task \(taskID)."
            voiceDraft = ""
            voiceTurnID = nil
            voiceProviderMessageID = nil
            voiceDraftRevision = 0
            try await refreshSnapshot()
        } catch {
            if generation == connectionGeneration.value { notice = "The voice turn may be committed, but task creation needs attention: \(userMessage(error))" }
        }
    }

    public func updateVoiceDraft(_ text: String) {
        guard text != voiceDraft else { return }
        voiceDraft = text
        voiceDraftRevision += 1
    }

    public func setNotice(_ message: String) {
        notice = message
    }

    public func createTask(goal: String, title: String, projectID: String?) async {
        var payload: [String: JSONValue] = [
            "title": .string(title),
            "goal": .string(goal),
            "source": .string("user"),
            "initial_message": .string(goal)
        ]
        if let projectID { payload["project_id"] = .string(projectID) }
        await send(kind: "task.create", payload: .object(payload))
    }

    public func append(to task: TaskRecord, content: String) async {
        await send(kind: "task.append", expectedVersion: task.version, payload: .object(["task_id": .string(task.id), "content": .string(content)]))
    }

    public func stop(_ task: TaskRecord, reason: String? = nil) async {
        var payload: [String: JSONValue] = ["task_id": .string(task.id)]
        if let reason, !reason.isEmpty { payload["reason"] = .string(reason) }
        await send(kind: "task.stop", expectedVersion: task.version, payload: .object(payload))
    }

    public func decide(_ approval: Approval, decision: String) async {
        guard decision == "approve" || decision == "reject" else { return }
        let payload: [String: JSONValue] = [
            "task_id": .string(approval.taskId),
            "approval_id": .string(approval.id),
            "decision": .string(decision)
        ]
        await send(kind: "task.approve", expectedVersion: approval.version, payload: .object(payload))
    }

    public func resultRun(for task: TaskRecord) -> Run? {
        guard task.canAcceptResult else { return nil }
        let candidates = runs.filter {
            $0.taskId == task.id && $0.status == "verification_pending" && $0.terminationConfirmed
        }
        guard candidates.count == 1 else { return nil }
        return candidates[0]
    }

    public func decideResult(for task: TaskRecord, run: Run, decision: String) async {
        guard decision == "accept" || decision == "reject",
              task.canAcceptResult,
              run.taskId == task.id,
              run.status == "verification_pending",
              run.terminationConfirmed,
              resultRun(for: task)?.id == run.id else { return }
        let payload: [String: JSONValue] = [
            "task_id": .string(task.id),
            "run_id": .string(run.id),
            "decision": .string(decision)
        ]
        await send(kind: "task.accept_result", expectedVersion: task.version, payload: .object(payload))
    }

    public func approve(_ device: Device) async {
        await send(kind: "device.approve", expectedVersion: device.version, payload: .object(["device_id": .string(device.id)]))
    }

    public func revoke(_ device: Device, reason: String? = nil) async {
        var payload: [String: JSONValue] = ["device_id": .string(device.id)]
        if let reason { payload["reason"] = .string(reason) }
        await send(kind: "device.revoke", expectedVersion: device.version, payload: .object(payload))
    }

    public func task(id: String?) -> TaskRecord? {
        guard let id else { return nil }
        return tasks.first { $0.id == id }
    }

    public func prepareExport(scope: String, expectedConnectionEpoch: UInt64) async {
#if DEBUG
        let testDownload = exportDownloadOverrideForTesting
#else
        let testDownload: ((String) async throws -> ScopedExportArtifact)? = nil
#endif
        guard scope == "personal" || scope == "workspace",
              expectedConnectionEpoch == connectionGeneration.value,
              api != nil || testDownload != nil else {
            if expectedConnectionEpoch == connectionGeneration.value { exportStatus = "Connect an approved device before preparing an export." }
            return
        }
        discardExportArtifact()
        exportStatus = "Preparing the authorized export…"
        let generation = connectionGeneration.value
        let capturedAPI = api
        let requestID = UUID()
        exportRequestID = requestID
        let task = Task {
            if let testDownload { return try await testDownload(scope) }
            guard let capturedAPI else { throw CyloError.missingCredential }
            return try await capturedAPI.downloadExport(scope: scope)
        }
        exportDownloadTask = task
        defer {
            if requestID == exportRequestID {
                exportDownloadTask = nil
                exportRequestID = nil
            }
        }
        do {
            let artifact = try await task.value
            guard !Task.isCancelled, requestID == exportRequestID,
                  generation == connectionGeneration.value,
                  testDownload != nil || capturedAPI === self.api else {
                try? FileManager.default.removeItem(at: artifact.fileURL)
                if requestID == exportRequestID {
                    exportStatus = "Export canceled. The prepared temporary file was removed."
                }
                return
            }
            exportArtifact = artifact
            exportStatus = "Export prepared by the server. Choose Save to select where to keep the file."
        } catch {
            guard requestID == exportRequestID, generation == connectionGeneration.value else { return }
            exportStatus = error.localizedDescription
        }
    }

    public func exportSaveFinished(saved: Bool, error: Error? = nil) {
        guard let artifact = exportArtifact else { return }
        try? FileManager.default.removeItem(at: artifact.fileURL)
        exportArtifact = nil
        if saved { exportStatus = "Export saved on this device." }
        else if let error, (error as NSError).code != NSUserCancelledError { exportStatus = "The export could not be saved: \(error.localizedDescription)" }
        else { exportStatus = "Saving was canceled. The prepared temporary file was removed." }
    }

    public func cancelExport(status: String? = "Export canceled. The prepared temporary file was removed.") {
        exportRequestID = nil
        exportDownloadTask?.cancel()
        exportDownloadTask = nil
        discardExportArtifact()
        exportStatus = status
    }

    private func discardExportArtifact() {
        if let artifact = exportArtifact { try? FileManager.default.removeItem(at: artifact.fileURL) }
        exportArtifact = nil
    }

    public func projectName(_ id: String?) -> String {
        guard let id else { return "Unassigned" }
        return projects.first(where: { $0.id == id })?.name ?? "Project unavailable"
    }

    private var pairingKey: String { "cylo.pairingID.\(serverAddress)" }

    private func configure() throws -> APIClient {
        let trimmed = serverAddress.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let url = URL(string: trimmed), let scheme = url.scheme?.lowercased(), ["https", "http"].contains(scheme), url.host != nil else {
            throw CyloError.invalidServerURL
        }
        defaults.set(trimmed, forKey: "cylo.serverAddress")
        if let api, api.serverURL == url, coordinator != nil { return api }
        if configuredServerURL != url {
            cancelExport(status: nil)
            connectionGeneration.switchOrigin(to: url)
            configuredServerURL = url
            snapshot = nil
            freshness = .loading
            pairingID = defaults.string(forKey: "cylo.pairingID.\(serverAddress)")
            pairingStatus = nil
            clearVisibleMemory()
            notifications = []
            capabilityReleases = []
            capabilityDeployments = []
            hasLoadedNotificationInbox = false
            canRegisterPushToken = false
            lastRegisteredAPNSTokenHex = nil
            voiceConfig = nil
            voicePoolStatus = nil
            voiceDraft = ""
            voiceDraftRevision = 0
            voiceTurnID = nil
            voiceProviderMessageID = nil
            connectionEpoch = connectionGeneration.value
        }
        eventTask?.cancel()
        let identity = DeviceIdentityStore(serverURL: url)
        let api = try APIClient(serverURL: url, credentials: identity)
        let deviceIdentity = try identity.loadOrCreate()
        let identityFingerprint = Self.pushIdentityFingerprint(deviceToken: deviceIdentity.deviceToken)
        pushIdentityFingerprint = identityFingerprint
        if configuredServerURL == url {
            registeredPushTokenID = defaults.string(forKey: pushRegistrationKey(for: url, identityFingerprint: identityFingerprint))
        }
        let journal = try CommandJournal(serverURL: url, deviceToken: deviceIdentity.deviceToken)
        let cache = try SnapshotCache(serverURL: url, deviceToken: deviceIdentity.deviceToken)
        snapshotCache = cache
        if configuredServerURL == url, snapshot == nil, let cached = try cache.load() {
            snapshot = cached
            freshness = .stale(reason: "Showing the last saved server snapshot.", cursor: cached.eventCursor)
        }
        self.identity = identity
        self.api = api
        self.journal = journal
        coordinator = CommandCoordinator(api: api, journal: journal)
        pairingID = defaults.string(forKey: pairingKey)
        return api
    }

    @discardableResult
    private func send(kind: String, expectedVersion: Int64? = nil, payload: JSONValue) async -> Bool {
        guard let coordinator else { notice = "Connect to a Cylo server first."; return false }
        let generation = connectionGeneration.value
        isBusy = true
        defer { isBusy = false }
        do {
            let outcome = try await coordinator.submit(kind: kind, expectedVersion: expectedVersion, payload: payload, workspaceID: snapshot?.devices.first?.workspaceId)
            guard generation == connectionGeneration.value else { return false }
            do { try await refreshSnapshot() }
            catch {
                guard generation == connectionGeneration.value else { return false }
                setStale("The command was accepted, but the latest snapshot could not be loaded: \(userMessage(error))")
            }
            guard generation == connectionGeneration.value else { return false }
            if kind == "task.stop" {
                notice = "Stop intent recorded (receipt \(outcome.receipt.commandId)). Awaiting server confirmation that the runtime terminated."
            } else {
                notice = "\(outcome.receipt.kind) accepted by the server (receipt \(outcome.receipt.commandId))."
            }
            return true
        } catch {
            if generation == connectionGeneration.value { notice = userMessage(error) }
            return false
        }
    }

    private func refreshSnapshot() async throws {
        #if DEBUG
        let testFetch = snapshotFetchOverrideForTesting
        #else
        let testFetch: (() async throws -> Snapshot)? = nil
        #endif
        let capturedAPI = api
        guard capturedAPI != nil || testFetch != nil else { throw CyloError.invalidServerURL }
        let generation = connectionGeneration.value
        let cache = snapshotCache
        let latest: Snapshot
        do {
            if let testFetch { latest = try await testFetch() }
            else if let capturedAPI { latest = try await capturedAPI.snapshot() }
            else { throw CyloError.invalidServerURL }
        }
        catch {
            if generation == connectionGeneration.value, Self.isAccessDenied(error) {
                clearMemoryAndPreparedExport(status: "Server access changed; cached memory and the prepared export were cleared.")
            }
            throw error
        }
        guard generation == connectionGeneration.value,
              testFetch != nil || capturedAPI === self.api else { return }
        if let current = snapshot, !cursor(latest.eventCursor, isAtLeast: current.eventCursor) {
            throw CyloError.invalidResponse
        }
        snapshot = latest
        try cache?.save(latest)
        freshness = .current(serverVersion: latest.serverVersion, cursor: latest.eventCursor)
        canRegisterPushToken = true
        if pushNotificationsEnabled { await registerPendingAPNSToken() }
        else { await revokePendingPushToken() }
    }

    private var pushRegistrationKey: String {
        pushRegistrationKey(for: configuredServerURL ?? URL(string: serverAddress) ?? URL(fileURLWithPath: "/"), identityFingerprint: pushIdentityFingerprint ?? "")
    }

    private func pushRegistrationKey(for url: URL, identityFingerprint: String) -> String {
        "cylo.apns.tokenRegistrationID.\(url.absoluteString).\(identityFingerprint)"
    }

    private var pushRevocationPendingKey: String {
        pushRevocationPendingKey(for: configuredServerURL ?? URL(string: serverAddress) ?? URL(fileURLWithPath: "/"), identityFingerprint: pushIdentityFingerprint ?? "")
    }

    private func pushRevocationPendingKey(for url: URL, identityFingerprint: String) -> String {
        "cylo.apns.tokenRevocationPending.\(url.absoluteString).\(identityFingerprint)"
    }

    private func pushRegistrationOperationKey(for url: URL, identityFingerprint: String) -> String {
        "cylo.apns.tokenRegistrationOperation.\(url.absoluteString).\(identityFingerprint)"
    }

    private func pushRegistrationTokenDigestKey(for url: URL, identityFingerprint: String) -> String {
        "cylo.apns.tokenRegistrationDigest.\(url.absoluteString).\(identityFingerprint)"
    }

    private func pushRegisteredTokenDigestKey(for url: URL, identityFingerprint: String) -> String {
        "cylo.apns.registeredTokenDigest.\(url.absoluteString).\(identityFingerprint)"
    }

    private func pushRevocationOperationKey(for url: URL, identityFingerprint: String, tokenID: String) -> String {
        let tokenIDDigest = SHA256.hash(data: Data(tokenID.utf8)).map { String(format: "%02x", $0) }.joined()
        return "cylo.apns.tokenRevocationOperation.\(url.absoluteString).\(identityFingerprint).\(tokenIDDigest)"
    }

    private func pushRevocationQueueKey(for url: URL, identityFingerprint: String) -> String {
        "cylo.apns.tokenRevocationQueue.\(url.absoluteString).\(identityFingerprint)"
    }

    private static func pushIdentityFingerprint(deviceToken: String) -> String {
        SHA256.hash(data: Data(deviceToken.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    private func pushIdempotencyKey(operation: String, origin: URL, identityFingerprint: String, binding: String, operationID: String = UUID().uuidString) -> String {
        let material = "cylo.apns.\(operation)\u{0}\(origin.absoluteString)\u{0}\(identityFingerprint)\u{0}\(binding)\u{0}\(operationID)"
        return SHA256.hash(data: Data(material.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    private func receiveAPNSToken(_ data: Data) {
        guard pushNotificationsEnabled else { return }
        pendingAPNSTokenHex = data.map { String(format: "%02x", $0) }.joined()
        Task { await registerPendingAPNSToken() }
    }

    private func registerPendingAPNSToken() async {
        guard pushNotificationsEnabled, canRegisterPushToken,
              let tokenHex = pendingAPNSTokenHex, tokenHex.count == 64,
              tokenHex != lastRegisteredAPNSTokenHex,
              tokenHex != tokenRegistrationInFlight, let api else { return }
        let origin = api.serverURL
        let identityFingerprint = pushIdentityFingerprint ?? ""
        let tokenDigest = SHA256.hash(data: Data(tokenHex.utf8)).map { String(format: "%02x", $0) }.joined()
        if defaults.string(forKey: pushRegisteredTokenDigestKey(for: origin, identityFingerprint: identityFingerprint)) == tokenDigest {
            lastRegisteredAPNSTokenHex = tokenHex
            return
        }
        let generation = connectionGeneration.value
        let preferenceEpoch = pushPreferenceEpoch
        let registrationAPI = api
        let registrationOrigin = api.serverURL
        let operationKey = pushRegistrationOperationKey(for: registrationOrigin, identityFingerprint: identityFingerprint)
        let tokenDigestKey = pushRegistrationTokenDigestKey(for: registrationOrigin, identityFingerprint: identityFingerprint)
        tokenRegistrationInFlight = tokenHex
        defer { if tokenRegistrationInFlight == tokenHex { tokenRegistrationInFlight = nil } }
        if defaults.string(forKey: operationKey) != nil {
            switch await recoverPendingAPNSRegistration(api: registrationAPI, origin: registrationOrigin, identityFingerprint: identityFingerprint) {
            case .completed: return
            case .retryLater: return
            case .none: break
            }
            guard generation == connectionGeneration.value,
                  registrationAPI === self.api,
                  registrationOrigin == configuredServerURL,
                  identityFingerprint == pushIdentityFingerprint else {
                return
            }
        }
        let idempotencyKey = defaults.string(forKey: operationKey) ?? pushIdempotencyKey(operation: "register", origin: registrationOrigin, identityFingerprint: identityFingerprint, binding: tokenHex)
        defaults.set(idempotencyKey, forKey: operationKey)
        defaults.set(SHA256.hash(data: Data(tokenHex.utf8)).map { String(format: "%02x", $0) }.joined(), forKey: tokenDigestKey)
        do {
            let receipt = try await registrationAPI.registerPushToken(platform: "\(pushPlatform)", tokenHex: tokenHex, idempotencyKey: idempotencyKey)
            guard preferenceEpoch == pushPreferenceEpoch, pushNotificationsEnabled,
                  generation == connectionGeneration.value, registrationAPI === self.api else {
                if let resourceID = receipt.resourceId {
                    await revokeAcceptedPushToken(resourceID, api: registrationAPI, origin: registrationOrigin, identityFingerprint: identityFingerprint)
                }
                if pushNotificationsEnabled { Task { await self.registerPendingAPNSToken() } }
                return
            }
            if let resourceID = receipt.resourceId {
                registeredPushTokenID = resourceID
                defaults.set(resourceID, forKey: pushRegistrationKey)
            }
            if receipt.resourceId != nil {
                defaults.removeObject(forKey: operationKey)
                defaults.set(defaults.string(forKey: tokenDigestKey) ?? tokenDigest, forKey: pushRegisteredTokenDigestKey(for: registrationOrigin, identityFingerprint: identityFingerprint))
                defaults.removeObject(forKey: tokenDigestKey)
                defaults.removeObject(forKey: pushRevocationPendingKey(for: registrationOrigin, identityFingerprint: identityFingerprint))
                lastRegisteredAPNSTokenHex = tokenHex
            }
        } catch {
            if preferenceEpoch != pushPreferenceEpoch || !pushNotificationsEnabled {
                // The server may have accepted the request before the connection failed. Retry with
                // the same scoped idempotency key to recover its receipt, then revoke that resource.
                do {
                    let receipt = try await registrationAPI.registerPushToken(platform: "\(pushPlatform)", tokenHex: tokenHex, idempotencyKey: idempotencyKey)
                    if let resourceID = receipt.resourceId {
                        await revokeAcceptedPushToken(resourceID, api: registrationAPI, origin: registrationOrigin, identityFingerprint: identityFingerprint)
                    }
                } catch {
                    defaults.set(true, forKey: pushRevocationPendingKey(for: registrationOrigin, identityFingerprint: identityFingerprint))
                }
            } else if generation == connectionGeneration.value {
                notice = "The device could not register for push wakeups: \(userMessage(error))"
            }
        }
    }

    private func revokeAcceptedPushToken(_ tokenID: String, api: APIClient, origin: URL, identityFingerprint: String) async {
        let registrationKey = pushRegistrationKey(for: origin, identityFingerprint: identityFingerprint)
        let pendingKey = pushRevocationPendingKey(for: origin, identityFingerprint: identityFingerprint)
        let queueKey = pushRevocationQueueKey(for: origin, identityFingerprint: identityFingerprint)
        var queue = (defaults.string(forKey: queueKey).flatMap { try? JSONDecoder().decode([String].self, from: Data($0.utf8)) }) ?? []
        if !queue.contains(tokenID) { queue.append(tokenID) }
        defaults.set(String(data: (try? JSONEncoder().encode(queue)) ?? Data("[]".utf8), encoding: .utf8) ?? "[]", forKey: queueKey)
        defaults.removeObject(forKey: pushRegistrationOperationKey(for: origin, identityFingerprint: identityFingerprint))
        defaults.removeObject(forKey: pushRegistrationTokenDigestKey(for: origin, identityFingerprint: identityFingerprint))
        defaults.set(true, forKey: pendingKey)
        let operationKey = pushRevocationOperationKey(for: origin, identityFingerprint: identityFingerprint, tokenID: tokenID)
        let idempotencyKey = defaults.string(forKey: operationKey) ?? pushIdempotencyKey(operation: "revoke", origin: origin, identityFingerprint: identityFingerprint, binding: tokenID)
        defaults.set(idempotencyKey, forKey: operationKey)
        do {
            _ = try await api.revokePushToken(tokenID: tokenID, idempotencyKey: idempotencyKey)
            defaults.removeObject(forKey: operationKey)
            queue.removeAll { $0 == tokenID }
            if queue.isEmpty { defaults.removeObject(forKey: queueKey) }
            else { defaults.set(String(data: (try? JSONEncoder().encode(queue)) ?? Data("[]".utf8), encoding: .utf8) ?? "[]", forKey: queueKey) }
            if defaults.string(forKey: registrationKey) == tokenID {
                defaults.removeObject(forKey: registrationKey)
                defaults.removeObject(forKey: pushRegisteredTokenDigestKey(for: origin, identityFingerprint: identityFingerprint))
            }
            if queue.isEmpty { defaults.removeObject(forKey: pendingKey) }
            if configuredServerURL == origin, pushIdentityFingerprint == identityFingerprint {
                if registeredPushTokenID == tokenID { registeredPushTokenID = nil }
                if queue.isEmpty { lastRegisteredAPNSTokenHex = nil }
                if !pushNotificationsEnabled { notice = "Push wakeups are disabled on this device." }
            }
        } catch {
            if configuredServerURL == origin, pushIdentityFingerprint == identityFingerprint {
                notice = "Push wakeups are disabled locally, but the server could not confirm token revocation: \(userMessage(error))"
            }
        }
    }

    private func revokePendingPushToken() async {
        guard !pushNotificationsEnabled, canRegisterPushToken, let api else { return }
        if defaults.string(forKey: pushRegistrationOperationKey(for: api.serverURL, identityFingerprint: pushIdentityFingerprint ?? "")) != nil {
            _ = await recoverPendingAPNSRegistration(api: api, origin: api.serverURL, identityFingerprint: pushIdentityFingerprint ?? "")
        }
        guard defaults.bool(forKey: pushRevocationPendingKey) else { return }
        let origin = api.serverURL
        let identityFingerprint = pushIdentityFingerprint ?? ""
        var tokenIDs = (defaults.string(forKey: pushRevocationQueueKey(for: origin, identityFingerprint: identityFingerprint)).flatMap { try? JSONDecoder().decode([String].self, from: Data($0.utf8)) }) ?? []
        if let tokenID = registeredPushTokenID, !tokenIDs.contains(tokenID) { tokenIDs.append(tokenID) }
        if tokenIDs.isEmpty {
            defaults.removeObject(forKey: pushRevocationPendingKey)
            return
        }
        for tokenID in tokenIDs {
            await revokeAcceptedPushToken(tokenID, api: api, origin: origin, identityFingerprint: identityFingerprint)
        }
        guard defaults.bool(forKey: pushRevocationPendingKey) else { return }
        let generation = connectionGeneration.value
        guard generation == connectionGeneration.value, api === self.api else { return }
    }

    private enum PendingRegistrationRecovery {
        case none
        case completed
        case retryLater
    }

    private func recoverPendingAPNSRegistration(api: APIClient, origin: URL, identityFingerprint: String) async -> PendingRegistrationRecovery {
        let operationKey = pushRegistrationOperationKey(for: origin, identityFingerprint: identityFingerprint)
        let tokenDigestKey = pushRegistrationTokenDigestKey(for: origin, identityFingerprint: identityFingerprint)
        guard let idempotencyKey = defaults.string(forKey: operationKey) else { return .none }
        let generation = connectionGeneration.value
        let preferenceEpoch = pushPreferenceEpoch
        do {
            let receipt = try await api.receipt(idempotencyKey: idempotencyKey)
            defaults.removeObject(forKey: operationKey)
            let registeredDigest = defaults.string(forKey: tokenDigestKey)
            defaults.removeObject(forKey: tokenDigestKey)
            guard let resourceID = receipt.resourceId else { return .completed }
            let contextIsCurrent = generation == connectionGeneration.value
                && api === self.api
                && origin == configuredServerURL
                && identityFingerprint == pushIdentityFingerprint
                && preferenceEpoch == pushPreferenceEpoch
            let currentTokenDigest = pendingAPNSTokenHex.map { SHA256.hash(data: Data($0.utf8)).map { String(format: "%02x", $0) }.joined() }
            if contextIsCurrent, pushNotificationsEnabled, registeredDigest != nil, registeredDigest == currentTokenDigest {
                registeredPushTokenID = resourceID
                defaults.set(resourceID, forKey: pushRegistrationKey(for: origin, identityFingerprint: identityFingerprint))
                defaults.set(registeredDigest!, forKey: pushRegisteredTokenDigestKey(for: origin, identityFingerprint: identityFingerprint))
                lastRegisteredAPNSTokenHex = pendingAPNSTokenHex
                defaults.removeObject(forKey: pushRevocationPendingKey(for: origin, identityFingerprint: identityFingerprint))
                return .completed
            }
            await revokeAcceptedPushToken(resourceID, api: api, origin: origin, identityFingerprint: identityFingerprint)
            return .none
        } catch let CyloError.http(status, _, _, _) where status == 404 {
            defaults.removeObject(forKey: operationKey)
            defaults.removeObject(forKey: tokenDigestKey)
            let queueKey = pushRevocationQueueKey(for: origin, identityFingerprint: identityFingerprint)
            let registrationKey = pushRegistrationKey(for: origin, identityFingerprint: identityFingerprint)
            if defaults.string(forKey: queueKey) == nil,
               defaults.string(forKey: registrationKey) == nil {
                defaults.removeObject(forKey: pushRevocationPendingKey(for: origin, identityFingerprint: identityFingerprint))
            }
            return .none
        } catch {
            return .retryLater
        }
    }

    private var pushPlatform: String {
        #if os(iOS)
        "ios"
        #elseif os(macOS)
        "macos"
        #else
        "ios"
        #endif
    }

    private func startEventLoop() {
        guard let api else { return }
        eventTask?.cancel()
        eventTask = Task { [weak self] in
            guard let self else { return }
            await self.listen(using: api)
        }
    }

    private func startVoiceHeartbeat(sessionID: String, api: APIClient, generation: UInt64) {
        voiceHeartbeatTask?.cancel()
        voiceHeartbeatTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(15))
                if Task.isCancelled { return }
                do {
                    _ = try await api.heartbeatVoiceSession(sessionID: sessionID)
                } catch {
                    guard let self, generation == self.connectionGeneration.value else { return }
                    self.voiceStatus = .interrupted(reason: "Voice lease renewal failed: \(self.userMessage(error))")
                    return
                }
            }
        }
    }

    private func receiveVoiceData(_ data: Data, generation: UInt64) {
        guard generation == connectionGeneration.value, voiceSession != nil else { return }
        guard let outer = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else { return }
        var event = outer
        if outer["type"] as? String == "data_message", let encoded = outer["data"] as? String,
           let innerData = encoded.data(using: .utf8), let inner = (try? JSONSerialization.jsonObject(with: innerData)) as? [String: Any] {
            event = inner
        }
        let type = (event["type"] as? String) ?? ""
        var messageID: String?
        var transcript: String?
        if type == "chat_message_delta" {
            let payload = (event["payload"] as? [String: Any]) ?? event
            let delta = (payload["delta"] as? [String: Any]) ?? [:]
            let version = delta["v"] as? [String: Any]
            let message = version?["message"] as? [String: Any]
            let author = message?["author"] as? [String: Any]
            guard author?["role"] as? String == "user" else { return }
            messageID = message?["id"] as? String
            if let content = message?["content"] as? [String: Any], let parts = content["parts"] as? [[String: Any]] {
                transcript = parts.compactMap { part -> String? in
                    if part["content_type"] as? String == "audio_transcription", part["direction"] as? String == "out" { return nil }
                    return (part["transcript"] as? String) ?? (part["text"] as? String) ?? (part["content"] as? String)
                }.joined(separator: " ")
            }
        } else if type.contains("input_audio") && type.contains("transcription") {
            let payload = (event["payload"] as? [String: Any]) ?? event
            transcript = (event["transcript"] as? String) ?? (event["text"] as? String) ?? (payload["transcript"] as? String) ?? (payload["text"] as? String)
            messageID = (event["item_id"] as? String) ?? (payload["item_id"] as? String)
        }
        guard let transcript, !transcript.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return }
        let clean = transcript.trimmingCharacters(in: .whitespacesAndNewlines)
        if let messageID, messageID != voiceProviderMessageID {
            voiceProviderMessageID = messageID
            voiceTurnID = UUID().uuidString.lowercased()
            voiceDraftRevision = 0
        } else if voiceTurnID == nil {
            voiceTurnID = UUID().uuidString.lowercased()
        }
        guard voiceDraft != clean else { return }
        voiceDraft = clean
        voiceDraftRevision += 1
    }

    private func listen(using api: APIClient) async {
        let generation = connectionGeneration.value
        let stream = EventStreamClient(api: api)
        while !Task.isCancelled && generation == connectionGeneration.value {
            let cursor = snapshot?.eventCursor
            do {
                let next = try await stream.run(after: cursor) { [weak self] push in
                    guard let self else { return }
                    guard !Task.isCancelled else { return }
                    await self.handle(push, expectedGeneration: generation, sourceAPI: api)
                }
                if Task.isCancelled || generation != connectionGeneration.value { return }
                if let next, snapshot?.eventCursor != next {
                    try await refreshSnapshot()
                }
                streamBackoff = 1
                setStale("The event stream ended; reconnecting.")
            } catch {
                if Task.isCancelled || generation != connectionGeneration.value { return }
                if let error = error as? CyloError,
                   case let .http(status, _, _, _) = error,
                   status == 401 || status == 403 {
                    clearMemoryAndPreparedExport(status: "Server access changed; cached memory and the prepared export were cleared.")
                    setOffline(error)
                    return
                }
                if snapshot == nil { setOffline(error) } else { setStale("Connection interrupted: \(userMessage(error))") }
            }
            let delay = streamBackoff
            streamBackoff = min(streamBackoff * 2, 30)
            try? await Task.sleep(nanoseconds: delay * 1_000_000_000)
        }
    }

    private func handle(_ push: ServerPush, expectedGeneration: UInt64, sourceAPI: APIClient?) async {
        guard isCurrentConnection(expectedGeneration, sourceAPI: sourceAPI), !Task.isCancelled else { return }
        switch push {
        case let .event(envelope):
            let memoryChanged = envelope.aggregateType == "memory" && ["memory.revised", "memory.deleted", "memory.proposal_accepted"].contains(envelope.type)
            if memoryChanged {
                await handleMemoryOrAuthorizationChange(status: "Server memory changed; prepare a fresh export.")
                guard isCurrentConnection(expectedGeneration, sourceAPI: sourceAPI), !Task.isCancelled else { return }
            }
            setStale("A server change arrived; refreshing the authoritative snapshot.")
            do { try await refreshSnapshot() }
            catch {
                guard isCurrentConnection(expectedGeneration, sourceAPI: sourceAPI), !Task.isCancelled else { return }
                setStale("Snapshot refresh failed: \(userMessage(error))")
            }
            guard isCurrentConnection(expectedGeneration, sourceAPI: sourceAPI), !Task.isCancelled else { return }
            if envelope.type.hasPrefix("notification.") { await refreshNotifications() }
        case .snapshotRequired:
            clearMemoryAndPreparedExport(status: "Server history expired; cached memory and the prepared export were cleared.")
            setStale("The saved event cursor expired; loading a fresh snapshot.")
            do { try await refreshSnapshot() }
            catch {
                guard isCurrentConnection(expectedGeneration, sourceAPI: sourceAPI), !Task.isCancelled else { return }
                setStale("Snapshot repair failed: \(userMessage(error))")
            }
            guard isCurrentConnection(expectedGeneration, sourceAPI: sourceAPI), !Task.isCancelled else { return }
            await refreshMemory()
        }
    }

    private func isCurrentConnection(_ generation: UInt64, sourceAPI: APIClient?) -> Bool {
        generation == connectionGeneration.value && (sourceAPI == nil || sourceAPI === api)
    }

    private func handleMemoryOrAuthorizationChange(status: String) async {
        clearMemoryAndPreparedExport(status: status)
        await refreshMemory()
    }

    private func recoverPendingCommands(expectedGeneration: UInt64, sourceAPI: APIClient, coordinator: CommandCoordinator) async {
        do {
            let recovered = try await coordinator.recoverPending()
            guard isCurrentConnection(expectedGeneration, sourceAPI: sourceAPI) else { return }
            guard !recovered.isEmpty else { return }
            notice = "Recovered \(recovered.count) command receipt(s) from this device's durable queue."
            if recovered.contains(where: { $0.receipt.kind.hasPrefix("memory.") }) {
                clearMemoryAndPreparedExport(status: "A recovered memory command changed server memory; cached memory and the prepared export were cleared.")
            }
            do { try await refreshSnapshot() }
            catch {
                guard isCurrentConnection(expectedGeneration, sourceAPI: sourceAPI) else { return }
                setStale("Recovered commands, but the latest snapshot could not be loaded: \(userMessage(error))")
            }
            guard isCurrentConnection(expectedGeneration, sourceAPI: sourceAPI) else { return }
            await refreshMemoryAfterRecoveredCommands(recovered, expectedGeneration: expectedGeneration, sourceAPI: sourceAPI)
        } catch {
            if isCurrentConnection(expectedGeneration, sourceAPI: sourceAPI) { notice = "A saved command needs attention: \(userMessage(error))" }
        }
    }

    private func refreshMemoryAfterRecoveredCommands(_ outcomes: [CommandOutcome], expectedGeneration: UInt64, sourceAPI: APIClient?) async {
        guard outcomes.contains(where: { $0.receipt.kind.hasPrefix("memory.") }) else { return }
        guard isCurrentConnection(expectedGeneration, sourceAPI: sourceAPI) else { return }
        await refreshMemory()
    }

    private func clearMemoryAndPreparedExport(status: String) {
        clearVisibleMemory()
        cancelExport(status: status)
    }

    private func clearVisibleMemory() {
        memoryRefreshSequence &+= 1
        pendingMemoryRefreshQuery = nil
        memoryClaims = []
        memoryMissing = []
        memoryConflicts = []
    }

    private static func isAccessDenied(_ error: Error) -> Bool {
        guard let failure = error as? CyloError,
              case let .http(status, _, _, _) = failure else { return false }
        return status == 401 || status == 403
    }

    private func setStale(_ reason: String) {
        freshness = .stale(reason: reason, cursor: snapshot?.eventCursor ?? "")
    }

    private func setOffline(_ error: Error) {
        freshness = .offline(reason: userMessage(error), cursor: snapshot?.eventCursor)
        if snapshot == nil, let url = URL(string: serverAddress), url.host != nil,
           let device = try? identity?.loadOrCreate(),
           let cache = try? SnapshotCache(serverURL: url, deviceToken: device.deviceToken), let cached = try? cache.load() {
            snapshotCache = cache
            snapshot = cached
        }
    }

    private func userMessage(_ error: Error) -> String {
        if let error = error as? CyloError,
           case let .http(status, code, _, details) = error,
           status == 409, code == "version_conflict" {
            let current = details?["current_version"]
            return "This item changed on another device. The latest server state is being loaded. Current version: \(current.map { String(describing: $0) } ?? "unknown")."
        }
        return error.localizedDescription
    }

    private func cursor(_ candidate: String, isAtLeast baseline: String) -> Bool {
        guard candidate.allSatisfy(\.isNumber), baseline.allSatisfy(\.isNumber) else { return false }
        let left = candidate.drop(while: { $0 == "0" })
        let right = baseline.drop(while: { $0 == "0" })
        if left.count != right.count { return left.count > right.count }
        return left >= right
    }
}
