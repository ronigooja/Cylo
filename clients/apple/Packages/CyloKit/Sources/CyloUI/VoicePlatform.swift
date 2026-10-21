import AVFAudio
import Foundation

/// Adapter contract for a pinned libwebrtc implementation. The app does not claim a call is active
/// until a concrete transport has created an offer and the server accepted the real media session.
public protocol WebRTCTransport: Sendable {
    func createOffer() async throws -> String
    func applyAnswer(_ answerSDP: String) async throws
    func close() async
}

public protocol VoiceAudioSessionAdapter: AnyObject {
    func activate() throws
    func deactivate() throws
}

#if os(iOS)
public final class AppleMobileAudioSessionAdapter: VoiceAudioSessionAdapter {
    private let session = AVAudioSession.sharedInstance()

    public init() {}

    public func activate() throws {
        try session.setCategory(.playAndRecord, mode: .voiceChat, options: [.allowBluetooth, .defaultToSpeaker])
        try session.setActive(true)
    }

    public func deactivate() throws {
        try session.setActive(false, options: .notifyOthersOnDeactivation)
    }
}
#elseif os(macOS)
public final class AppleDesktopAudioSessionAdapter: VoiceAudioSessionAdapter {
    private let engine = AVAudioEngine()

    public init() {}

    public func activate() throws {
        engine.prepare()
        try engine.start()
    }

    public func deactivate() throws {
        engine.stop()
    }
}
#endif

public enum VoiceIntegrationStatus: Equatable, Sendable {
    case unavailable(reason: String)
    case idle
    case connecting
    case active(sessionID: String, mediaGeneration: Int64)
    case interrupted(reason: String)
}
