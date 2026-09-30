import Cocoa
import Combine
import Network
import SwiftUI
import UserNotifications

// MARK: - Main Menu Bar Popup View

/// Per-kind look of the alert card: its icon and tint.
struct AlertStyle {
    var icon: String
    var tint: Color

    init(kind: VPNAlertKind) {
        switch kind {
        case .authFailed: (icon, tint) = ("lock.fill", Theme.danger)
        case .ikeFailed: (icon, tint) = ("network.slash", Theme.connecting)
        case .routeFailed: (icon, tint) = ("arrow.triangle.branch", Theme.connecting)
        case .processStopped: (icon, tint) = ("bolt.slash.fill", Theme.connecting)
        case .connectionLost: (icon, tint) = ("wifi.exclamationmark", Theme.connecting)
        case .sessionStale: (icon, tint) = ("clock.arrow.circlepath", Theme.connecting)
        case .generic: (icon, tint) = ("exclamationmark.triangle.fill", Theme.connecting)
        }
    }
}

/// One-line result of a profile edit / delete.
struct NoticeBanner: View {
    var notice: VPNNotice
    var onDismiss: () -> Void

    private var tint: Color {
        switch notice.level {
        case .success: return Theme.connected
        case .info: return Theme.accent
        case .error: return Theme.danger
        }
    }

    private var icon: String {
        switch notice.level {
        case .success: return "checkmark.circle.fill"
        case .info: return "info.circle.fill"
        case .error: return "exclamationmark.octagon.fill"
        }
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: icon)
                .font(.system(size: 11, weight: .semibold))
                .foregroundColor(tint)
            Text(notice.message)
                .font(.system(size: 11.5))
                .foregroundColor(Theme.primaryText)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button(action: onDismiss) {
                Image(systemName: "xmark")
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundColor(Theme.secondaryText)
            }
            .buttonStyle(.plain)
            .focusable(false)
            .help("Dismiss")
        }
        .padding(10)
        .background(CardBackground(fill: tint.opacity(0.08), stroke: tint.opacity(0.25)))
    }
}

struct ProfileCardRow: View {
    let profile: VPNProfileItem
    @ObservedObject var vpn: VPNManager
    var onEdit: () -> Void
    var onDelete: () -> Void
    @State private var hovering = false

    var body: some View {
        let state = profile.linkState
        let locked = vpn.isLocked(profile)
        HStack(spacing: 10) {
            StatusDot(color: Theme.color(for: state), size: 7, pulsing: state == .connecting).accessibilityHidden(true)

            VStack(alignment: .leading, spacing: 2) {
                Text(profile.title)
                    .font(.system(size: 13, weight: .medium))
                    .foregroundColor(Theme.primaryText)
                    .lineLimit(1)
                    .truncationMode(.tail)

                Text(subtitle(for: state))
                    .font(.system(size: 11))
                    .foregroundColor(Theme.secondaryText)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .id(state == .connecting)
                    .transition(.opacity)
            }

            Spacer(minLength: 8)

            ConnectSwitch(isOn: state != .idle, label: "VPN connection \(profile.title)") { vpn.toggleConnect(profile: profile) }
                .help(state == .idle ? "Connect" : "Disconnect")

            CustomMenuButton(profileTitle: profile.title, locked: locked, onEdit: onEdit, onDelete: onDelete)
        }
        .padding(.leading, 12)
        .padding(.trailing, 6)
        .padding(.vertical, 9)
        .background(CardBackground(fill: hovering ? Theme.hoverFill : Theme.cardFill))
        .contentShape(Rectangle())
        .onHover { hovering = $0 }
        .contextMenu {
            Button("Edit…", action: onEdit).disabled(locked)
            Divider()
            Button("Delete…") { MenuHelper.confirmDelete(title: profile.title, onDelete: onDelete) }
                .disabled(locked)
        }
        .animation(.easeInOut(duration: 0.2), value: state)
    }

    private func subtitle(for state: LinkState) -> String {
        if state == .connecting {
            if vpn.isReconnecting {
                return vpn.networkOnline ? "Reconnecting to \(profile.server)…" : "Waiting for network"
            }
            return "Connecting to \(profile.server)…"
        }
        return profile.username.isEmpty ? profile.server : "\(profile.server) · \(profile.username)"
    }
}

/// Details of the live tunnel, shown under the header while connected.
struct ConnectionDetails: View {
    var ip: String
    var showIP: Bool = true
    var warnings: [String] = []

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if showIP {
                item("Public IP", ip.isEmpty ? "Checking…" : ip)
            }
            ForEach(warnings, id: \.self) { w in
                HStack(alignment: .top, spacing: 6) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .font(.system(size: 10))
                        .foregroundColor(.orange)
                        .accessibilityHidden(true)
                    Text(w)
                        .font(.system(size: 11))
                        .foregroundColor(Theme.secondaryText)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Warning: \(w)")
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .background(CardBackground())
    }

