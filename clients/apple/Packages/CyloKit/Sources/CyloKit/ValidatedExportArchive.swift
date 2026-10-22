import CryptoKit
import Foundation

struct ValidatedExportArchive {
    let manifest: ExportManifest

    init(fileURL: URL, archiveSHA256: String?, manifestSHA256: String?, decoder: JSONDecoder) throws {
        let attributes = try FileManager.default.attributesOfItem(atPath: fileURL.path)
        guard let fileSize = (attributes[.size] as? NSNumber)?.uint64Value,
              fileSize >= 22, fileSize <= Self.maximumBytes else { throw CyloError.invalidResponse }
        let file = try FileHandle(forReadingFrom: fileURL)
        defer { try? file.close() }

        guard try Self.hex(Self.digest(file, offset: 0, count: fileSize)) == archiveSHA256?.lowercased() else {
            throw CyloError.invalidResponse
        }
        let (manifestOffset, manifestLength, dataOffset, dataLength, manifestCRC, dataCRC) = try Self.entries(file, size: fileSize)
        guard manifestLength <= Self.maximumManifestBytes else { throw CyloError.invalidResponse }
        let manifestBytes = try Self.read(file, offset: manifestOffset, count: manifestLength)
        guard Self.hex(SHA256.hash(data: manifestBytes)) == manifestSHA256?.lowercased(),
              try Self.crc32(file, offset: manifestOffset, count: manifestLength) == manifestCRC,
              try Self.crc32(file, offset: dataOffset, count: dataLength) == dataCRC else {
            throw CyloError.invalidResponse
        }
        let parsed = try decoder.decode(ExportManifest.self, from: manifestBytes)
        guard parsed.integrity.dataBytes >= 0, UInt64(parsed.integrity.dataBytes) == dataLength,
              Self.hex(try Self.digest(file, offset: dataOffset, count: dataLength)) == parsed.integrity.dataSha256.lowercased() else {
            throw CyloError.invalidResponse
        }
        manifest = parsed
    }

    private static let maximumBytes: UInt64 = 1 << 30
    private static let maximumManifestBytes: UInt64 = 1 << 20
    private static let chunkSize = 1024 * 1024
    private static let crcTable: [UInt32] = (0..<256).map { value in
        var entry = UInt32(value)
        for _ in 0..<8 { entry = entry & 1 == 1 ? (entry >> 1) ^ 0xedb8_8320 : entry >> 1 }
        return entry
    }

    private static func entries(_ file: FileHandle, size: UInt64) throws -> (UInt64, UInt64, UInt64, UInt64, UInt32, UInt32) {
        let tailLength = min(size, 65_557)
        let tailOffset = size - tailLength
        let tail = try read(file, offset: tailOffset, count: tailLength)
        guard tail.count >= 22 else { throw CyloError.invalidResponse }
        var endIndex: Int?
        for index in stride(from: tail.count - 22, through: 0, by: -1) {
            if try u32(tail, index) == 0x06054b50,
               index + 22 + Int(try u16(tail, index + 20)) == tail.count {
                endIndex = index
                break
            }
        }
        guard let end = endIndex,
              try u16(tail, end + 4) == 0, try u16(tail, end + 6) == 0,
              try u16(tail, end + 8) == 2, try u16(tail, end + 10) == 2 else { throw CyloError.invalidResponse }
        let directorySize = UInt64(try u32(tail, end + 12))
        let directoryOffset = UInt64(try u32(tail, end + 16))
        let endOffset = tailOffset + UInt64(end)
        guard directoryOffset <= endOffset, directorySize == endOffset - directoryOffset else { throw CyloError.invalidResponse }

        var cursor = directoryOffset
        var localEnd: UInt64 = 0
        var results: [(UInt64, UInt64, UInt32)] = []
        for name in ["manifest.json", "data.json"] {
            let header = try read(file, offset: cursor, count: 46)
            let flags = try u16(header, 8)
            guard try u32(header, 0) == 0x02014b50,
                  flags == 0 || flags == 0x0008,
                  try u16(header, 10) == 0 else { throw CyloError.invalidResponse }
            let crc = try u32(header, 16)
            let compressed = UInt64(try u32(header, 20))
            let uncompressed = UInt64(try u32(header, 24))
            let nameLength = UInt64(try u16(header, 28))
            let extraLength = UInt64(try u16(header, 30))
            let commentLength = UInt64(try u16(header, 32))
            let disk = try u16(header, 34)
            let localOffset = UInt64(try u32(header, 42))
            let next = cursor + 46 + nameLength + extraLength + commentLength
            guard disk == 0, compressed == uncompressed, next <= endOffset,
                  String(data: try read(file, offset: cursor + 46, count: nameLength), encoding: .utf8) == name,
                  localOffset == localEnd else { throw CyloError.invalidResponse }

            let local = try read(file, offset: localOffset, count: 30)
            let localFlags = try u16(local, 6)
            guard try u32(local, 0) == 0x04034b50,
                  localFlags == flags, localFlags == 0 || localFlags == 0x0008,
                  try u16(local, 8) == 0 else { throw CyloError.invalidResponse }
            if flags == 0 {
                guard try u32(local, 14) == crc,
                      UInt64(try u32(local, 18)) == compressed,
                      UInt64(try u32(local, 22)) == uncompressed else { throw CyloError.invalidResponse }
            } else {
                let localCRC = try u32(local, 14)
                let localCompressed = try u32(local, 18)
                let localUncompressed = try u32(local, 22)
                guard (localCRC == 0 || localCRC == crc),
                      (localCompressed == 0 || UInt64(localCompressed) == compressed),
                      (localUncompressed == 0 || UInt64(localUncompressed) == uncompressed) else { throw CyloError.invalidResponse }
            }
            let localNameLength = UInt64(try u16(local, 26))
            let localExtraLength = UInt64(try u16(local, 28))
            let payloadOffset = localOffset + 30 + localNameLength + localExtraLength
            guard try read(file, offset: localOffset + 30, count: localNameLength) == Data(name.utf8),
                  payloadOffset <= directoryOffset, compressed <= directoryOffset - payloadOffset else { throw CyloError.invalidResponse }
            var payloadEnd = payloadOffset + compressed
            if flags == 0x0008 {
                let remaining = directoryOffset - payloadEnd
                guard remaining >= 12 else { throw CyloError.invalidResponse }
                let first = try read(file, offset: payloadEnd, count: 4)
                if try u32(first, 0) == 0x08074b50 {
                    let descriptor = try read(file, offset: payloadEnd + 4, count: 12)
                    guard try u32(descriptor, 0) == crc,
                          UInt64(try u32(descriptor, 4)) == compressed,
                          UInt64(try u32(descriptor, 8)) == uncompressed else { throw CyloError.invalidResponse }
                    payloadEnd += 16
                } else {
                    let descriptor = try read(file, offset: payloadEnd, count: 12)
                    guard try u32(descriptor, 0) == crc,
                          UInt64(try u32(descriptor, 4)) == compressed,
                          UInt64(try u32(descriptor, 8)) == uncompressed else { throw CyloError.invalidResponse }
                    payloadEnd += 12
                }
                guard payloadEnd <= directoryOffset else { throw CyloError.invalidResponse }
            }
            results.append((payloadOffset, compressed, crc))
            localEnd = payloadEnd
            cursor = next
        }
        guard cursor == endOffset, localEnd == directoryOffset else { throw CyloError.invalidResponse }
        return (results[0].0, results[0].1, results[1].0, results[1].1, results[0].2, results[1].2)
    }

