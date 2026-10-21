import Foundation

public struct Snapshot: Codable, Equatable, Sendable {
    public var serverVersion: String
    public var eventCursor: String
    public var projects: [Project]
    public var tasks: [TaskRecord]
    public var approvals: [Approval]
    public var devices: [Device]
    public var runs: [Run]

    public init(serverVersion: String, eventCursor: String, projects: [Project], tasks: [TaskRecord], approvals: [Approval], devices: [Device], runs: [Run]) {
        self.serverVersion = serverVersion
        self.eventCursor = eventCursor
        self.projects = projects
        self.tasks = tasks
        self.approvals = approvals
        self.devices = devices
        self.runs = runs
    }
}

public struct Project: Codable, Equatable, Identifiable, Sendable {
    public var id: String
    public var name: String
    public var description: String?
    public var status: String
    public var version: Int64
    public var createdAt: String
    public var updatedAt: String
}

public struct TaskRecord: Codable, Equatable, Identifiable, Sendable {
    public var id: String
    public var projectId: String?
    public var title: String
    public var goal: String
    public var source: String
    public var profileId: String?
    public var status: TaskStatus
    public var version: Int64
    public var createdAt: String
    public var updatedAt: String
    public var acceptedAt: String?
    public var startedAt: String?
    public var finishedAt: String?
    public var result: String?
    public var error: String?
    public var evidence: [Evidence]
}

public enum TaskStatus: String, Codable, CaseIterable, Sendable {
    case queued, active, waiting, awaitingUser = "awaiting_user", paused, complete, failed, canceled

    public var label: String {
        switch self {
        case .queued: "Queued"
        case .active: "Active"
        case .waiting: "Waiting"
        case .awaitingUser: "Awaiting your review"
        case .paused: "Paused"
        case .complete: "Complete"
        case .failed: "Failed"
        case .canceled: "Canceled"
        }
    }
}

public struct Evidence: Codable, Equatable, Identifiable, Sendable {
    public var id: String
    public var kind: String
    public var summary: String
    public var source: String
    public var createdAt: String
    public var verified: Bool
}

public struct Device: Codable, Equatable, Identifiable, Sendable {
    public var id: String
    public var workspaceId: String
    public var name: String
    public var status: String
    public var version: Int64
    public var createdAt: String
    public var approvedAt: String?
    public var lastSeenAt: String?
}

public struct Approval: Codable, Equatable, Identifiable, Sendable {
    public var id: String
    public var taskId: String
    public var status: String
    public var prompt: String
    public var version: Int64
    public var createdAt: String
    public var expiresAt: String?
    public var requestedAction: JSONValue?
    public var requestedActionDigest: String?
}

public struct Run: Codable, Equatable, Identifiable, Sendable {
    public var id: String
    public var taskId: String
    public var attemptId: String
    public var status: String
    public var desiredControl: String
    public var controlVersion: Int64
    public var startedAt: String
    public var stoppedAt: String?
    public var evidence: [Evidence]
}

public struct TaskEntry: Codable, Equatable, Identifiable, Sendable {
    public var id: String
    public var kind: String
    public var content: String
    public var authorDeviceId: String
    public var createdAt: String
}

public struct CommandReceipt: Codable, Equatable, Sendable {
    public var commandId: String
    public var workspaceId: String
    public var kind: String
    public var status: String
    public var resourceType: String
    public var resourceId: String
    public var resourceVersion: Int64
    public var acceptedAt: String
    public var eventCursor: String
}

public struct APIFailureBody: Codable, Sendable {
    public struct Failure: Codable, Sendable {
        public var code: String
        public var message: String
        public var details: [String: JSONValue]?
    }
    public var error: Failure
}

public enum CyloError: Error, LocalizedError, Sendable {
    case invalidServerURL
    case missingCredential
    case http(status: Int, code: String, message: String, details: [String: JSONValue]?)
    case invalidResponse
    case persistence(String)

    public var errorDescription: String? {
        switch self {
        case .invalidServerURL: "Enter a valid Cylo server URL."
        case .missingCredential: "This device has not been paired with the server."
        case let .http(_, _, message, _): message
        case .invalidResponse: "The server returned a response the app could not read."
        case let .persistence(message): message
        }
    }
}

public enum JSONValue: Codable, Equatable, Sendable {
    case string(String)
    case integer(Int64)
    case number(Double)
    case bool(Bool)
    case object([String: JSONValue])
    case array([JSONValue])
    case null

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() { self = .null }
        else if let value = try? container.decode(Bool.self) { self = .bool(value) }
        else if let value = try? container.decode(Int64.self) { self = .integer(value) }
        else if let value = try? container.decode(Double.self) { self = .number(value) }
        else if let value = try? container.decode(String.self) { self = .string(value) }
        else if let value = try? container.decode([String: JSONValue].self) { self = .object(value) }
        else if let value = try? container.decode([JSONValue].self) { self = .array(value) }
        else { throw DecodingError.dataCorruptedError(in: container, debugDescription: "Unsupported JSON value") }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case let .string(value): try container.encode(value)
        case let .integer(value): try container.encode(value)
        case let .number(value): try container.encode(value)
        case let .bool(value): try container.encode(value)
        case let .object(value): try container.encode(value)
        case let .array(value): try container.encode(value)
        case .null: try container.encodeNil()
        }
    }
}

public struct EventEnvelope: Codable, Equatable, Sendable {
    public var schemaVersion: Int
    public var eventId: String
    public var cursor: String
    public var type: String
    public var occurredAt: String
    public var workspaceId: String
    public var subjectId: String?
    public var aggregateType: String
    public var aggregateId: String
    public var aggregateVersion: Int64
    public var payload: JSONValue
}

public enum ClientFreshness: Equatable, Sendable {
    case loading
    case current(serverVersion: String, cursor: String)
    case stale(reason: String, cursor: String)
    case offline(reason: String, cursor: String?)
}

public struct PairingStatus: Codable, Sendable {
    public var pairingId: String
    public var status: String
    public var deviceId: String?
    public var approvedAt: String?
    public var expiresAt: String
}

public struct BootstrapRequest: Encodable, Sendable {
    public let requestId: String
    public let code: String
    public let deviceName: String
    public let deviceToken: String
}

public struct PairingRequest: Encodable, Sendable {
    public let requestId: String
    public let deviceName: String
    public let deviceToken: String
}

public struct BootstrapResponse: Decodable, Sendable {
    public var device: Device
}

public struct PairingResponse: Decodable, Sendable {
    public var pairingId: String
    public var status: String
    public var expiresAt: String
}
