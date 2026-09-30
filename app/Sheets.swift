import Cocoa
import Combine
import Network
import SwiftUI
import UserNotifications

// MARK: - Sheets

/// Title row shared by the settings and profile sheets.
struct SheetHeader: View {
    var title: String
    var subtitle: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title)
                .font(.system(size: 15, weight: .semibold))
                .foregroundColor(Theme.primaryText)
            Text(subtitle)
                .font(.system(size: 11))
                .foregroundColor(Theme.secondaryText)
                .fixedSize(horizontal: false, vertical: true)
        }
    }
}

/// Label above a native text field.
struct FormField<Field: View>: View {
    var label: String
    @ViewBuilder var field: () -> Field

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(label)
                .font(.system(size: 11, weight: .medium))
                .foregroundColor(Theme.secondaryText)
            field()
                .textFieldStyle(.roundedBorder)
        }
    }
}

/// One settings card: title + explanation on the left, the control on the right.
struct SettingCard<Control: View>: View {
    var title: String
    var detail: String
    @ViewBuilder var control: () -> Control

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(.system(size: 12, weight: .medium))
                    .foregroundColor(Theme.primaryText)
                Text(detail)
                    .font(.system(size: 10.5))
                    .foregroundColor(Theme.secondaryText)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 8)
            control()
        }
        .padding(11)
        .background(CardBackground())
    }
}

/// Global settings, applied by the CLI on the next connect.
struct SettingsSheet: View {
    @Binding var isPresented: Bool
    @ObservedObject var vpn = VPNManager.shared
    @State private var notifications: ConnectionNotifier.Permission?
    @State private var autoUpdate = AppUpdater.autoCheck
    @State private var notificationsOn = ConnectionNotifier.enabled

    private func refreshNotifications() {
        ConnectionNotifier.permission { notifications = $0 }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SheetHeader(title: "Settings", subtitle: "Changes apply on the next connection.")
                .padding(.bottom, 4)

            SettingCard(
                title: "MTU",
                detail: "Packet size for every profile. Use 1280 if transfers stall on hotspots or PPPoE."
            ) {
                Picker("", selection: Binding(get: { vpn.mtu }, set: { vpn.setMTU($0) })) {
                    Text("1280").tag(1280)
                    Text("1400").tag(1400)
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(width: 110)
            }

            SettingCard(
                title: "Verbose log",
                detail: "Detailed protocol logging to /var/log/vpn.log (view with `vpn logs`). Milestones and errors are always logged."
            ) {
                Toggle("", isOn: Binding(get: { vpn.verbose }, set: { vpn.setVerbose($0) }))
                    .toggleStyle(.switch)
                    .controlSize(.small)
                    .labelsHidden()
            }

            SettingCard(
                title: "Kill switch",
                detail: "If a full-tunnel VPN drops, block internet traffic until it reconnects instead of letting it leak. If stuck, turn the VPN off or run `vpn repair`."
            ) {
                Toggle("", isOn: Binding(get: { vpn.killSwitch }, set: { vpn.setKillSwitch($0) }))
                    .toggleStyle(.switch)
                    .controlSize(.small)
                    .labelsHidden()
            }

            SettingCard(
                title: "Notifications",
                detail: "Alerts when the VPN drops or reconnects, even while this window is closed."
            ) {
                switch notifications {
                case .allowed:
                    Toggle("", isOn: Binding(get: { notificationsOn }, set: { notificationsOn = $0; ConnectionNotifier.enabled = $0 }))
                        .toggleStyle(.switch)
                        .controlSize(.small)
                        .labelsHidden()
                case .notAsked:
                    Button("Turn On") { ConnectionNotifier.requestAuthorization(refreshNotifications) }
                        .controlSize(.small)
                case .denied:
                    Button("Open Settings…") { ConnectionNotifier.openSystemSettings() }
                        .controlSize(.small)
                case nil:
                    EmptyView()
                }
            }

            SettingCard(
                title: "Software update",
                detail: "Check for a new version when the app opens\(AppBranding.version.map { " (you have \($0))" } ?? "")."
            ) {
                HStack(spacing: 10) {
                    Button("Check Now") { AppUpdater.check(manual: true) }
                        .controlSize(.small)
                    Toggle("", isOn: Binding(get: { autoUpdate }, set: { autoUpdate = $0; AppUpdater.autoCheck = $0 }))
                        .toggleStyle(.switch)
                        .controlSize(.small)
                        .labelsHidden()
                }
            }

            HStack {
                Spacer()
                Button("Done") { isPresented = false }
                    .keyboardShortcut(.defaultAction)
                    .buttonStyle(.borderedProminent)
                    .focusable(false)
            }
            .padding(.top, 6)
        }
        .padding(20)
        .frame(width: 380)
        .onAppear(perform: refreshNotifications)
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            refreshNotifications()
        }
    }
}

