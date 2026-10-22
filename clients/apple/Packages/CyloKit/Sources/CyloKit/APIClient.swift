import Foundation
import CryptoKit

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

public protocol BearerTokenProviding: Sendable {
    func bearerToken() throws -> String
}

public struct ScopedExportArtifact: Sendable {
    public let fileURL: URL
    public let filename: String
    public let scope: String
    public let snapshotCursor: String
    public let instanceLineageID: String
    public let generation: Int64
    public let deletionSequence: String

    public init(fileURL: URL, filename: String, scope: String, snapshotCursor: String, instanceLineageID: String = "", generation: Int64 = 0, deletionSequence: String = "0") {
        self.fileURL = fileURL
        self.filename = filename
        self.scope = scope
        self.snapshotCursor = snapshotCursor
        self.instanceLineageID = instanceLineageID
        self.generation = generation
        self.deletionSequence = deletionSequence
    }
}

public final class APIClient: @unchecked Sendable {
    public let serverURL: URL
    private let session: URLSession
    private let credentials: BearerTokenProviding?
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder

    public init(serverURL: URL, credentials: BearerTokenProviding? = nil, session: URLSession = .shared) throws {
        guard let scheme = serverURL.scheme?.lowercased(), ["http", "https"].contains(scheme), serverURL.host != nil else {
            throw CyloError.invalidServerURL
        }
        self.serverURL = serverURL
        self.credentials = credentials
        self.session = session
        encoder = JSONEncoder()
        encoder.keyEncodingStrategy = .convertToSnakeCase
        decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
    }

    public func bootstrap(code: String, deviceName: String, identity: DeviceIdentity) async throws -> BootstrapResponse {
        let body = BootstrapRequest(requestId: identity.bootstrapRequestID, code: code, deviceName: deviceName, deviceToken: identity.deviceToken)
        return try await request(path: ["v1", "bootstrap"], method: "POST", body: body, authenticated: false)
    }

    public func requestPairing(deviceName: String, identity: DeviceIdentity) async throws -> PairingResponse {
        let body = PairingRequest(requestId: identity.pairingRequestID, deviceName: deviceName, deviceToken: identity.deviceToken)
        return try await request(path: ["v1", "pairings"], method: "POST", body: body, authenticated: false)
    }

    public func pairingStatus(id: String, identity: DeviceIdentity) async throws -> PairingStatus {
        let url = try makeURL(path: ["v1", "pairings", id])
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.setValue("Bearer \(identity.deviceToken)", forHTTPHeaderField: "Authorization")
        return try await perform(request)
    }

    public func snapshot() async throws -> Snapshot {
        try await request(path: ["v1", "snapshot"], method: "GET", authenticated: true)
    }

