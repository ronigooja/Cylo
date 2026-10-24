import CryptoKit
import Darwin
import Foundation

public enum CredentialStorageChoice: String, CaseIterable, Identifiable, Sendable {
    case keychain
    case privateFile

    public var id: String { rawValue }

    public var title: String {
        switch self {
        case .keychain: "Keychain (recommended)"
        case .privateFile: "Local private file"
        }
    }

    public var explanation: String {
        switch self {
        case .keychain:
            "The device identity stays in the system Keychain. The system may ask you to authorize Cylo to read it."
        case .privateFile:
            "The same identity format is stored in your user Application Support folder with private file permissions. It is not encrypted by Keychain, and it is separate from any existing Keychain identity."
        }
    }

    public func makeStore(serverURL: URL, bundleIdentifier: String = Bundle.main.bundleIdentifier ?? "com.cylo.client") -> any DeviceIdentityStoring {
        switch self {
        case .keychain:
            DeviceIdentityStore(serverURL: serverURL, bundleIdentifier: bundleIdentifier)
        case .privateFile:
            PrivateFileDeviceIdentityStore(serverURL: serverURL, bundleIdentifier: bundleIdentifier)
        }
    }

    public static func defaultsKey(serverURL: URL, bundleIdentifier: String = Bundle.main.bundleIdentifier ?? "com.cylo.client") -> String {
        "cylo.credentialStorage.\(bundleIdentifier).\(CredentialStorageOrigin.digest(for: serverURL))"
    }
}

public final class PrivateFileDeviceIdentityStore: DeviceIdentityStoring, @unchecked Sendable {
    private let fileURL: URL
    private let directoryURL: URL
    private let supportDirectoryURL: URL
    private let deviceIdentitiesDirectoryURL: URL
    private let appDirectoryURL: URL
    private let fileManager: FileManager
    private let lock = NSLock()

    public init(
        serverURL: URL,
        bundleIdentifier: String = Bundle.main.bundleIdentifier ?? "com.cylo.client",
        applicationSupportDirectory: URL? = nil,
        fileManager: FileManager = .default
    ) {
        let support = applicationSupportDirectory
            ?? fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? URL(fileURLWithPath: NSHomeDirectory(), isDirectory: true).appendingPathComponent("Library/Application Support", isDirectory: true)
        let appDirectory = support.appendingPathComponent(bundleIdentifier, isDirectory: true)
        let deviceIdentitiesDirectory = appDirectory.appendingPathComponent("DeviceIdentities", isDirectory: true)
        let originDirectory = deviceIdentitiesDirectory.appendingPathComponent(CredentialStorageOrigin.digest(for: serverURL), isDirectory: true)
        supportDirectoryURL = support
        directoryURL = originDirectory
        deviceIdentitiesDirectoryURL = deviceIdentitiesDirectory
        appDirectoryURL = appDirectory
        fileURL = originDirectory.appendingPathComponent("identity.json", isDirectory: false)
        self.fileManager = fileManager
    }

    public func loadExisting() throws -> DeviceIdentity? {
        lock.lock()
        defer { lock.unlock() }
        try ensurePrivateDirectories()
        var info = stat()
        let result = lstat(fileURL.path, &info)
        if result != 0 {
            if errno == ENOENT { return nil }
            throw CyloError.persistence("The local device identity file could not be inspected.")
        }
        guard (info.st_mode & mode_t(S_IFMT)) == mode_t(S_IFREG), info.st_uid == getuid() else {
            throw CyloError.persistence("The local device identity path is not a private regular file.")
        }
        guard chmod(fileURL.path, mode_t(S_IRUSR | S_IWUSR)) == 0 else {
            throw CyloError.persistence("The local device identity file permissions could not be secured.")
        }
        do {
            let record = try JSONDecoder().decode(DeviceIdentityRecord.self, from: Data(contentsOf: fileURL))
            return record.identity
        } catch {
            throw CyloError.persistence("The saved local device identity cannot be read.")
        }
    }

    public func loadOrCreate() throws -> DeviceIdentity {
        if let identity = try loadExisting() { return identity }
        return try storeIfAbsent(DeviceIdentityRecord.newIdentity())
    }

