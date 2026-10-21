import CyloUI
import SwiftUI

@main
struct CyloMacApp: App {
    @StateObject private var model = CyloAppModel()

    var body: some Scene {
        WindowGroup {
            CyloRootView(model: model)
                .frame(minWidth: 840, minHeight: 620)
        }
    }
}
