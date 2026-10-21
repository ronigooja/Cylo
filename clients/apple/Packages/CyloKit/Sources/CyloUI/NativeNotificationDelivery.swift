import CyloKit
import Foundation
import UserNotifications

/// Best-effort local wake/alert for newly observed durable inbox items. The inbox remains
/// authoritative; this does not implement APNs transport or remote notification registration.
public final class NativeNotificationDelivery: @unchecked Sendable {
    public static let shared = NativeNotificationDelivery()
    private let center = UNUserNotificationCenter.current()

    private init() {}

    public func requestPermission() async throws -> Bool {
        try await center.requestAuthorization(options: [.alert, .badge, .sound])
    }

    public func deliver(_ item: NotificationRecord) async {
        let settings = await center.notificationSettings()
        guard settings.authorizationStatus == .authorized || settings.authorizationStatus == .provisional else { return }
        let content = UNMutableNotificationContent()
        content.title = item.title
        content.body = item.body
        content.sound = .default
        content.userInfo = [
            "notification_id": item.id,
            "aggregate_type": item.aggregateType,
            "aggregate_id": item.aggregateId
        ]
        let request = UNNotificationRequest(identifier: item.id, content: content, trigger: nil)
        try? await center.add(request)
    }
}