    /// Stores an identity only when this origin has no file. Conflicting identities are never overwritten.
    public func storeIfAbsent(_ identity: DeviceIdentity) throws -> DeviceIdentity {
        lock.lock()
        defer { lock.unlock() }
        try ensurePrivateDirectories()
        if let existing = try loadExistingWithoutLock() {
            guard existing == identity else { throw CyloError.credentialConflict }
            return existing
        }
        let data = try JSONEncoder().encode(DeviceIdentityRecord(identity))
        let temporaryURL = directoryURL.appendingPathComponent(".identity-\(UUID().uuidString).tmp", isDirectory: false)
        let descriptor = open(temporaryURL.path, O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW, mode_t(S_IRUSR | S_IWUSR))
        guard descriptor >= 0 else {
            throw CyloError.persistence("A private temporary identity file could not be created.")
        }
        var descriptorOpen = true
        do {
            var offset = 0
            try data.withUnsafeBytes { bytes in
                guard let base = bytes.baseAddress else { return }
                while offset < bytes.count {
                    let count = write(descriptor, base.advanced(by: offset), bytes.count - offset)
                    if count < 0 {
                        if errno == EINTR { continue }
                        throw CyloError.persistence("The local device identity could not be written.")
                    }
                    offset += count
                }
            }
            guard fsync(descriptor) == 0 else {
                throw CyloError.persistence("The local device identity could not be synchronized.")
            }
            guard close(descriptor) == 0 else {
                descriptorOpen = false
                throw CyloError.persistence("The local device identity could not be closed safely.")
            }
            descriptorOpen = false
            // `link` is atomic and fails if an identity already exists; unlike rename, it cannot overwrite it.
            guard link(temporaryURL.path, fileURL.path) == 0 else {
                if errno == EEXIST, let existing = try loadExistingWithoutLock() {
                    guard existing == identity else { throw CyloError.credentialConflict }
                    try? fileManager.removeItem(at: temporaryURL)
                    return existing
                }
                throw CyloError.persistence("The local device identity could not be committed atomically.")
            }
            guard unlink(temporaryURL.path) == 0 else {
                throw CyloError.persistence("The local device identity temporary file could not be removed.")
            }
            guard fsyncDirectory() else {
                throw CyloError.persistence("The local device identity directory could not be synchronized.")
            }
            return identity
        } catch {
            if descriptorOpen { _ = close(descriptor) }
            try? fileManager.removeItem(at: temporaryURL)
            throw error
        }
    }

    public func bearerToken() throws -> String {
        guard let identity = try loadExisting() else { throw CyloError.missingCredential }
        return identity.deviceToken
    }

    private func loadExistingWithoutLock() throws -> DeviceIdentity? {
        var info = stat()
        let result = lstat(fileURL.path, &info)
        if result != 0 {
            if errno == ENOENT { return nil }
            throw CyloError.persistence("The local device identity file could not be inspected.")
        }
        guard (info.st_mode & mode_t(S_IFMT)) == mode_t(S_IFREG), info.st_uid == getuid() else {
            throw CyloError.persistence("The local device identity path is not a private regular file.")
        }
        guard chmod(fileURL.path, mode_t(S_IRUSR | S_IWUSR)) == 0 else {
            throw CyloError.persistence("The local device identity file permissions could not be secured.")
        }
        do { return try JSONDecoder().decode(DeviceIdentityRecord.self, from: Data(contentsOf: fileURL)).identity }
        catch { throw CyloError.persistence("The saved local device identity cannot be read.") }
    }

    private func ensurePrivateDirectories() throws {
        if !fileManager.fileExists(atPath: supportDirectoryURL.path) {
            do {
                try fileManager.createDirectory(at: supportDirectoryURL, withIntermediateDirectories: true)
            } catch {
                throw CyloError.persistence("The user Application Support folder could not be created.")
            }
        }
        var supportInfo = stat()
        guard lstat(supportDirectoryURL.path, &supportInfo) == 0,
              (supportInfo.st_mode & mode_t(S_IFMT)) == mode_t(S_IFDIR),
              supportInfo.st_uid == getuid() else {
            throw CyloError.persistence("The user Application Support path is not a directory owned by this account.")
        }
        let components = [appDirectoryURL, deviceIdentitiesDirectoryURL, directoryURL]
        for directory in components {
            if !fileManager.fileExists(atPath: directory.path) {
                do {
                    try fileManager.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
                } catch {
                    // Validate the path below in case another operation created it concurrently.
                }
            }
            var info = stat()
            guard lstat(directory.path, &info) == 0,
                  (info.st_mode & mode_t(S_IFMT)) == mode_t(S_IFDIR),
                  info.st_uid == getuid(),
                  chmod(directory.path, mode_t(S_IRWXU)) == 0 else {
                throw CyloError.persistence("The local device identity folder is not a private directory.")
            }
        }
    }

    private func fsyncDirectory() -> Bool {
        let descriptor = open(directoryURL.path, O_RDONLY | O_DIRECTORY | O_NOFOLLOW)
        guard descriptor >= 0 else { return false }
        defer { _ = close(descriptor) }
        return fsync(descriptor) == 0
    }
}

private enum CredentialStorageOrigin {
    static func digest(for serverURL: URL) -> String {
        let scheme = (serverURL.scheme ?? "https").lowercased()
        let host = (serverURL.host ?? "server").lowercased()
        let port = serverURL.port ?? 443
        let origin = "\(scheme)://\(host):\(port)\(serverURL.path)"
        return SHA256.hash(data: Data(origin.utf8)).map { String(format: "%02x", $0) }.joined()
    }
}
