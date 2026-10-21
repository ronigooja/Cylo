import Combine
import CyloKit
import Foundation

@MainActor
public final class CyloAppModel: ObservableObject {
    @Published public var serverAddress: String
    @Published public private(set) var snapshot: Snapshot?
    @Published public private(set) var freshness: ClientFreshness = .loading
    @Published public private(set) var notice: String?
    @Published public private(set) var isBusy = false
    @Published public private(set) var pairingID: String?
    @Published public private(set) var pairingStatus: PairingStatus?

    private let defaults: UserDefaults
    private var api: APIClient?
    private var identity: DeviceIdentityStore?
    private var journal: CommandJournal?
    private var coordinator: CommandCoordinator?
    private var eventTask: Task<Void, Never>?
    private var snapshotCache: SnapshotCache?
    private var streamBackoff: UInt64 = 1

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        serverAddress = defaults.string(forKey: "cylo.serverAddress") ?? ""
        if let url = URL(string: serverAddress), url.host != nil,
           let device = try? DeviceIdentityStore(serverURL: url).loadOrCreate(),
           let cache = try? SnapshotCache(serverURL: url, deviceToken: device.deviceToken), let cached = try? cache.load() {
            snapshotCache = cache
            snapshot = cached
            freshness = .stale(reason: "Showing the last saved server snapshot.", cursor: cached.eventCursor)
        } else {
            snapshotCache = nil
        }
    }

    deinit { eventTask?.cancel() }

    public var projects: [Project] { snapshot?.projects ?? [] }
    public var tasks: [TaskRecord] { snapshot?.tasks ?? [] }
    public var approvals: [Approval] { snapshot?.approvals ?? [] }
    public var devices: [Device] { snapshot?.devices ?? [] }
    public var runs: [Run] { snapshot?.runs ?? [] }

    public func start() async {
        do {
            _ = try configure()
            try await refreshSnapshot()
            do {
                let recovered = try await coordinator?.recoverPending() ?? []
                if !recovered.isEmpty {
                    notice = "Recovered \(recovered.count) command receipt(s) from this device's durable queue."
                    try await refreshSnapshot()
                }
            } catch {
                notice = "A saved command needs attention: \(userMessage(error))"
            }
            startEventLoop()
        } catch {
            setOffline(error)
        }
    }

    public func reconnect() async { await start() }

    public func updateServerAddress() async {
        defaults.set(serverAddress.trimmingCharacters(in: .whitespacesAndNewlines), forKey: "cylo.serverAddress")
        eventTask?.cancel()
        eventTask = nil
        snapshot = nil
        freshness = .loading
        await start()
    }

    public func bootstrap(code: String, deviceName: String) async {
        do {
            let api = try configure()
            guard let identity else { throw CyloError.missingCredential }
            _ = try identity.loadOrCreate()
            _ = try await api.bootstrap(code: code, deviceName: deviceName, identity: identity.loadOrCreate())
            notice = "This device is registered. Loading the server snapshot…"
            try await refreshSnapshot()
            startEventLoop()
        } catch {
            notice = userMessage(error)
        }
    }

    public func requestPairing(deviceName: String) async {
        do {
            let api = try configure()
            guard let identity else { throw CyloError.missingCredential }
            let result = try await api.requestPairing(deviceName: deviceName, identity: identity.loadOrCreate())
            pairingID = result.pairingId
            defaults.set(result.pairingId, forKey: pairingKey)
            pairingStatus = try await api.pairingStatus(id: result.pairingId, identity: identity.loadOrCreate())
            notice = "Pairing request sent. An approved device must approve this device before it can read Cylo data."
        } catch {
            notice = userMessage(error)
        }
    }

    public func refreshPairing() async {
        do {
            let api = try configure()
            guard let id = pairingID ?? defaults.string(forKey: pairingKey), let identity else { return }
            pairingID = id
            pairingStatus = try await api.pairingStatus(id: id, identity: identity.loadOrCreate())
            if pairingStatus?.status == "approved" {
                notice = "This device was approved. Loading the server snapshot…"
                try await refreshSnapshot()
                startEventLoop()
            }
        } catch { notice = userMessage(error) }
    }

    public func createProject(name: String, description: String?) async {
        var payload: [String: JSONValue] = ["name": .string(name)]
        if let description, !description.isEmpty { payload["description"] = .string(description) }
        await send(kind: "project.create", payload: .object(payload))
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
        eventTask?.cancel()
        let identity = DeviceIdentityStore(serverURL: url)
        let api = try APIClient(serverURL: url, credentials: identity)
        let deviceIdentity = try identity.loadOrCreate()
        let journal = try CommandJournal(serverURL: url, deviceToken: deviceIdentity.deviceToken)
        snapshotCache = try SnapshotCache(serverURL: url, deviceToken: deviceIdentity.deviceToken)
        self.identity = identity
        self.api = api
        self.journal = journal
        coordinator = CommandCoordinator(api: api, journal: journal)
        pairingID = defaults.string(forKey: pairingKey)
        return api
    }

    private func send(kind: String, expectedVersion: Int64? = nil, payload: JSONValue) async {
        guard let coordinator else { notice = "Connect to a Cylo server first."; return }
        isBusy = true
        defer { isBusy = false }
        do {
            let outcome = try await coordinator.submit(kind: kind, expectedVersion: expectedVersion, payload: payload, workspaceID: snapshot?.devices.first?.workspaceId)
            if kind == "task.stop" {
                notice = "Stop intent recorded (receipt \(outcome.receipt.commandId)). Awaiting server confirmation that the runtime terminated."
            } else {
                notice = "\(outcome.receipt.kind) accepted by the server (receipt \(outcome.receipt.commandId))."
            }
            do { try await refreshSnapshot() }
            catch { setStale("The command was accepted, but the latest snapshot could not be loaded: \(userMessage(error))") }
        } catch { notice = userMessage(error) }
    }

    private func refreshSnapshot() async throws {
        guard let api else { throw CyloError.invalidServerURL }
        let latest = try await api.snapshot()
        if let current = snapshot, !cursor(latest.eventCursor, isAtLeast: current.eventCursor) {
            throw CyloError.invalidResponse
        }
        snapshot = latest
        try snapshotCache?.save(latest)
        freshness = .current(serverVersion: latest.serverVersion, cursor: latest.eventCursor)
    }

    private func startEventLoop() {
        guard let api else { return }
        eventTask?.cancel()
        eventTask = Task { [weak self] in
            guard let self else { return }
            await self.listen(using: api)
        }
    }

    private func listen(using api: APIClient) async {
        let stream = EventStreamClient(api: api)
        while !Task.isCancelled {
            let cursor = snapshot?.eventCursor
            do {
                let next = try await stream.run(after: cursor) { [weak self] push in
                    guard let self else { return }
                    await self.handle(push)
                }
                if Task.isCancelled { return }
                if let next, snapshot?.eventCursor != next {
                    try await refreshSnapshot()
                }
                streamBackoff = 1
                setStale("The event stream ended; reconnecting.")
            } catch {
                if Task.isCancelled { return }
                if let error = error as? CyloError,
                   case let .http(status, _, _, _) = error,
                   status == 401 || status == 403 {
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

    private func handle(_ push: ServerPush) async {
        switch push {
        case .event:
            setStale("A server change arrived; refreshing the authoritative snapshot.")
            do { try await refreshSnapshot() }
            catch { setStale("Snapshot refresh failed: \(userMessage(error))") }
        case .snapshotRequired:
            setStale("The saved event cursor expired; loading a fresh snapshot.")
            do { try await refreshSnapshot() }
            catch { setStale("Snapshot repair failed: \(userMessage(error))") }
        }
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
