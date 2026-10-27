import Foundation
import CryptoKit
import ZoeKit

public final class SnapshotCache {
    private let fileURL: URL
    private let fileManager: FileManager

    public init(serverURL: URL, credentialScope: String, fileManager: FileManager = .default) throws {
        self.fileManager = fileManager
        let root = try fileManager.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
        let directory = root.appendingPathComponent("Zoe", isDirectory: true).appendingPathComponent("Snapshots", isDirectory: true)
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true)
        var components = URLComponents(url: serverURL, resolvingAgainstBaseURL: false)
        components?.query = nil
        components?.fragment = nil
        let origin = components?.string ?? serverURL.absoluteString
        let digest = SHA256.hash(data: Data("\(origin)\u{0}\(credentialScope)".utf8)).map { String(format: "%02x", $0) }.joined()
        fileURL = directory.appendingPathComponent("\(digest).json")
    }

    public func discard() throws {
        if fileManager.fileExists(atPath: fileURL.path) { try fileManager.removeItem(at: fileURL) }
    }

    public func load() throws -> Snapshot? {
        guard fileManager.fileExists(atPath: fileURL.path) else { return nil }
        return try ZoeJSON.decoder.decode(Snapshot.self, from: Data(contentsOf: fileURL))
    }

    public func save(_ snapshot: Snapshot) throws {
        let data = try ZoeJSON.encoder.encode(snapshot)
        try data.write(to: fileURL, options: .atomic)
        #if os(iOS)
        try fileManager.setAttributes([.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication], ofItemAtPath: fileURL.path)
        #else
        try fileManager.setAttributes([.posixPermissions: NSNumber(value: 0o600)], ofItemAtPath: fileURL.path)
        #endif
    }
}
