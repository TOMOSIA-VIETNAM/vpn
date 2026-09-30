import Cocoa
import Combine
import Network
import SwiftUI
import UserNotifications

// MARK: - System Notifications

/// Posts connection drops and recoveries to Notification Center, so they are seen while the
/// popover is closed.
enum ConnectionNotifier {
    /// UNUserNotificationCenter needs an app bundle; a bare swiftc build has none and would crash.
    static var available: Bool { Bundle.main.bundleIdentifier != nil }

    static func requestAuthorization(_ done: (@MainActor () -> Void)? = nil) {
        guard available else { return }
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { _, _ in
            DispatchQueue.main.async { MainActor.assumeIsolated { done?() } }
        }
    }

    private static let enabledKey = "notificationsEnabled"

    /// The user's own switch, on top of the system permission; on by default.
    static var enabled: Bool {
        get { UserDefaults.standard.object(forKey: enabledKey) as? Bool ?? true }
        set { UserDefaults.standard.set(newValue, forKey: enabledKey) }
    }

    enum Permission { case allowed, notAsked, denied }

    static func permission(_ done: @escaping @MainActor (Permission) -> Void) {
        guard available else { return }
        UNUserNotificationCenter.current().getNotificationSettings { settings in
            let value: Permission
            switch settings.authorizationStatus {
            case .notDetermined: value = .notAsked
            case .denied: value = .denied
            default: value = .allowed
            }
            DispatchQueue.main.async { MainActor.assumeIsolated { done(value) } }
        }
    }

    /// This app's page in System Settings → Notifications.
    static func openSystemSettings() {
        let id = Bundle.main.bundleIdentifier ?? ""
        let pane = URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension?id=\(id)")
        let legacy = URL(string: "x-apple.systempreferences:com.apple.preference.notifications")
        if let pane, NSWorkspace.shared.open(pane) { return }
        if let legacy { NSWorkspace.shared.open(legacy) }
    }

    /// Reuses one identifier, so a newer status replaces the previous banner instead of stacking.
    static func post(title: String, body: String) {
        guard available, enabled else { return }
        let content = UNMutableNotificationContent()
        content.title = title
        content.body = body
        content.sound = .default
        let request = UNNotificationRequest(identifier: "connection-status", content: content, trigger: nil)
        UNUserNotificationCenter.current().add(request)
    }
}

/// What the update window shows; the window stays open through download and failure.
final class DialogModel: ObservableObject {
    @Published var title: String
    @Published var text: String
    @Published var note: String?
    @Published var link: (label: String, url: URL)?
    @Published var buttons: [String]
    @Published var busy: String?
    var choose: (Int) -> Void = { _ in }

    init(title: String, text: String, note: String? = nil, link: (label: String, url: URL)? = nil, buttons: [String]) {
        self.title = title
        self.text = text
        self.note = note
        self.link = link
        self.buttons = buttons
    }
}

struct UpdateDialog: View {
    @ObservedObject var model: DialogModel

    var body: some View {
        VStack(spacing: 12) {
            Image(nsImage: NSApp.applicationIconImage)
                .resizable()
                .frame(width: 64, height: 64)
            Text(model.title)
                .font(.system(size: 13, weight: .bold))
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            Text(model.text)
                .font(.system(size: 11))
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
            if let note = model.note {
                Text(note)
                    .font(.system(size: 11))
                    .foregroundColor(.secondary)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let link = model.link {
                Button(link.label) { NSWorkspace.shared.open(link.url) }
                    .buttonStyle(.link)
                    .font(.system(size: 11))
            }
            if let busy = model.busy {
                HStack(spacing: 8) {
                    ProgressView().controlSize(.small)
                    Text(busy).font(.system(size: 11)).foregroundColor(.secondary)
                }
                .padding(.top, 6)
            } else {
                VStack(spacing: 8) {
                    ForEach(Array(model.buttons.enumerated()), id: \.offset) { index, label in
                        Button(action: { model.choose(index) }) {
                            Text(label).frame(maxWidth: .infinity)
                        }
                        .controlSize(.large)
                        .modifier(DialogButtonStyle(primary: index == 0))
                    }
                    if model.buttons.count == 1 {
                        // A lone OK answers Esc too.
                        Button("") { model.choose(0) }
                            .keyboardShortcut(.cancelAction)
                            .frame(width: 0, height: 0)
                            .opacity(0)
                    }
                }
                .padding(.top, 6)
            }
        }
        .padding(.horizontal, 20)
        .padding(.bottom, 20)
        .padding(.top, 8)
        .frame(width: 300)
    }
}

private struct DialogButtonStyle: ViewModifier {
    let primary: Bool
    func body(content: Content) -> some View {
        if primary {
            content.buttonStyle(.borderedProminent).keyboardShortcut(.defaultAction)
        } else {
            content.buttonStyle(.bordered).keyboardShortcut(.cancelAction)
        }
    }
}
