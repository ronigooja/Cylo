import ZoeUI
import SwiftUI

@main
struct ZoeMobileApp: App {
    @StateObject private var model = ZoeAppModel()
    @UIApplicationDelegateAdaptor(ZoePushNotificationAppDelegate.self) private var pushDelegate

    var body: some Scene {
        WindowGroup {
            Color.clear
                .onOpenURL { model.receiveInstallationLink($0) }
                .task { await model.start() }
        }
    }
}
