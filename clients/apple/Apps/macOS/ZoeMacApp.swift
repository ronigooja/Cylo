import ZoeUI
import ZoeKit
import SwiftUI

@main
struct ZoeMacApp: App {
    @StateObject private var model: ZoeAppModel
    @StateObject private var coordinator: ScreenCoordinator
    @NSApplicationDelegateAdaptor(ZoePushNotificationAppDelegate.self) private var pushDelegate

    init() {
        let model = ZoeAppModel()
        _model = StateObject(wrappedValue: model)
        _coordinator = StateObject(wrappedValue: ScreenCoordinator(model: model))
    }

    var body: some Scene {
        WindowGroup {
            MacSurfaceRoot(model: model, coordinator: coordinator)
                .onOpenURL { model.receiveInstallationLink($0) }
                .task { await model.start() }
        }
        .windowResizability(.contentSize)
        .defaultSize(width: 460, height: 380)
        .commands {
            CommandMenu("Demo") {
                Button("All components (one window)") { coordinator.surfaces.open(SurfaceSpec(id: "demo.gallery", kind: .window, document: DemoDocuments.gallery)) }
                Divider()
                Button("All components (one window)") { coordinator.surfaces.open(SurfaceSpec(id: "demo.gallery", kind: .window, document: DemoDocuments.gallery)) }
                Divider()
                Button("Canvas / Layer") { coordinator.surfaces.open(SurfaceSpec(id: "demo.canvas", kind: .window, document: DemoDocuments.canvas)) }
                ForEach(ComponentCatalog.descriptors, id: \.type) { descriptor in Button(descriptor.type) { coordinator.surfaces.open(SurfaceSpec(id: "demo.\(descriptor.type)", kind: .window, document: DemoDocuments.standard(descriptor.type))) } }
            }
        }
        Settings { MacSettingsView(model: model, coordinator: coordinator) }
    }
}
