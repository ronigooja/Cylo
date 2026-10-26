import CyloUI
import SwiftUI

@main
struct CyloMacApp: App {
    @StateObject private var model = CyloAppModel()
    @NSApplicationDelegateAdaptor(CyloPushNotificationAppDelegate.self) private var pushDelegate

    var body: some Scene {
        WindowGroup {
            CyloRootView(model: model)
                .onOpenURL { model.receiveInstallationLink($0) }
                .frame(minWidth: 840, minHeight: 620)
        }
        .defaultSize(width: 1280, height: 820)

        Settings {
            CyloSettingsView(model: model)
                .frame(width: 640, height: 680)
        }
    }
}
