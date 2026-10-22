import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
#if canImport(Darwin)
import Darwin
#elseif canImport(Glibc)
import Glibc
#endif

struct BoundedDownload {
    let fileURL: URL
    let response: HTTPURLResponse
    let lease: BoundedDownloadLease
}

final class BoundedDownloadLease {
    let fileURL: URL
    private var fileLock: DownloadFileLock?

    init(fileURL: URL, fileLock: DownloadFileLock) {
        self.fileURL = fileURL
        self.fileLock = fileLock
    }

    func cleanup() {
        try? FileManager.default.removeItem(at: fileURL)
        fileLock?.release(removeFile: true)
        fileLock = nil
    }

    deinit { cleanup() }
}

final class DownloadFileLock {
    private(set) var descriptor: Int32
    let fileURL: URL

    init(fileURL: URL, create: Bool) throws {
        self.fileURL = fileURL
        let flags = create ? (O_CREAT | O_RDWR) : O_RDWR
        let fd = fileURL.path.withCString { open($0, flags, mode_t(0o600)) }
        guard fd >= 0 else { throw CyloError.persistence("The export download lock could not be opened.") }
        guard flock(fd, LOCK_EX | LOCK_NB) == 0 else {
            _ = close(fd)
            throw CyloError.persistence("The export download is already active.")
        }
        descriptor = fd
    }

    func release(removeFile: Bool) {
        guard descriptor >= 0 else { return }
        let fd = descriptor
        descriptor = -1
        _ = flock(fd, LOCK_UN)
        _ = close(fd)
        if removeFile { try? FileManager.default.removeItem(at: fileURL) }
    }

    deinit { release(removeFile: false) }
}

final class BoundedDownloadDelegate: NSObject, URLSessionDownloadDelegate, @unchecked Sendable {
    static let temporaryDirectory = FileManager.default.temporaryDirectory.appendingPathComponent("cylo-export-downloads", isDirectory: true)

    private let maximumBytes: Int64
    private let maximumErrorBytes: Int64 = 1 << 20
    private let destination: URL
    private var fileLock: DownloadFileLock?
    private let lock = NSLock()
    private var continuation: CheckedContinuation<BoundedDownload, Error>?
    private var task: URLSessionDownloadTask?
    private var exceededLimit = false
    private var cancelled = false
    private var completed = false

    init(destinationDirectory: URL, maximumBytes: Int64) throws {
        self.maximumBytes = maximumBytes
        destination = destinationDirectory.appendingPathComponent("cylo-download-" + UUID().uuidString.lowercased() + ".tmp", isDirectory: false)
        let lockURL = destinationDirectory.appendingPathComponent(destination.deletingPathExtension().lastPathComponent + ".lock", isDirectory: false)
        fileLock = try DownloadFileLock(fileURL: lockURL, create: true)
    }

    func download(request: URLRequest, using session: URLSession) async throws -> BoundedDownload {
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<BoundedDownload, Error>) in
                lock.lock()
                self.continuation = continuation
                let isCancelled = cancelled
                let task = isCancelled ? nil : session.downloadTask(with: request)
                self.task = task
                lock.unlock()
                guard let task else {
                    finish(.failure(CancellationError()))
                    return
                }
                task.resume()
            }
        } onCancel: {
            lock.lock()
            cancelled = true
            let task = self.task
            lock.unlock()
            task?.cancel()
        }
    }

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didWriteData bytesWritten: Int64, totalBytesWritten: Int64, totalBytesExpectedToWrite: Int64) {
        let statusCode = (downloadTask.response as? HTTPURLResponse)?.statusCode ?? 200
        let currentLimit = (200..<300).contains(statusCode) ? maximumBytes : maximumErrorBytes
        if totalBytesWritten > currentLimit || totalBytesExpectedToWrite > currentLimit {
            lock.lock()
            exceededLimit = true
            lock.unlock()
            downloadTask.cancel()
        }
    }

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {
        guard let response = downloadTask.response as? HTTPURLResponse else {
            finish(.failure(CyloError.invalidResponse))
            return
        }
        lock.lock()
        let alreadyExceeded = exceededLimit
        lock.unlock()
        if alreadyExceeded {
            finish(.failure(CyloError.exportTooLarge))
            return
        }
        let responseLimit = (200..<300).contains(response.statusCode) ? maximumBytes : maximumErrorBytes
        let attributes = try? FileManager.default.attributesOfItem(atPath: location.path)
        let downloadedSize = (attributes?[.size] as? NSNumber)?.int64Value ?? responseLimit + 1
        if downloadedSize > responseLimit {
            lock.lock()
            exceededLimit = true
            lock.unlock()
            finish(.failure(CyloError.exportTooLarge))
            return
        }
        do {
            try FileManager.default.moveItem(at: location, to: destination)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: destination.path)
            guard let fileLock else { throw CyloError.persistence("The export download lock was lost.") }
            self.fileLock = nil
            let lease = BoundedDownloadLease(fileURL: destination, fileLock: fileLock)
            finish(.success(BoundedDownload(fileURL: destination, response: response, lease: lease)))
        } catch {
            try? FileManager.default.removeItem(at: destination)
            finish(.failure(CyloError.persistence("The export could not be stored in a private temporary file.")))
        }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        guard let error else { return }
        lock.lock()
        let wasTooLarge = exceededLimit
        lock.unlock()
        finish(.failure(wasTooLarge ? CyloError.exportTooLarge : error))
    }

    private func finish(_ result: Result<BoundedDownload, Error>) {
        lock.lock()
        guard !completed, let continuation else {
            lock.unlock()
            if case let .success(download) = result { try? FileManager.default.removeItem(at: download.fileURL) }
            return
        }
        completed = true
        self.continuation = nil
        task = nil
        let failedLock: DownloadFileLock? = if case .failure = result { fileLock } else { nil }
        if case .failure = result { fileLock = nil }
        lock.unlock()
        if case .failure = result {
            try? FileManager.default.removeItem(at: destination)
            failedLock?.release(removeFile: true)
        }
        continuation.resume(with: result)
    }

    static func cleanupAbandonedFiles() {
        let directory = temporaryDirectory
        guard (try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])) != nil,
              let entries = try? FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil) else { return }
        try? FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.path)
        for lockURL in entries where lockURL.lastPathComponent.hasPrefix("cylo-download-") && lockURL.pathExtension == "lock" {
            guard let fileLock = try? DownloadFileLock(fileURL: lockURL, create: false) else { continue }
            let temporaryURL = lockURL.deletingPathExtension().appendingPathExtension("tmp")
            try? FileManager.default.removeItem(at: temporaryURL)
            fileLock.release(removeFile: true)
        }
    }
}

func downloadBounded(_ request: URLRequest, using sourceSession: URLSession, maximumBytes: Int64) async throws -> BoundedDownload {
    let directory = BoundedDownloadDelegate.temporaryDirectory
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
    try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: directory.path)
    let delegate = try BoundedDownloadDelegate(destinationDirectory: directory, maximumBytes: maximumBytes)
    let session = URLSession(configuration: sourceSession.configuration, delegate: delegate, delegateQueue: nil)
    do {
        let result = try await delegate.download(request: request, using: session)
        session.finishTasksAndInvalidate()
        return result
    } catch {
        session.invalidateAndCancel()
        throw error
    }
}
