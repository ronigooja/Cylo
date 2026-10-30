import ZoeUI
import SwiftUI

@main
struct ZoeMacApp: App {
    @StateObject private var model = ZoeAppModel()
    @NSApplicationDelegateAdaptor(ZoePushNotificationAppDelegate.self) private var pushDelegate

    var body: some Scene {
        WindowGroup {
            Color.clear
                .onOpenURL { model.receiveInstallationLink($0) }
                .task { await model.start() }
                .frame(minWidth: 840, minHeight: 620)
        }
        .defaultSize(width: 1280, height: 820)
    }
}
