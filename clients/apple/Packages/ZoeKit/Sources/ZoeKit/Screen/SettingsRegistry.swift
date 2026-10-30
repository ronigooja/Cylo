import Foundation

public enum SettingActor: Equatable, Sendable { case user, ai }

public protocol SettingsStore {
    func load() -> [String: JSONValue]
    func save(_ values: [String: JSONValue])
    func loadAudit() -> [SettingAuditRecord]
    func saveAudit(_ audit: [SettingAuditRecord])
}

public final class InMemorySettingsStore: SettingsStore {
    public private(set) var values: [String: JSONValue]
    public private(set) var audit: [SettingAuditRecord]
    public var savedAudit: [SettingAuditRecord] { audit }

    public init(values: [String: JSONValue] = [:], audit: [SettingAuditRecord] = []) {
        self.values = values
        self.audit = audit
    }

    public func load() -> [String: JSONValue] { values }
    public func save(_ values: [String: JSONValue]) { self.values = values }
    public func loadAudit() -> [SettingAuditRecord] { audit }
    public func saveAudit(_ audit: [SettingAuditRecord]) { self.audit = audit }
}

@MainActor public final class SettingsRegistry {
    private let store: SettingsStore
    private var registered: [String: SettingDescriptor] = [:]
    private var values: [String: JSONValue]

    public private(set) var audit: [SettingAuditRecord]
    public var onAIChange: ((SettingAuditRecord) -> Void)?

    public init(store: SettingsStore = InMemorySettingsStore()) {
        self.store = store
        values = store.load()
        audit = store.loadAudit()
    }

    public func register(_ descriptor: SettingDescriptor, defaultValue: JSONValue) {
        registered[descriptor.key] = descriptor
        if values[descriptor.key] == nil {
            values[descriptor.key] = defaultValue
            store.save(values)
        }
    }

    public func descriptors() -> [SettingDescriptor] {
        registered.values.sorted { $0.key < $1.key }
    }

    public func value(for key: String) -> JSONValue? {
        guard registered[key] != nil else { return nil }
        return values[key]
    }

    @discardableResult
    public func write(key: String, value: JSONValue, by actor: SettingActor) throws -> SettingAuditRecord? {
        guard let descriptor = registered[key] else { throw SettingWriteError.unknownKey }
        if actor == .ai && (descriptor.userOnlyReason != nil || !descriptor.aiMutable) { throw SettingWriteError.userOnly }
        guard valid(value, for: descriptor) else { throw SettingWriteError.invalidValue }

        let oldValue = values[key] ?? .null
        values[key] = value
        store.save(values)
        guard actor == .ai else { return nil }

        let record = SettingAuditRecord(id: UUID().uuidString, key: key, oldValue: oldValue,
                                        newValue: value, at: Date())
        audit.append(record)
        store.saveAudit(audit)
        onAIChange?(record)
        return record
    }

    @discardableResult
    public func undo(recordID: String) -> Bool {
        guard let index = audit.firstIndex(where: { $0.id == recordID && !$0.undone }) else { return false }
        let record = audit[index]
        values[record.key] = record.oldValue
        audit[index].undone = true
        store.save(values)
        store.saveAudit(audit)
        return true
    }

    public func registerDefaults() {
        register(SettingDescriptor(key: "voice.enabled", type: "bool", summary: "Enable continuous voice", aiMutable: true), defaultValue: .bool(true))
        register(SettingDescriptor(key: "voice.silenceTimeoutSeconds", type: "number", range: "5...3600", summary: "Voice silence timeout", aiMutable: true), defaultValue: .number(60))
        register(SettingDescriptor(key: "textInput.visible", type: "bool", summary: "Show text input", aiMutable: true), defaultValue: .bool(true))
        register(SettingDescriptor(key: "presence.showInDock", type: "bool", summary: "Show in Dock", aiMutable: true), defaultValue: .bool(true))
        register(SettingDescriptor(key: "presence.floating", type: "bool", summary: "Use a floating presence surface", aiMutable: true), defaultValue: .bool(false))
        register(SettingDescriptor(key: "presence.opacity", type: "number", range: "0.2...1", summary: "Presence opacity", aiMutable: true), defaultValue: .number(1))
        register(SettingDescriptor(key: "theme.accent", type: "string", summary: "Theme accent", aiMutable: true), defaultValue: .string("default"))
        register(SettingDescriptor(key: "theme.density", type: "string", summary: "Theme density", aiMutable: true), defaultValue: .string("comfortable"))
        register(SettingDescriptor(key: "connection.instanceKey", type: "string", summary: "Connected instance key", aiMutable: false, userOnlyReason: "Credentials are user-controlled"), defaultValue: .string(""))
        register(SettingDescriptor(key: "os.microphonePermission", type: "bool", summary: "Microphone permission", aiMutable: false, userOnlyReason: "OS permission is user-controlled"), defaultValue: .bool(false))
        register(SettingDescriptor(key: "ai.settingsAccess", type: "bool", summary: "Allow AI setting changes", aiMutable: false, userOnlyReason: "Only the user can revoke AI settings access"), defaultValue: .bool(true))
    }

    private func valid(_ value: JSONValue, for descriptor: SettingDescriptor) -> Bool {
        switch descriptor.type {
        case "bool": if case .bool = value { return true }
        case "string": if case .string = value { return true }
        case "number":
            guard let number = value.screenNumber, let range = descriptor.range else { return false }
            let parts = range.components(separatedBy: "...").compactMap { Double($0) }
            if parts.count == 2 { return number >= parts[0] && number <= parts[1] }
            return false
        default: return true
        }
        return false
    }
}
