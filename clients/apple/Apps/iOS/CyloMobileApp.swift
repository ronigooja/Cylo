import CyloUI
import SwiftUI

@main
struct CyloMobileApp: App {
    @StateObject private var model = CyloAppModel()

    var body: some Scene {
        WindowGroup {
            CyloRootView(model: model)
        }
    }
}
