import Foundation

/// Opaque token that prevents asynchronous responses from an earlier server connection
/// from updating the currently selected server's client state.
public struct ConnectionGeneration: Equatable, Sendable {
    public private(set) var value: UInt64
    public private(set) var origin: String?

    public init(origin: URL? = nil) {
        self.value = 0
        self.origin = origin.map(Self.canonicalOrigin)
    }

    @discardableResult
    public mutating func switchOrigin(to origin: URL) -> UInt64 {
        self.value &+= 1
        self.origin = Self.canonicalOrigin(origin)
        return value
    }

    @discardableResult
    public mutating func advance() -> UInt64 {
        value &+= 1
        return value
    }

    public func accepts(_ token: UInt64, from origin: URL) -> Bool {
        value == token && self.origin == Self.canonicalOrigin(origin)
    }

    private static func canonicalOrigin(_ url: URL) -> String {
        var components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        components?.path = ""
        components?.query = nil
        components?.fragment = nil
        return components?.string ?? url.absoluteString
    }
}
