import CyloKit
import Foundation
import UserNotifications
#if os(iOS)
import UIKit
#elseif os(macOS)
import AppKit
#endif

/// Best-effort local alerts for newly observed durable inbox items. The inbox remains
/// authoritative; remote pushes only wake the app to fetch current server state.
public final class NativeNotificationDelivery: @unchecked Sendable {
    public static let shared = NativeNotificationDelivery()
    private let center = UNUserNotificationCenter.current()

    private init() {}

    public func requestPermission() async throws -> Bool {
        try await center.requestAuthorization(options: [.alert, .badge, .sound])
    }

    public func registerForRemoteNotifications() {
        #if os(iOS)
        UIApplication.shared.registerForRemoteNotifications()
        #elseif os(macOS)
        NSApplication.shared.registerForRemoteNotifications()
        #endif
    }

    public func unregisterFromRemoteNotifications() {
        #if os(iOS)
        UIApplication.shared.unregisterForRemoteNotifications()
        #elseif os(macOS)
        NSApplication.shared.unregisterForRemoteNotifications()
        #endif
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

@MainActor
public final class RemotePushBridge {
    public static let shared = RemotePushBridge()

    public var onToken: ((Data) -> Void)?
    public var onWake: (() async -> Void)?
    public var onFailure: (() -> Void)?

    private init() {}

    public func didRegister(token: Data) { onToken?(token) }
    public func didWake() async { await onWake?() }
    public func didFailRegistration() { onFailure?() }
}

#if os(iOS)
@MainActor
public final class CyloPushNotificationAppDelegate: NSObject, UIApplicationDelegate {
    public func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        RemotePushBridge.shared.didRegister(token: deviceToken)
    }

    public func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        RemotePushBridge.shared.didFailRegistration()
    }

    public func application(_ application: UIApplication, didReceiveRemoteNotification userInfo: [AnyHashable: Any], fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void) {
        Task { @MainActor in
            await RemotePushBridge.shared.didWake()
            completionHandler(.newData)
        }
    }
}
#elseif os(macOS)
@MainActor
public final class CyloPushNotificationAppDelegate: NSObject, NSApplicationDelegate {
    public func application(_ application: NSApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        RemotePushBridge.shared.didRegister(token: deviceToken)
    }

    public func application(_ application: NSApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        RemotePushBridge.shared.didFailRegistration()
    }

    public func application(_ application: NSApplication, didReceiveRemoteNotification userInfo: [String: Any]) {
        Task { @MainActor in await RemotePushBridge.shared.didWake() }
    }
}
#endif
