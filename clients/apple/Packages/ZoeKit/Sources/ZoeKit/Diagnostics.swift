import Foundation
import CryptoKit

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

    public init(clientRequestID: String, phase: String, requestPath: String, httpStatus: Int = 0, responseBytes: Int64 = 0, errorType: String = "", errorSummary: String = "", installationID: String?, appVersion: String? = nil, osVersion: String? = nil) {
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

    init?(serverURL: URL, installationID: String?) {
        guard let installationID, !installationID.isEmpty else { return nil }
        defaults = UserDefaults.standard
        storageKey = "zoe.diagnostics." + SHA256.hash(data: Data(serverURL.absoluteString.utf8)).map { String(format: "%02x", $0) }.joined()
        key = SymmetricKey(data: SHA256.hash(data: Data((installationID + "|zoe-diagnostics-v1").utf8)))
        if let blob = defaults.data(forKey: storageKey), let sealed = try? AES.GCM.SealedBox(combined: blob), let data = try? AES.GCM.open(sealed, using: key), let decoded = try? decoder.decode([DiagnosticEvent].self, from: data) {
            events = Array(decoded.suffix(100))
        } else { events = [] }
    }

    func append(_ event: DiagnosticEvent) {
        events.append(event)
        if events.count > 100 { events.removeFirst(events.count - 100) }
        persist()
    }

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
