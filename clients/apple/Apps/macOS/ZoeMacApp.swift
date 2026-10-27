import ZoeUI
import SwiftUI

@main
struct ZoeMacApp: App {
    @StateObject private var model = ZoeAppModel()
    @NSApplicationDelegateAdaptor(ZoePushNotificationAppDelegate.self) private var pushDelegate

    var body: some Scene {
        WindowGroup {
            ZoeRootView(model: model)
                .onOpenURL { model.receiveInstallationLink($0) }
                .frame(minWidth: 840, minHeight: 620)
        }
        .defaultSize(width: 1280, height: 820)

        Settings {
            ZoeSettingsView(model: model)
                .frame(width: 640, height: 680)
        }
    }
}
