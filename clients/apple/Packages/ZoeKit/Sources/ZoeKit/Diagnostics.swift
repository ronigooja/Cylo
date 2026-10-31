import Foundation
import CryptoKit

public enum DiagnosticLevel: String, Codable, CaseIterable, Sendable {
    case error, info, debug

    public static func parse(_ value: String?) -> DiagnosticLevel {
        guard let value, let level = DiagnosticLevel(rawValue: value.lowercased()) else { return .error }
        return level
    }

    fileprivate var rank: Int { self == .error ? 0 : (self == .info ? 1 : 2) }
    fileprivate func isAllowed(by policy: DiagnosticLevel) -> Bool { rank <= policy.rank }
}

public struct DiagnosticEvent: Codable, Equatable, Sendable {
    public let clientRequestID: String
    public let occurredAt: String
    public let appVersion: String
    public let osVersion: String
    public let deviceIDHash: String
    public let phase: String
    public let requestPath: String
    public let httpStatus: Int
    public let responseBytes: Int64
    public let errorType: String
    public let errorSummary: String
    public let level: String

    enum CodingKeys: String, CodingKey { case clientRequestID, occurredAt, appVersion, osVersion, deviceIDHash, phase, requestPath, httpStatus, responseBytes, errorType, errorSummary, level }

    public init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        clientRequestID = try values.decode(String.self, forKey: .clientRequestID)
        occurredAt = try values.decode(String.self, forKey: .occurredAt)
        appVersion = try values.decode(String.self, forKey: .appVersion)
        osVersion = try values.decode(String.self, forKey: .osVersion)
        deviceIDHash = try values.decode(String.self, forKey: .deviceIDHash)
        phase = try values.decode(String.self, forKey: .phase)
        requestPath = try values.decode(String.self, forKey: .requestPath)
        httpStatus = try values.decode(Int.self, forKey: .httpStatus)
        responseBytes = try values.decode(Int64.self, forKey: .responseBytes)
        errorType = try values.decode(String.self, forKey: .errorType)
        errorSummary = try values.decode(String.self, forKey: .errorSummary)
        level = try values.decodeIfPresent(String.self, forKey: .level) ?? DiagnosticLevel.error.rawValue
    }

    public init(clientRequestID: String, phase: String, requestPath: String, httpStatus: Int = 0, responseBytes: Int64 = 0, errorType: String = "", errorSummary: String = "", installationID: String?, appVersion: String? = nil, osVersion: String? = nil, level: DiagnosticLevel = .error) {
        self.clientRequestID = clientRequestID
        self.occurredAt = ISO8601DateFormatter().string(from: Date())
        self.appVersion = appVersion ?? Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "unknown"
        self.osVersion = osVersion ?? ProcessInfo.processInfo.operatingSystemVersionString
        let source = installationID ?? "unknown"
        self.deviceIDHash = SHA256.hash(data: Data(source.utf8)).map { String(format: "%02x", $0) }.joined()
        self.phase = phase
        self.requestPath = requestPath
        self.httpStatus = max(0, min(httpStatus, 599))
        self.responseBytes = max(0, responseBytes)
        self.errorType = String(errorType.prefix(64))
        self.errorSummary = String(errorSummary.prefix(512)).filter { $0.isASCII && $0 != "\n" && $0 != "\r" }
        self.level = level.rawValue
    }
}

public struct RedactedDiagnostic: Equatable, Sendable {
    public let httpStatus: Int
    public let errorType: String
    public let errorSummary: String

    public init(_ error: Error) {
        if let failure = error as? ZoeError {
            switch failure {
            case let .http(status, code, _, _):
                httpStatus = status
                errorType = "http_error"
                errorSummary = String(code.prefix(128))
            case .invalidResponse:
                httpStatus = 0; errorType = "ZoeError"; errorSummary = "invalid_response"
            case .persistence:
                httpStatus = 0; errorType = "ZoeError"; errorSummary = "persistence"
            case .credentialAccessDenied:
                httpStatus = 0; errorType = "ZoeError"; errorSummary = "credential_access_denied"
            case .missingCredential:
                httpStatus = 0; errorType = "ZoeError"; errorSummary = "missing_credential"
            case .invalidServerURL:
                httpStatus = 0; errorType = "ZoeError"; errorSummary = "invalid_server_url"
            case .exportTooLarge:
                httpStatus = 0; errorType = "ZoeError"; errorSummary = "export_too_large"
            case .pendingCommandWorkspaceMismatch:
                httpStatus = 0; errorType = "ZoeError"; errorSummary = "pending_command_workspace_mismatch"
            }
        } else {
            let nsError = error as NSError
            httpStatus = 0
            errorType = String(String(describing: type(of: error)).prefix(64))
            errorSummary = "\(nsError.domain)#\(nsError.code)"
        }
    }
}

actor EncryptedDiagnosticQueue {
    private let defaults: UserDefaults
    private let storageKey: String
    private let key: SymmetricKey
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()
    private var events: [DiagnosticEvent]
    private var retryAt = Date.distantPast
    private var retryDelay: TimeInterval = 1
    private var uploadInFlight = false
    private var level: DiagnosticLevel

    init?(serverURL: URL, installationID: String?) {
        guard let installationID, !installationID.isEmpty else { return nil }
        defaults = UserDefaults.standard
        storageKey = "zoe.diagnostics." + SHA256.hash(data: Data(serverURL.absoluteString.utf8)).map { String(format: "%02x", $0) }.joined()
        let levelKey = storageKey + ".level"
        level = DiagnosticLevel.parse(defaults.string(forKey: levelKey))
        key = SymmetricKey(data: SHA256.hash(data: Data((installationID + "|zoe-diagnostics-v1").utf8)))
        if let blob = defaults.data(forKey: storageKey), let sealed = try? AES.GCM.SealedBox(combined: blob), let data = try? AES.GCM.open(sealed, using: key), let decoded = try? decoder.decode([DiagnosticEvent].self, from: data) {
            events = Array(decoded.suffix(100))
        } else { events = [] }
    }

    func append(_ event: DiagnosticEvent) {
        guard DiagnosticLevel.parse(event.level).isAllowed(by: level) else { return }
        events.append(event)
        if events.count > 100 { events.removeFirst(events.count - 100) }
        persist()
    }

    func setLevel(_ newLevel: DiagnosticLevel) {
        level = newLevel
        defaults.set(newLevel.rawValue, forKey: storageKey + ".level")
    }

    func currentLevel() -> DiagnosticLevel { level }

    func take(limit: Int) -> [DiagnosticEvent] { Array(events.prefix(limit)) }

    func canAttempt() -> Bool { Date() >= retryAt }

    func beginUpload() -> Bool {
        guard !uploadInFlight else { return false }
        uploadInFlight = true
        return true
    }

    func endUpload() { uploadInFlight = false }

    func markFailure() {
        retryAt = Date().addingTimeInterval(retryDelay)
        retryDelay = min(retryDelay * 2, 300)
    }

    func markSuccess() {
        retryAt = .distantPast
        retryDelay = 1
    }

    func removeFirst(_ count: Int) {
        guard count > 0 else { return }
        events.removeFirst(min(count, events.count))
        persist()
    }

    private func persist() {
        guard let data = try? encoder.encode(events), let sealed = try? AES.GCM.seal(data, using: key), let combined = sealed.combined else { return }
        defaults.set(combined, forKey: storageKey)
    }
}
