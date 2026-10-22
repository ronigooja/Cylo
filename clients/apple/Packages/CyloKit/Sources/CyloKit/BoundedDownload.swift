import Foundation

struct BoundedDownload {
    let fileURL: URL
    let response: HTTPURLResponse
}

final class BoundedDownloadDelegate: NSObject, URLSessionDownloadDelegate, @unchecked Sendable {
    private let maximumBytes: Int64
    private let maximumErrorBytes: Int64 = 1 << 20
    private let destination: URL
    private let lock = NSLock()
    private var continuation: CheckedContinuation<BoundedDownload, Error>?
    private var task: URLSessionDownloadTask?
    private var exceededLimit = false
    private var cancelled = false
    private var completed = false

    init(destinationDirectory: URL, maximumBytes: Int64) {
        self.maximumBytes = maximumBytes
        destination = destinationDirectory.appendingPathComponent("cylo-download-" + UUID().uuidString.lowercased() + ".tmp", isDirectory: false)
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
            try FileManager.default.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true)
            try FileManager.default.moveItem(at: location, to: destination)
            try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: destination.path)
            finish(.success(BoundedDownload(fileURL: destination, response: response)))
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
        lock.unlock()
        continuation.resume(with: result)
    }
}

func downloadBounded(_ request: URLRequest, using sourceSession: URLSession, maximumBytes: Int64) async throws -> BoundedDownload {
    let delegate = BoundedDownloadDelegate(destinationDirectory: FileManager.default.temporaryDirectory, maximumBytes: maximumBytes)
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
