import CyloUI
import SwiftUI

@main
struct CyloMacApp: App {
    @StateObject private var model = CyloAppModel()
    @NSApplicationDelegateAdaptor(CyloPushNotificationAppDelegate.self) private var pushDelegate

    var body: some Scene {
        WindowGroup {
            CyloRootView(model: model)
                .frame(minWidth: 840, minHeight: 620)
        }
        .defaultSize(width: 1280, height: 820)
    }
}