    private func item(_ label: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .font(.system(size: 10, weight: .medium))
                .foregroundColor(Theme.secondaryText)
            Text(value)
                .font(.system(size: 12, weight: .medium).monospacedDigit())
                .foregroundColor(Theme.primaryText)
                .textSelection(.enabled)
        }
    }
}

struct AlertCard: View {
    let alert: VPNAlertInfo
    /// The primary action for this failure (Update Password / Try Again), if any.
    var primary: (title: String, action: () -> Void)?
    var onDismiss: () -> Void

    var body: some View {
        let style = AlertStyle(kind: alert.kind)
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: style.icon)
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundColor(style.tint)
                Text(alert.title)
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundColor(Theme.primaryText)
                    .lineLimit(2)
            }

            Text(alert.message)
                .font(.system(size: 11.5))
                .foregroundColor(Theme.primaryText)
                .fixedSize(horizontal: false, vertical: true)

            if !alert.detail.isEmpty && alert.detail != alert.message {
                Text(alert.detail)
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundColor(Theme.secondaryText)
                    .lineLimit(2)
                    .textSelection(.enabled)
            }

            HStack(spacing: 8) {
                if let primary {
                    Button(primary.title, action: primary.action)
                        .buttonStyle(.borderedProminent)
                }
                Button("Dismiss", action: onDismiss)
                    .buttonStyle(.bordered)
            }
            .controlSize(.small)
            .focusable(false)
            .padding(.top, 2)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(CardBackground(fill: style.tint.opacity(0.08), stroke: style.tint.opacity(0.25)))
    }
}

struct MenuBarPopupView: View {
    @ObservedObject var vpn = VPNManager.shared
    @State private var showingAddModal = false
    @State private var showingSettings = false
    @State private var editingProfile: VPNProfileItem?
    var listHeight: CGFloat? = nil
    var width: CGFloat = 340

    @ViewBuilder private var profileList: some View {
        if vpn.profiles.isEmpty {
            VStack(spacing: 6) {
                Image(systemName: "network.badge.shield.half.filled")
                    .font(.system(size: 26, weight: .light))
                    .foregroundColor(Theme.tertiaryText)
                    .padding(.bottom, 4)
                Text("No VPN configurations")
                    .font(.system(size: 13, weight: .medium))
                    .foregroundColor(Theme.primaryText)
                Text("Add your L2TP/IPsec server to get started.")
                    .font(.system(size: 11))
                    .foregroundColor(Theme.secondaryText)
                    .multilineTextAlignment(.center)
                Button("Add Configuration…") { showingAddModal = true }
                    .controlSize(.small)
                    .focusable(false)
                    .padding(.top, 6)
            }
            .padding(.vertical, 22)
            .frame(maxWidth: .infinity)
            .background(CardBackground())
            .padding(.horizontal, Theme.inset)
        } else {
            VStack(spacing: 6) {
                ForEach(vpn.profiles) { profile in
                    ProfileCardRow(
                        profile: profile,
                        vpn: vpn,
                        onEdit: { editingProfile = profile },
                        onDelete: { vpn.deleteProfile(name: profile.name) }
                    )
                }
            }
            .padding(.horizontal, Theme.inset)
        }
    }

    private var activeProfile: VPNProfileItem? {
        vpn.profiles.first(where: { $0.name == vpn.activeProfileName })
    }

    private var headerStatus: String {
        if vpn.isDisconnecting { return "Disconnecting…" }
        if vpn.linkState == .idle && vpn.connectionLost { return "Connection Lost" }
        if vpn.isReconnecting && !vpn.networkOnline { return "Waiting for Network…" }
        if vpn.linkState == .idle && vpn.activeAlert != nil { return "Connection Failed" }
        return Theme.statusText(for: vpn.linkState, reconnecting: vpn.isReconnecting)
    }

    private var headerColor: Color {
        if vpn.isDisconnecting { return Theme.connecting }
        if vpn.linkState == .idle && vpn.activeAlert != nil { return Theme.danger }
        return Theme.color(for: vpn.linkState)
    }

    /// A one-line hint for what the current state means, after the status word.
    private var headerDetail: String {
        if vpn.isDisconnecting { return "Restoring your network settings" }
        let title = activeProfile?.title
        switch vpn.linkState {
        case .connected:
            return title.map { "Protected via \($0)" } ?? "Your traffic is protected"
        case .connecting:
            return title ?? "Establishing the tunnel"
        case .idle:
            return vpn.profiles.isEmpty ? "Add a configuration to get started" : "Your traffic is not protected"
        }
    }

    private var currentAlert: VPNAlertInfo? {
        // A stale ("already logged in") server session never reaches here — it's
        // recovered silently, with no alert at all (see VPNManager.applyFailure) — so
        // `.sessionStale` no longer appears as a real activeAlert.kind.
        vpn.activeAlert ?? vpn.errorMessage.map {
            VPNAlertInfo(kind: .generic, title: "Connection Error", message: $0, detail: "")
        }
    }

