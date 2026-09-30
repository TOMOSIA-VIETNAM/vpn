import ServiceManagement

/// "Open at login" for the menu bar app, through the system's login-item service
/// (macOS 13 and later; on 12 the setting is simply not offered).
enum LoginItem {
    static var isSupported: Bool {
        if #available(macOS 13.0, *) { return true }
        return false
    }

    static var isEnabled: Bool {
        if #available(macOS 13.0, *) { return SMAppService.mainApp.status == .enabled }
        return false
    }

    /// Returns nil on success, else a short reason (for example when the app is not in Applications).
    static func set(_ on: Bool) -> String? {
        guard #available(macOS 13.0, *) else { return "requires macOS 13 or later" }
        do {
            if on { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
            return nil
        } catch {
            return error.localizedDescription
        }
    }
}
