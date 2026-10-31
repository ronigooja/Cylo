import Foundation

public struct ClientPolicy: Codable, Equatable, Sendable {
    public let logLevel: DiagnosticLevel
    public let pollIntervalSeconds: Int
    public let revision: Int

    public init(logLevel: DiagnosticLevel, pollIntervalSeconds: Int, revision: Int) {
        self.logLevel = logLevel
        self.pollIntervalSeconds = min(600, max(30, pollIntervalSeconds))
        self.revision = revision
    }

    enum CodingKeys: String, CodingKey { case logLevel = "log_level", pollIntervalSeconds = "poll_interval_seconds", revision }

    public init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        self.init(logLevel: DiagnosticLevel.parse(try values.decodeIfPresent(String.self, forKey: .logLevel)),
                  pollIntervalSeconds: try values.decodeIfPresent(Int.self, forKey: .pollIntervalSeconds) ?? 60,
                  revision: try values.decodeIfPresent(Int.self, forKey: .revision) ?? 0)
    }
}

public actor ClientSyncLoop {
    private let api: APIClient
    private let sleep: @Sendable (Duration) async throws -> Void
    private var task: Task<Void, Never>?
    private var interval: Duration = .seconds(60)
    private var policyRevision = 0
    private var policyRetryAt = ContinuousClock.now
    private var policyRetryDelay: Duration = .seconds(1)

    public init(api: APIClient, sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.api = api
        self.sleep = sleep
    }

    public func start() {
        guard task == nil else { return }
        task = Task { [weak self] in await self?.run() }
    }

    public func cancel() {
        task?.cancel()
        task = nil
    }

    private func run() async {
        defer { task = nil }
        while !Task.isCancelled {
            await tick()
            do { try await sleep(interval) } catch { break }
        }
    }

    private func tick() async {
        guard api.hasCredentials else { return }
        do {
            let policy = try await api.fetchClientPolicy()
            if policy.revision >= policyRevision {
                policyRevision = policy.revision
                interval = .seconds(policy.pollIntervalSeconds)
                await api.applyDiagnosticLevel(policy.logLevel)
            }
            policyRetryAt = ContinuousClock.now
            policyRetryDelay = .seconds(1)
        } catch {
            if ContinuousClock.now >= policyRetryAt {
                await api.recordDiagnosticAndWait(phase: "policy_fetch", requestPath: "/v1/client/policy", errorType: "transport_error", errorSummary: RedactedDiagnostic(error).errorSummary)
                policyRetryAt = ContinuousClock.now + policyRetryDelay
                policyRetryDelay = min(policyRetryDelay * 2, .seconds(300))
            }
        }
        await api.uploadDiagnosticsIfPossible()
    }
}
