import Foundation

public struct PendingCommand: Codable, Equatable, Identifiable, Sendable {
    public var idempotencyKey: String
    public var kind: String
    public var expectedVersion: Int64?
    public var payload: JSONValue
    public var createdAt: String
    public var id: String { idempotencyKey }
}

public actor CommandJournal {
    private let fileURL: URL
    private let fileManager: FileManager
    private let encoder: JSONEncoder
    private let decoder = JSONDecoder()

    public init(fileURL: URL? = nil, fileManager: FileManager = .default) throws {
        self.fileManager = fileManager
        if let fileURL {
            self.fileURL = fileURL
        } else {
            let root = try fileManager.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
            let directory = root.appendingPathComponent("Cylo", isDirectory: true)
            try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
            self.fileURL = directory.appendingPathComponent("pending-commands.json")
        }
        encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
    }

    public func all() throws -> [PendingCommand] {
        guard fileManager.fileExists(atPath: fileURL.path) else { return [] }
        do { return try decoder.decode([PendingCommand].self, from: Data(contentsOf: fileURL)) }
        catch { throw CyloError.persistence("Pending command journal is unreadable: \(error.localizedDescription)") }
    }

    public func append(_ command: PendingCommand) throws {
        var current = try all()
        guard !current.contains(where: { $0.idempotencyKey == command.idempotencyKey }) else { return }
        current.append(command)
        try save(current)
    }

    public func remove(idempotencyKey: String) throws {
        let current = try all().filter { $0.idempotencyKey != idempotencyKey }
        try save(current)
    }

    private func save(_ commands: [PendingCommand]) throws {
        do {
            let data = try encoder.encode(commands)
            try fileManager.createDirectory(at: fileURL.deletingLastPathComponent(), withIntermediateDirectories: true)
            try data.write(to: fileURL, options: .atomic)
            #if os(iOS)
            try fileManager.setAttributes([.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication], ofItemAtPath: fileURL.path)
            #endif
        } catch {
            throw CyloError.persistence("Could not persist pending command: \(error.localizedDescription)")
        }
    }
}

public struct CommandOutcome: Sendable {
    public let receipt: CommandReceipt
    public let recovered: Bool
}

public actor CommandCoordinator {
    private let api: APIClient
    private let journal: CommandJournal

    public init(api: APIClient, journal: CommandJournal) {
        self.api = api
        self.journal = journal
    }

    /// A command is journaled before POST. A retry first checks the durable receipt by key.
    public func submit(kind: String, expectedVersion: Int64? = nil, payload: JSONValue) async throws -> CommandOutcome {
        let command = PendingCommand(
            idempotencyKey: UUID().uuidString.lowercased(),
            kind: kind,
            expectedVersion: expectedVersion,
            payload: payload,
            createdAt: ISO8601DateFormatter().string(from: Date())
        )
        try await journal.append(command)
        return try await deliver(command)
    }

    public func recoverPending() async throws -> [CommandOutcome] {
        let commands = try await journal.all()
        var outcomes: [CommandOutcome] = []
        for command in commands {
            outcomes.append(try await deliver(command))
        }
        return outcomes
    }

    private func deliver(_ command: PendingCommand) async throws -> CommandOutcome {
        do {
            let receipt = try await api.receipt(idempotencyKey: command.idempotencyKey)
            try await journal.remove(idempotencyKey: command.idempotencyKey)
            return CommandOutcome(receipt: receipt, recovered: true)
        } catch let error as CyloError {
            guard case let .http(status, code, _, _) = error, status == 404 || code == "not_found" else { throw error }
        }

        do {
            let receipt = try await api.submit(kind: command.kind, expectedVersion: command.expectedVersion, payload: command.payload, idempotencyKey: command.idempotencyKey)
            try await journal.remove(idempotencyKey: command.idempotencyKey)
            return CommandOutcome(receipt: receipt, recovered: false)
        } catch {
            // Keep the exact key and payload. The next attempt resolves a lost response first.
            throw error
        }
    }
}