    private func primaryAction(for alert: VPNAlertInfo) -> (title: String, action: () -> Void)? {
        guard let profile = activeProfile else { return nil }
        switch alert.kind {
        case .authFailed: return ("Update Password", { editingProfile = profile })
        case .processStopped: return nil
        default: return ("Try Again", { vpn.connect(profileName: profile.name) })
        }
    }

    var body: some View {
        let state = vpn.linkState
        VStack(alignment: .leading, spacing: 0) {
            // Header: brand + overall link status.
            HStack(spacing: 10) {
                BrandTile(size: 30)
                VStack(alignment: .leading, spacing: 2) {
                    Text(AppBranding.name)
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundColor(Theme.primaryText)
                    HStack(spacing: 5) {
                        StatusDot(color: headerColor, size: 6, pulsing: state == .connecting || vpn.isDisconnecting).accessibilityHidden(true)
                        (Text(headerStatus).foregroundColor(Theme.primaryText)
                            + Text(" · \(headerDetail)").foregroundColor(Theme.secondaryText))
                            .font(.system(size: 11))
                            .lineLimit(1)
                            .truncationMode(.tail)
                            .id(headerStatus + headerDetail)
                            .transition(.opacity)
                    }
                }
                Spacer()
            }
            .padding(.horizontal, Theme.inset)
            .padding(.top, 14)
            .padding(.bottom, 12)

            if state == .connected, vpn.showPublicIP || !vpn.warnings.isEmpty {
                ConnectionDetails(ip: vpn.publicIP, showIP: vpn.showPublicIP, warnings: vpn.warnings)
                    .padding(.horizontal, Theme.inset)
                    .padding(.bottom, 12)
                    .transition(.opacity)
            }

            if let note = vpn.reconnectNote {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Image(systemName: "arrow.triangle.2.circlepath")
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundColor(Theme.connecting)
                    Text(note)
                        .font(.system(size: 11))
                        .foregroundColor(Theme.primaryText)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(10)
                .background(CardBackground(fill: Theme.connecting.opacity(0.08), stroke: Theme.connecting.opacity(0.25)))
                .padding(.horizontal, Theme.inset)
                .padding(.bottom, 12)
                .transition(.opacity)
            }

            Divider()

            HStack {
                Text("Configurations")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundColor(Theme.secondaryText)
                Spacer()
                IconButton(systemName: "plus", help: "Add configuration") { showingAddModal = true }
            }
            .padding(.leading, Theme.inset)
            .padding(.trailing, Theme.inset - 4)
            .padding(.top, 10)
            .padding(.bottom, 6)

            // The window passes a fixed list height (room for about 5 profiles, scrolls
            // beyond that); the menu bar popover keeps its content-sized list.
            if let listHeight {
                ScrollView(showsIndicators: false) { profileList }
                    .frame(height: listHeight)
            } else {
                profileList
            }

            // Outcome of the last edit / delete (saved, blocked while connected, failed).
            if let notice = vpn.notice {
                NoticeBanner(notice: notice, onDismiss: { vpn.dismissNotice() })
                    .padding(.horizontal, Theme.inset)
                    .padding(.top, 10)
                    .transition(.opacity)
            }

            if let alert = currentAlert {
                AlertCard(
                    alert: alert,
                    primary: primaryAction(for: alert),
                    onDismiss: { vpn.dismissAlert() }
                )
                .padding(.horizontal, Theme.inset)
                .padding(.top, 10)
                .transition(.opacity)
            }

            Divider().padding(.top, 12)

            // Footer: version and copyright; settings (MTU, logging, kill switch) open in a
            // sheet like the profile form.
            HStack(spacing: 2) {
                Text([AppBranding.version.map { "v\($0)" }, AppBranding.copyright].compactMap { $0 }.joined(separator: " · "))
                    .font(.system(size: 10.5).monospacedDigit())
                    .foregroundColor(Theme.tertiaryText)
                    .lineLimit(1)
                    .help(AppBranding.version.map { "\(AppBranding.name) \($0)" } ?? AppBranding.name)
                Spacer()
                IconButton(systemName: "gearshape", help: "Settings") { showingSettings = true }
                IconButton(systemName: "power", help: "Quit \(AppBranding.name) (⌘Q)") { NSApplication.shared.terminate(nil) }
            }
            .padding(.leading, Theme.inset)
            .padding(.trailing, Theme.inset - 4)
            .padding(.vertical, 8)
        }
        .frame(width: width)
        .animation(.easeInOut(duration: 0.25), value: state)
        .animation(.easeInOut(duration: 0.25), value: vpn.activeAlert)
        .animation(.easeInOut(duration: 0.25), value: vpn.notice)
        .sheet(isPresented: $showingSettings) {
            SettingsSheet(isPresented: $showingSettings)
        }
        .sheet(isPresented: $showingAddModal) {
            ProfileFormSheet(isPresented: $showingAddModal, initialProfile: nil)
        }
        .sheet(item: $editingProfile) { prof in
            ProfileFormSheet(isPresented: Binding(get: { editingProfile != nil }, set: { if !$0 { editingProfile = nil } }), initialProfile: prof)
        }
    }
}