    public func downloadExport(scope: String, temporaryDirectory: URL = FileManager.default.temporaryDirectory) async throws -> ScopedExportArtifact {
        guard scope == "personal" || scope == "workspace" else { throw CyloError.invalidResponse }
        var components = URLComponents(url: try makeURL(path: ["v1", "export"]), resolvingAgainstBaseURL: false)
        components?.queryItems = [URLQueryItem(name: "scope", value: scope)]
        guard let url = components?.url else { throw CyloError.invalidServerURL }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.setValue("application/vnd.cylo.export+zip", forHTTPHeaderField: "Accept")
        try authorize(&request)

        let (downloadURL, rawResponse) = try await session.download(for: request)
        defer { try? FileManager.default.removeItem(at: downloadURL) }
        guard let response = rawResponse as? HTTPURLResponse else { throw CyloError.invalidResponse }
        guard (200..<300).contains(response.statusCode) else {
            let attributes = try? FileManager.default.attributesOfItem(atPath: downloadURL.path)
            let size = (attributes?[.size] as? NSNumber)?.intValue ?? 0
            guard size <= 1 << 20 else { throw CyloError.invalidResponse }
            let data = try Data(contentsOf: downloadURL)
            throw decodeFailure(status: response.statusCode, data: data)
        }
        if let length = response.value(forHTTPHeaderField: "Content-Length").flatMap(UInt64.init), length > Self.maximumExportBytes {
            throw CyloError.exportTooLarge
        }
        let downloadedAttributes = try FileManager.default.attributesOfItem(atPath: downloadURL.path)
        let downloadedSize = (downloadedAttributes[.size] as? NSNumber)?.uint64Value
        guard let downloadedSize, downloadedSize <= Self.maximumExportBytes else {
            throw CyloError.exportTooLarge
        }
        if let length = response.value(forHTTPHeaderField: "Content-Length").flatMap(UInt64.init), length != downloadedSize {
            throw CyloError.invalidResponse
        }
        guard response.value(forHTTPHeaderField: "Content-Type")?.lowercased().hasPrefix("application/vnd.cylo.export+zip") == true,
              let filename = Self.safeExportFilename(response.value(forHTTPHeaderField: "Content-Disposition")) else {
            throw CyloError.invalidResponse
        }
        let archive: ValidatedExportArchive
        do {
            archive = try ValidatedExportArchive(fileURL: downloadURL,
                                                 archiveSHA256: response.value(forHTTPHeaderField: "X-Cylo-Archive-SHA256"),
                                                 manifestSHA256: response.value(forHTTPHeaderField: "X-Cylo-Manifest-SHA256"),
                                                 decoder: decoder)
        } catch { throw CyloError.invalidResponse }
        let manifest = archive.manifest
        let deletionSequence = Int64(manifest.authority.deletionSequence)
        guard manifest.format == "cylo.authority-export", manifest.schemaVersion == 2,
              manifest.scope == scope, !manifest.workspaceId.isEmpty, !manifest.subjectId.isEmpty,
              !manifest.authority.snapshotEventCursor.isEmpty, !manifest.authority.instanceLineageId.isEmpty,
              manifest.authority.generation > 0, let deletionSequence, deletionSequence >= 0 else { throw CyloError.invalidResponse }

        let fileURL = temporaryDirectory.appendingPathComponent("cylo-export-" + UUID().uuidString.lowercased() + ".zip", isDirectory: false)
        do {
            try FileManager.default.createDirectory(at: temporaryDirectory, withIntermediateDirectories: true)
            try FileManager.default.copyItem(at: downloadURL, to: fileURL)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: fileURL.path)
            return ScopedExportArtifact(fileURL: fileURL, filename: filename, scope: scope, snapshotCursor: manifest.authority.snapshotEventCursor, instanceLineageID: manifest.authority.instanceLineageId, generation: manifest.authority.generation, deletionSequence: manifest.authority.deletionSequence)
        } catch {
            try? FileManager.default.removeItem(at: fileURL)
            throw CyloError.persistence("The export could not be written to a private temporary file.")
        }
    }

    private static let maximumExportBytes: UInt64 = 1 << 30

    private static func safeExportFilename(_ header: String?) -> String? {
        guard let header, let marker = header.range(of: "filename=\""),
              let end = header[marker.upperBound...].firstIndex(of: "\"") else { return nil }
        let candidate = String(header[marker.upperBound..<end])
        guard candidate.hasPrefix("cylo-export-"), candidate.hasSuffix(".zip"),
              !candidate.contains("/"), !candidate.contains("\\"),
              candidate.dropFirst("cylo-export-".count).dropLast(".zip".count).allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-") }) else { return nil }
        return candidate
    }

    public func searchMemory(query: String = "", limit: Int = 50) async throws -> MemorySearchResult {
        var components = URLComponents(url: try makeURL(path: ["v1", "memory"]), resolvingAgainstBaseURL: false)
        components?.queryItems = [URLQueryItem(name: "q", value: query), URLQueryItem(name: "limit", value: String(limit))]
        guard let url = components?.url else { throw CyloError.invalidServerURL }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try authorize(&request)
        return try await perform(request)
    }

    public func voiceConfig() async throws -> VoiceConfig {
        try await request(path: ["v1", "voice", "config"], method: "GET", authenticated: true)
    }

    public func voicePoolStatus() async throws -> VoicePoolStatus {
        try await request(path: ["v1", "voice", "pool", "status"], method: "GET", authenticated: true)
    }

    public func notifications(unreadOnly: Bool = false, limit: Int = 50) async throws -> NotificationList {
        var components = URLComponents(url: try makeURL(path: ["v1", "notifications"]), resolvingAgainstBaseURL: false)
        components?.queryItems = [
            URLQueryItem(name: "unread", value: unreadOnly ? "true" : "false"),
            URLQueryItem(name: "limit", value: String(limit))
        ]
        guard let url = components?.url else { throw CyloError.invalidServerURL }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try authorize(&request)
        return try await perform(request)
    }

    public func capabilityReleases() async throws -> CapabilityReleaseList {
        try await request(path: ["v1", "capabilities", "releases"], method: "GET", authenticated: true)
    }

    public func capabilityDeployments() async throws -> CapabilityDeploymentList {
        try await request(path: ["v1", "capabilities", "deployments"], method: "GET", authenticated: true)
    }

    public func capabilityDeployment(id: String) async throws -> CapabilityDeployment {
        try await request(path: ["v1", "capabilities", "deployments", id], method: "GET", authenticated: true)
    }

    public func createVoiceSession(requestID: String, offerSDP: String, voice: String, voiceMode: String, languageCode: String) async throws -> VoiceSessionResponse {
        let body: [String: JSONValue] = [
            "request_id": .string(requestID),
            "offer_sdp": .string(offerSDP),
            "voice": .string(voice),
            "voice_mode": .string(voiceMode),
            "language_code": .string(languageCode)
        ]
        return try await request(path: ["v1", "voice", "sessions"], method: "POST", body: body, authenticated: true)
    }

    public func confirmVoiceConnected(sessionID: String, generation: Int64) async throws -> VoiceOperationResponse {
        let body = VoiceGenerationRequest(generation: generation)
        return try await request(path: ["v1", "voice", "sessions", sessionID, "connected"], method: "POST", body: body, authenticated: true)
    }

    public func heartbeatVoiceSession(sessionID: String) async throws -> VoiceOperationResponse {
        try await request(path: ["v1", "voice", "sessions", sessionID, "heartbeat"], method: "POST", body: EmptyObject(), authenticated: true)
    }

    public func hangupVoiceSession(_ session: VoiceSessionResponse) async throws -> VoiceOperationResponse {
        let body = VoiceHangupRequest(mediaClosed: true, generation: session.generation, accountLeaseId: session.accountLeaseId ?? "", accountLeaseGeneration: session.accountLeaseGeneration ?? 0)
        return try await request(path: ["v1", "voice", "sessions", session.voiceSessionId, "hangup"], method: "POST", body: body, authenticated: true)
    }

    public func receipt(id: String) async throws -> CommandReceipt {
        try await request(path: ["v1", "commands", id], method: "GET", authenticated: true)
    }

    public func receipt(idempotencyKey: String) async throws -> CommandReceipt {
        var components = URLComponents(url: try makeURL(path: ["v1", "commands"]), resolvingAgainstBaseURL: false)
        components?.queryItems = [URLQueryItem(name: "key", value: idempotencyKey)]
        guard let url = components?.url else { throw CyloError.invalidServerURL }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        try authorize(&request)
        return try await perform(request)
    }

    public func submit<Payload: Encodable & Sendable>(kind: String, expectedVersion: Int64? = nil, payload: Payload, idempotencyKey: String) async throws -> CommandReceipt {
        let envelope = CommandEnvelope(kind: kind, expectedVersion: expectedVersion, payload: payload)
        var request = URLRequest(url: try makeURL(path: ["v1", "commands"]))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue(idempotencyKey, forHTTPHeaderField: "Idempotency-Key")
        try authorize(&request)
        request.httpBody = try encoder.encode(envelope)
        return try await perform(request)
    }

    public func registerPushToken(platform: String, tokenHex: String, idempotencyKey: String = UUID().uuidString) async throws -> CommandReceipt {
        try await submit(kind: "device.push_token.register", payload: PushTokenRegistration(platform: platform, token: tokenHex), idempotencyKey: idempotencyKey)
    }

    public func revokePushToken(tokenID: String, idempotencyKey: String = UUID().uuidString) async throws -> CommandReceipt {
        try await submit(kind: "device.push_token.revoke", payload: PushTokenRevocation(tokenId: tokenID), idempotencyKey: idempotencyKey)
    }

    public func events(after cursor: String?) async throws -> URLSession.AsyncBytes {
        var request = URLRequest(url: try makeURL(path: ["v1", "events"]))
        request.httpMethod = "GET"
        request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
        if let cursor, !cursor.isEmpty {
            request.setValue(cursor, forHTTPHeaderField: "Last-Event-ID")
        }
        try authorize(&request)
        let (bytes, response) = try await session.bytes(for: request)
        guard let response = response as? HTTPURLResponse else { throw CyloError.invalidResponse }
        guard (200..<300).contains(response.statusCode) else {
            let data = try await collect(bytes)
            throw decodeFailure(status: response.statusCode, data: data)
        }
        return bytes
    }

    public func decode<T: Decodable>(_ type: T.Type, from data: Data) throws -> T {
        try decoder.decode(type, from: data)
    }

    private func request<Response: Decodable, Body: Encodable>(path: [String], method: String, body: Body? = Optional<NoBody>.none, authenticated: Bool) async throws -> Response {
        var request = URLRequest(url: try makeURL(path: path))
        request.httpMethod = method
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try encoder.encode(body)
        }
        if authenticated { try authorize(&request) }
        return try await perform(request)
    }

    private func request<Response: Decodable>(path: [String], method: String, authenticated: Bool) async throws -> Response {
        try await request(path: path, method: method, body: Optional<NoBody>.none, authenticated: authenticated)
    }

    private func perform<Response: Decodable>(_ request: URLRequest) async throws -> Response {
        let (data, response) = try await session.data(for: request)
        guard let response = response as? HTTPURLResponse else { throw CyloError.invalidResponse }
        guard (200..<300).contains(response.statusCode) else { throw decodeFailure(status: response.statusCode, data: data) }
        do { return try decoder.decode(Response.self, from: data) }
        catch { throw CyloError.invalidResponse }
    }

    private func authorize(_ request: inout URLRequest) throws {
        guard let credentials else { throw CyloError.missingCredential }
        request.setValue("Bearer \(try credentials.bearerToken())", forHTTPHeaderField: "Authorization")
    }

    private func makeURL(path: [String]) throws -> URL {
        var url = serverURL
        for component in path {
            url.appendPathComponent(component)
        }
        guard url.scheme != nil, url.host != nil else { throw CyloError.invalidServerURL }
        return url
    }

    private func decodeFailure(status: Int, data: Data) -> CyloError {
        if let body = try? decoder.decode(APIFailureBody.self, from: data) {
            return .http(status: status, code: body.error.code, message: body.error.message, details: body.error.details)
        }
        return .http(status: status, code: "http_error", message: "The server returned HTTP \(status).", details: nil)
    }

    private func collect(_ bytes: URLSession.AsyncBytes) async throws -> Data {
        try await collect(bytes, limit: Int.max)
    }

    private func collect(_ bytes: URLSession.AsyncBytes, limit: Int) async throws -> Data {
        var data = Data()
        for try await byte in bytes {
            guard data.count < limit else { throw CyloError.exportTooLarge }
            data.append(byte)
        }
        return data
    }
}

private struct CommandEnvelope<Payload: Encodable & Sendable>: Encodable {
    var kind: String
    var expectedVersion: Int64?
    var payload: Payload
}

private struct NoBody: Encodable {}

private struct EmptyObject: Encodable {}

private struct PushTokenRegistration: Encodable, Sendable {
    let platform: String
    let token: String
}

private struct PushTokenRevocation: Encodable, Sendable {
    let tokenId: String
}

private struct VoiceGenerationRequest: Encodable {
    var generation: Int64
}

private struct VoiceHangupRequest: Encodable {
    var mediaClosed: Bool
    var generation: Int64
    var accountLeaseId: String
    var accountLeaseGeneration: Int64
}
