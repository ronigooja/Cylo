#if os(macOS)
import SwiftUI
import ZoeKit

public enum SurfaceDensity: String, Sendable {
    case minimal, compact, regular
}

private struct SurfaceDensityKey: EnvironmentKey {
    static let defaultValue: SurfaceDensity = .regular
}

private struct ScreenReduceMotionKey: EnvironmentKey {
    static let defaultValue = false
}

public extension EnvironmentValues {
    var surfaceDensity: SurfaceDensity {
        get { self[SurfaceDensityKey.self] }
        set { self[SurfaceDensityKey.self] = newValue }
    }

    var screenReduceMotion: Bool {
        get { self[ScreenReduceMotionKey.self] }
        set { self[ScreenReduceMotionKey.self] = newValue }
    }
}

public enum ScreenStyle {
    public static func tone(_ tone: ScreenTone) -> Color {
        switch tone {
        case .neutral: return .secondary
        case .info: return .blue
        case .attention: return .orange
        case .success: return .green
        case .critical: return .red
        }
    }

    public static func font(for emphasis: ScreenEmphasis, level: Int? = nil) -> Font {
        if let level {
            return level <= 1 ? .largeTitle.weight(.semibold) : level == 2 ? .title2.weight(.semibold) : .title3.weight(.semibold)
        }
        switch emphasis {
        case .low: return .subheadline
        case .normal: return .body
        case .high: return .headline
        }
    }

    public static func spacing(_ density: SurfaceDensity) -> CGFloat {
        switch density {
        case .minimal: return 4
        case .compact: return 8
        case .regular: return 14
        }
    }

    public static func padding(_ density: SurfaceDensity) -> CGFloat {
        switch density {
        case .minimal: return 6
        case .compact: return 10
        case .regular: return 16
        }
    }
}

struct MaterialView: NSViewRepresentable {
    let material: String
    let opacity: Double

    func makeNSView(context: Context) -> NSVisualEffectView {
        let view = NSVisualEffectView()
        view.blendingMode = .behindWindow
        view.state = .active
        return view
    }

    func updateNSView(_ view: NSVisualEffectView, context: Context) {
        switch material.lowercased() {
        case "sidebar": view.material = .sidebar
        case "menu": view.material = .menu
        case "popover": view.material = .popover
        case "hud": view.material = .hudWindow
        case "content": view.material = .contentBackground
        default: view.material = .underWindowBackground
        }
        view.alphaValue = max(0, min(1, opacity))
    }
}
#endif
