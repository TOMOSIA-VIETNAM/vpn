import Cocoa
import Combine
import Network
import SwiftUI
import UserNotifications

// MARK: - Link State & Theme

enum LinkState: Equatable {
    case idle, connecting, connected
}

extension VPNProfileItem {
    var linkState: LinkState {
        isConnecting ? .connecting : (isConnected ? .connected : .idle)
    }
}

/// Every color and metric the views use. Colors come from the system palette, so the
/// UI follows the user's Light/Dark appearance and accent color like any native app.
enum Theme {
    static let connected = Color(nsColor: .systemGreen)
    static let connecting = Color(nsColor: .systemOrange)
    static let danger = Color(nsColor: .systemRed)
    /// The user's system accent color, not tied to whether the window is key.
    static let accent = Color(nsColor: .controlAccentColor)

    static let primaryText = Color(nsColor: .labelColor)
    static let secondaryText = Color(nsColor: .secondaryLabelColor)
    static let tertiaryText = Color(nsColor: .tertiaryLabelColor)

    /// Resting fill of a card or row, and the fill while the pointer hovers it.
    static let cardFill = Color.primary.opacity(0.05)
    static let hoverFill = Color.primary.opacity(0.09)
    static let cardRadius: CGFloat = 8
    /// Left/right padding of every popover section, so all edges line up.
    static let inset: CGFloat = 14

    static func color(for state: LinkState) -> Color {
        switch state {
        case .idle: return tertiaryText
        case .connecting: return connecting
        case .connected: return connected
        }
    }

    static func statusText(for state: LinkState, reconnecting: Bool) -> String {
        switch state {
        case .idle: return "Not Connected"
        case .connecting: return reconnecting ? "Reconnecting…" : "Connecting…"
        case .connected: return "Connected"
        }
    }
}

// MARK: - Shared Components

/// Status dot with a soft expanding halo while `pulsing`.
//
// The halo is driven by TimelineView and derives its phase from wall-clock time rather
// than `repeatForever` + `onAppear`, which restarted or froze whenever SwiftUI re-created
// the view (every state change or poll re-render).
struct StatusDot: View {
    var color: Color
    var size: CGFloat
    var pulsing: Bool = false

    var body: some View {
        ZStack {
            if pulsing {
                TimelineView(.animation(minimumInterval: 1.0 / 20.0)) { timeline in
                    let p = CGFloat((timeline.date.timeIntervalSinceReferenceDate / 1.4).truncatingRemainder(dividingBy: 1))
                    Circle()
                        .fill(color.opacity(0.4 * Double(1 - p)))
                        .frame(width: size, height: size)
                        .scaleEffect(1 + 1.4 * p)
                }
                .transition(.opacity)
            }
            Circle()
                .fill(color)
                .frame(width: size, height: size)
        }
        .animation(.easeInOut(duration: 0.3), value: pulsing)
    }
}

/// App tile shown in the popover header; falls back to a system shield when the
/// bundled logo is missing (running the binary outside the .app).
struct BrandTile: View {
    var size: CGFloat

    var body: some View {
        if let logo = AppBranding.logo {
            Image(nsImage: logo)
                .resizable()
                .interpolation(.high)
                .frame(width: size, height: size)
        } else {
            RoundedRectangle(cornerRadius: size * 0.225)
                .fill(Color.black)
                .overlay(
                    Image(systemName: "shield")
                        .font(.system(size: size * 0.5, weight: .semibold))
                        .foregroundColor(.white)
                )
                .frame(width: size, height: size)
        }
    }
}

/// Rounded background shared by the cards in the popover and the sheets.
struct CardBackground: View {
    var fill: Color = Theme.cardFill
    var stroke: Color? = nil

    var body: some View {
        RoundedRectangle(cornerRadius: Theme.cardRadius)
            .fill(fill)
            .overlay(
                RoundedRectangle(cornerRadius: Theme.cardRadius)
                    .strokeBorder(stroke ?? .clear, lineWidth: 1)
            )
    }
}

/// Small borderless icon button with a hover highlight, for toolbar-like actions.
struct IconButton: View {
    var systemName: String
    var help: String
    var dimmed: Bool = false
    var action: () -> Void
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Image(systemName: systemName)
                .font(.system(size: 12, weight: .medium))
                .foregroundColor(hovering ? Theme.primaryText : Theme.secondaryText)
                .opacity(dimmed ? 0.5 : 1)
                .frame(width: 24, height: 22)
                .background(RoundedRectangle(cornerRadius: 5).fill(hovering ? Theme.hoverFill : .clear))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .focusable(false)
        .help(help)
        .accessibilityLabel(help)
        .onHover { hovering = $0 }
    }
}

