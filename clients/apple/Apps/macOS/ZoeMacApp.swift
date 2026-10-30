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
                .frame(minWidth: 840, minHeight: 620)
        }
        .defaultSize(width: 1280, height: 820)
        Settings { MacSettingsView(model: model, coordinator: coordinator) }
        CommandMenu("Demo") {
            Button("Canvas / Layer") { coordinator.surfaces.open(SurfaceSpec(id: "demo.canvas", kind: .window, document: DemoDocuments.canvas)) }
            ForEach(ComponentCatalog.descriptors, id: \.type) { descriptor in Button(descriptor.type) { coordinator.surfaces.open(SurfaceSpec(id: "demo.\(descriptor.type)", kind: .window, document: DemoDocuments.standard(descriptor.type))) } }
        }
    }
}
