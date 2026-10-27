import ZoeUI
import SwiftUI

@main
struct ZoeMobileApp: App {
    @StateObject private var model = ZoeAppModel()
    @UIApplicationDelegateAdaptor(ZoePushNotificationAppDelegate.self) private var pushDelegate

    var body: some Scene {
        WindowGroup {
            ZoeRootView(model: model)
                .onOpenURL { model.receiveInstallationLink($0) }
        }
    }
}
