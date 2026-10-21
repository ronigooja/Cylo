import Foundation

#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

public protocol BearerTokenProviding: Sendable {
    func bearerToken() throws -> String
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

    public func hangupVoiceSession(sessionID: String) async throws -> VoiceOperationResponse {
        let body = VoiceHangupRequest(mediaClosed: true)
        return try await request(path: ["v1", "voice", "sessions", sessionID, "hangup"], method: "POST", body: body, authenticated: true)
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
        var data = Data()
        for try await byte in bytes { data.append(byte) }
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

private struct VoiceGenerationRequest: Encodable {
    var generation: Int64
}

private struct VoiceHangupRequest: Encodable {
    var mediaClosed: Bool
}
