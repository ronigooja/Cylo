import Foundation

func uiText(_ key: String) -> String { NSLocalizedString(key, bundle: .module, comment: "") }

func uiFormat(_ key: String, _ arguments: CVarArg...) -> String {
    String(format: uiText(key), locale: Locale.current, arguments: arguments)
}