/// On/off switch for a connection. Drawn by hand because the stock switch turns gray
/// whenever its window is not key, which a popover often is not (for example right after
/// it opens, or while another app has focus), so a live connection looked switched off.
struct ConnectSwitch: View {
    var isOn: Bool
    /// Names the connection this switch controls, for VoiceOver.
    var label: String = "VPN connection"
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                Capsule()
                    .fill(isOn ? Theme.accent : Color.primary.opacity(0.15))
                Circle()
                    .fill(Color.white)
                    .shadow(color: Color.black.opacity(0.25), radius: 1, y: 0.5)
                    .padding(2)
                    .offset(x: isOn ? 7 : -7)
            }
            .frame(width: 32, height: 18)
            .contentShape(Capsule())
            .animation(.spring(response: 0.25, dampingFraction: 0.8), value: isOn)
        }
        .buttonStyle(.plain)
        .focusable(false)
        // A plain Button has no switch semantics; restore what VoiceOver read from Toggle.
        .accessibilityLabel(label)
        .accessibilityValue(isOn ? "On" : "Off")
    }
}

// MARK: - Native Helper for Menu Popups without ugly Dropdown Chevrons

struct CustomMenuButton: View {
    var profileTitle: String
    /// True while the profile's tunnel is up or coming up: Edit / Delete are shown but disabled.
    var locked: Bool
    var onEdit: () -> Void
    var onDelete: () -> Void

    var body: some View {
        IconButton(
            systemName: "ellipsis",
            help: locked ? "Turn the VPN off to edit or delete this configuration" : "Edit or delete",
            dimmed: locked,
            action: showNativeMenu
        )
    }

    private func showNativeMenu() {
        let menu = NSMenu()
        menu.autoenablesItems = false

        let helper = MenuHelper(title: profileTitle, onEdit: onEdit, onDelete: onDelete)
        let editItem = NSMenuItem(title: "Edit…", action: #selector(MenuHelper.editAction), keyEquivalent: "")
        let deleteItem = NSMenuItem(title: "Delete…", action: #selector(MenuHelper.deleteAction), keyEquivalent: "")
        editItem.target = helper
        deleteItem.target = helper
        deleteItem.attributedTitle = NSAttributedString(
            string: "Delete…",
            attributes: [.foregroundColor: locked ? NSColor.disabledControlTextColor : NSColor.systemRed]
        )
        editItem.isEnabled = !locked
        deleteItem.isEnabled = !locked

        menu.addItem(editItem)
        menu.addItem(NSMenuItem.separator())
        menu.addItem(deleteItem)
        if locked {
            menu.addItem(NSMenuItem.separator())
            let hint = NSMenuItem(title: "Disconnect to edit or delete", action: nil, keyEquivalent: "")
            hint.isEnabled = false
            menu.addItem(hint)
        }

        if let event = NSApp.currentEvent {
            // popUpContextMenu returns after the pick, but the menu (and helper) must
            // outlive the call: the action fires on the helper during it.
            withExtendedLifetime(helper) {
                NSMenu.popUpContextMenu(menu, with: event, for: NSApp.keyWindow?.contentView ?? NSView())
            }
        }
    }
}

@MainActor
final class MenuHelper: NSObject {
    let title: String
    let onEdit: () -> Void
    let onDelete: () -> Void

    init(title: String, onEdit: @escaping () -> Void, onDelete: @escaping () -> Void) {
        self.title = title
        self.onEdit = onEdit
        self.onDelete = onDelete
        super.init()
    }

    @objc func editAction() { onEdit() }
    @objc func deleteAction() { Self.confirmDelete(title: title, onDelete: onDelete) }

    /// Deleting also wipes the profile's saved secrets from Keychain, so ask first.
    /// Shared by the "…" menu and the row's right-click menu.
    static func confirmDelete(title: String, onDelete: () -> Void) {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "Delete \"\(title)\"?"
        alert.informativeText = "The server, account and saved passwords for this configuration will be removed from this Mac. This can't be undone."
        let delete = alert.addButton(withTitle: "Delete")
        delete.hasDestructiveAction = true
        alert.addButton(withTitle: "Cancel")
        if alert.runModal() == .alertFirstButtonReturn { onDelete() }
    }
}