struct ProfileFormSheet: View {
    @Binding var isPresented: Bool
    var initialProfile: VPNProfileItem?

    @State private var name = ""
    @State private var server = ""
    @State private var user = ""
    @State private var password = ""
    @State private var psk = ""
    @State private var isFullTunnel = true

    var isEdit: Bool { initialProfile != nil }

    private func trimmed(_ v: String) -> String { v.trimmingCharacters(in: .whitespaces) }

    /// Adding needs everything; editing only needs the visible fields — an empty
    /// password / shared secret there means "keep the saved one".
    private var canSave: Bool {
        guard !trimmed(name).isEmpty, !trimmed(server).isEmpty else { return false }
        if isEdit { return !trimmed(user).isEmpty }
        return !trimmed(user).isEmpty && !password.isEmpty && !psk.isEmpty
    }

    private var secretPrompt: Text { Text(isEdit ? "Leave blank to keep" : "Required") }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SheetHeader(
                title: isEdit ? "Edit Configuration" : "New Configuration",
                subtitle: isEdit
                    ? "Password and shared secret stay saved in Keychain. Fill them in only to change them."
                    : "L2TP over IPsec. Ask your network administrator for these details."
            )
            .padding(.bottom, 4)

            FormField(label: "Display name") {
                TextField("", text: $name, prompt: Text("Office VPN"))
            }

            FormField(label: "Server address") {
                TextField("", text: $server, prompt: Text("vpn.example.com or 1.2.3.4"))
            }

            HStack(spacing: 10) {
                FormField(label: "Account name") {
                    TextField("", text: $user, prompt: Text("Required"))
                }
                FormField(label: "Password") {
                    SecureField("", text: $password, prompt: secretPrompt)
                }
            }

            FormField(label: "Shared secret") {
                SecureField("", text: $psk, prompt: secretPrompt)
            }

            SettingCard(title: "Send all traffic over VPN", detail: "Route all internet traffic through the VPN.") {
                Toggle("", isOn: $isFullTunnel)
                    .toggleStyle(.switch)
                    .controlSize(.small)
                    .labelsHidden()
            }
            .padding(.top, 2)

            HStack(spacing: 8) {
                Spacer()
                Button("Cancel") {
                    isPresented = false
                }
                .keyboardShortcut(.cancelAction)
                .focusable(false)

                Button(isEdit ? "Save" : "Create") {
                    guard canSave else { return }

                    VPNManager.shared.saveProfile(
                        name: initialProfile?.name ?? trimmed(name),
                        displayName: isEdit ? trimmed(name) : nil,
                        server: trimmed(server),
                        user: trimmed(user),
                        psk: psk,
                        password: password,
                        isFullTunnel: isFullTunnel,
                        isNew: !isEdit
                    )
                    isPresented = false
                }
                .keyboardShortcut(.defaultAction)
                .buttonStyle(.borderedProminent)
                .disabled(!canSave)
                .focusable(false)
            }
            .padding(.top, 6)
        }
        .padding(20)
        .frame(width: 380)
        .onAppear {
            if let p = initialProfile {
                name = p.title
                server = p.server
                user = p.username
                isFullTunnel = p.isFullTunnel
            }
        }
    }
}
