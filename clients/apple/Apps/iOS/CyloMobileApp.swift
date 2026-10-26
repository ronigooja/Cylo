import CyloUI
import SwiftUI

@main
struct CyloMobileApp: App {
    @StateObject private var model = CyloAppModel()
    @UIApplicationDelegateAdaptor(CyloPushNotificationAppDelegate.self) private var pushDelegate

    var body: some Scene {
        WindowGroup {
            CyloRootView(model: model)
                .onOpenURL { model.receiveInstallationLink($0) }
        }
    }
}
