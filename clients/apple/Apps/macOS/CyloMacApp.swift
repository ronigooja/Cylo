import CyloUI
import SwiftUI

@main
struct CyloMacApp: App {
    @StateObject private var model = CyloAppModel()
    @NSApplicationDelegateAdaptor(CyloPushNotificationAppDelegate.self) private var pushDelegate

    var body: some Scene {
        WindowGroup {
            CyloRootView(model: model)
                .frame(minWidth: 900, minHeight: 660)
        }
        .defaultSize(width: 1180, height: 800)
    }
}