    private static func digest(_ file: FileHandle, offset: UInt64, count: UInt64) throws -> [UInt8] {
        try file.seek(toOffset: offset)
        var remaining = count
        var hasher = SHA256()
        while remaining > 0 {
            let next = Int(min(remaining, UInt64(chunkSize)))
            guard let bytes = try file.read(upToCount: next), bytes.count == next else { throw CyloError.invalidResponse }
            hasher.update(data: bytes)
            remaining -= UInt64(next)
        }
        return Array(hasher.finalize())
    }

    private static func crc32(_ file: FileHandle, offset: UInt64, count: UInt64) throws -> UInt32 {
        try file.seek(toOffset: offset)
        var remaining = count
        var crc: UInt32 = 0xffff_ffff
        while remaining > 0 {
            let next = Int(min(remaining, UInt64(chunkSize)))
            guard let bytes = try file.read(upToCount: next), bytes.count == next else { throw CyloError.invalidResponse }
            for byte in bytes {
                let index = Int((crc ^ UInt32(byte)) & 0xff)
                crc = Self.crcTable[index] ^ (crc >> 8)
            }
            remaining -= UInt64(next)
        }
        return ~crc
    }

    private static func read(_ file: FileHandle, offset: UInt64, count: UInt64) throws -> Data {
        guard count <= UInt64(Int.max) else { throw CyloError.invalidResponse }
        try file.seek(toOffset: offset)
        guard let data = try file.read(upToCount: Int(count)), data.count == Int(count) else { throw CyloError.invalidResponse }
        return data
    }

    private static func u16(_ data: Data, _ offset: Int) throws -> UInt16 {
        guard offset >= 0, offset + 2 <= data.count else { throw CyloError.invalidResponse }
        return UInt16(data[offset]) | UInt16(data[offset + 1]) << 8
    }

    private static func u32(_ data: Data, _ offset: Int) throws -> UInt32 {
        guard offset >= 0, offset + 4 <= data.count else { throw CyloError.invalidResponse }
        return UInt32(data[offset]) | UInt32(data[offset + 1]) << 8 | UInt32(data[offset + 2]) << 16 | UInt32(data[offset + 3]) << 24
    }

    private static func hex<H: Sequence>(_ bytes: H) -> String where H.Element == UInt8 {
        bytes.map { String(format: "%02x", $0) }.joined()
    }
}

struct ExportManifest: Decodable {
    let format: String
    let schemaVersion: Int
    let workspaceId: String
    let subjectId: String
    let scope: String
    let exportId: String
    let authority: ExportAuthority
    let integrity: ExportIntegrity
}

struct ExportAuthority: Decodable {
    let instanceLineageId: String
    let generation: Int64
    let deletionSequence: String
    let snapshotEventCursor: String
    let validatedThroughCursor: String
    let exportAuditEventCursor: String
    let exportAuditEventId: String
    let exportAuditEventType: String
    let source: String
}

struct ExportIntegrity: Decodable {
    let dataBytes: Int
    let dataSha256: String
}
