#if os(iOS)
import AVFoundation
import CyloKit
import SwiftUI
import UIKit

/// Camera frames stay in the capture pipeline; only decoded QR text reaches the connection parser.
struct InstallationCameraScanner: View {
    @Environment(\.dismiss) private var dismiss
    let completion: (Result<InstallationConnection, Error>) -> Void

    var body: some View {
        NavigationStack {
            CameraCaptureView(completion: completion)
                .ignoresSafeArea(edges: .bottom)
                .navigationTitle(uiText("Scan QR code"))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button(uiText("Cancel")) { dismiss() }
                    }
                }
        }
    }
}

private enum CameraScanError: LocalizedError {
    case denied, unavailable
    var errorDescription: String? {
        switch self {
        case .denied: "Camera access is disabled. Allow access in Settings or import a QR image."
        case .unavailable: "The camera is unavailable. Import a QR image instead."
        }
    }
}

private struct CameraCaptureView: UIViewControllerRepresentable {
    let completion: (Result<InstallationConnection, Error>) -> Void
    func makeUIViewController(context: Context) -> CameraCaptureController {
        CameraCaptureController(completion: completion)
    }
    func updateUIViewController(_ controller: CameraCaptureController, context: Context) {}
    static func dismantleUIViewController(_ controller: CameraCaptureController, coordinator: ()) {
        controller.stop()
    }
}

private final class CameraCaptureController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    private let session = AVCaptureSession()
    private let captureQueue = DispatchQueue(label: "com.cylo.installation-camera")
    private let completion: (Result<InstallationConnection, Error>) -> Void
    private var preview: AVCaptureVideoPreviewLayer?
    private var finished = false // Main queue only.
    private var active = false // Capture queue only.
    private var configured = false // Capture queue only.

    init(completion: @escaping (Result<InstallationConnection, Error>) -> Void) {
        self.completion = completion
        super.init(nibName: nil, bundle: nil)
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.videoGravity = .resizeAspectFill
        view.layer.addSublayer(preview)
        self.preview = preview
        view.accessibilityLabel = uiText("Scan QR code")
    }
    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        preview?.frame = view.bounds
        // Follow device rotation without recording images or requesting microphone access.
        if let angle = view.window?.windowScene?.interfaceOrientation, let connection = preview?.connection {
            let degrees: CGFloat
            switch angle {
            case .landscapeLeft: degrees = 0
            case .landscapeRight: degrees = 180
            case .portraitUpsideDown: degrees = 270
            default: degrees = 90
            }
            if connection.isVideoRotationAngleSupported(degrees) { connection.videoRotationAngle = degrees }
        }
    }
    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        captureQueue.async { self.active = true }
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized: start()
        case .notDetermined:
            AVCaptureDevice.requestAccess(for: .video) { allowed in
                if allowed { self.start() }
                else { self.fail(CameraScanError.denied) }
            }
        default: fail(CameraScanError.denied)
        }
    }
    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        finished = true
        stop()
    }
    func stop() {
        captureQueue.async {
            self.active = false
            if self.session.isRunning { self.session.stopRunning() }
        }
    }
    private func start() {
        captureQueue.async {
            guard self.active else { return }
            do {
                if !self.configured {
                    guard let camera = AVCaptureDevice.default(for: .video) else { throw CameraScanError.unavailable }
                    let input = try AVCaptureDeviceInput(device: camera)
                    let output = AVCaptureMetadataOutput()
                    self.session.beginConfiguration()
                    defer { self.session.commitConfiguration() }
                    guard self.session.canAddInput(input) else { throw CameraScanError.unavailable }
                    self.session.addInput(input)
                    guard self.session.canAddOutput(output) else { throw CameraScanError.unavailable }
                    self.session.addOutput(output)
                    guard output.availableMetadataObjectTypes.contains(.qr) else { throw CameraScanError.unavailable }
                    output.setMetadataObjectsDelegate(self, queue: .main)
                    output.metadataObjectTypes = [.qr]
                    self.configured = true
                    DispatchQueue.main.async { self.view.setNeedsLayout() }
                }
                self.session.startRunning()
            } catch { self.fail(CameraScanError.unavailable) }
        }
    }
    private func fail(_ error: Error) {
        DispatchQueue.main.async { self.finish(.failure(error)) }
    }
    private func finish(_ result: Result<InstallationConnection, Error>) {
        guard !finished else { return }
        finished = true
        stop()
        completion(result)
    }
    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
        let payloads = objects.compactMap { ($0 as? AVMetadataMachineReadableCodeObject)?.stringValue }
        guard !payloads.isEmpty else { return }
        finish(Result { try InstallationConnection.decodeQRPayloads(payloads) })
    }
}
#endif
