import React, { useState } from 'react';
import { Copy, Check, Terminal, FileCode, Cpu } from 'lucide-react';

const MAIN_SWIFT_CODE = `import Cocoa
import SwiftUI

// MARK: - Models for CLI Config and State

struct CLIAccount: Codable {
    var username: String?
}

struct CLIProfile: Codable {
    var server: String?
    var server_id: String?
    var full_tunnel: Bool?
    var default_account: String?
    var accounts: [String: CLIAccount]?
}

struct CLIConfig: Codable {
    var active_profile: String?
    var profiles: [String: CLIProfile]?
}

struct CLIState: Codable {
    var phase: String? // "CONNECTED", "CONNECTING", "DISCONNECTED", "FAILED"
    var profile: String?
    var account: String?
    var server: String?
    var pid: Int?
    var tun_device: String?
    var local_ip: String?
    var fail_stage: String?
    var fail_detail: String?
}

struct VPNProfileItem: Identifiable, Hashable {
    var id: String { name }
    var name: String
    var server: String
    var username: String
    var isFullTunnel: Bool
    var isConnected: Bool
    var isConnecting: Bool
}

// MARK: - Error & Alert Models

enum VPNAlertKind {
    case sessionStale
    case authFailed
    case ikeFailed
    case routeFailed
    case generic
}

struct VPNAlertInfo: Equatable {
    var kind: VPNAlertKind
    var title: String
    var message: String
    var detail: String
}

// MARK: - Status Bar Icon Generator

func makeMenuBarIcon(phase: String) -> NSImage {
    let size = NSSize(width: 18, height: 18)
    let img = NSImage(size: size, flipped: false) { rect in
        if phase == "CONNECTED" {
            // Vibrant Green / Emerald Shield with active badge
            let config = NSImage.SymbolConfiguration(pointSize: 14, weight: .bold)
            if let shield = NSImage(systemSymbolName: "checkmark.shield.fill", accessibilityDescription: nil)?.withSymbolConfiguration(config) {
                NSColor(red: 0.15, green: 0.9, blue: 0.55, alpha: 1.0).set()
                shield.draw(in: NSRect(x: 1, y: 1, width: 15, height: 15))
            }
            let badgeRect = NSRect(x: 12, y: 11, width: 5, height: 5)
            let badgePath = NSBezierPath(ovalIn: badgeRect)
            NSColor(red: 0.2, green: 0.98, blue: 0.65, alpha: 1.0).setFill()
            badgePath.fill()
            NSColor(red: 0.05, green: 0.3, blue: 0.15, alpha: 0.8).setStroke()
            badgePath.lineWidth = 0.5
            badgePath.stroke()
        } else if phase == "CONNECTING" {
            // Orange / Yellow Connecting Shield
            let config = NSImage.SymbolConfiguration(pointSize: 14, weight: .semibold)
            if let shield = NSImage(systemSymbolName: "shield.lefthalf.filled", accessibilityDescription: nil)?.withSymbolConfiguration(config) {
                NSColor(red: 0.98, green: 0.7, blue: 0.15, alpha: 1.0).set()
                shield.draw(in: NSRect(x: 1, y: 1, width: 15, height: 15))
            }
            let badgeRect = NSRect(x: 12, y: 11, width: 5, height: 5)
            let badgePath = NSBezierPath(ovalIn: badgeRect)
            NSColor.systemOrange.setFill()
            badgePath.fill()
        } else {
            // Clean Disconnected Shield
            let config = NSImage.SymbolConfiguration(pointSize: 14, weight: .medium)
            if let shield = NSImage(systemSymbolName: "shield", accessibilityDescription: nil)?.withSymbolConfiguration(config) {
                NSColor.labelColor.withAlphaComponent(0.85).set()
                shield.draw(in: NSRect(x: 1, y: 1, width: 15, height: 15))
            }
        }
        return true
    }
    
    // When connected or connecting, keep isTemplate = false so macOS preserves vivid RGB green/orange!
    img.isTemplate = (phase == "DISCONNECTED")
    return img
}

// MARK: - VPN Manager (Real CLI & File Sync)

@MainActor
final class VPNManager: ObservableObject {
    static let shared = VPNManager()

    @Published var profiles: [VPNProfileItem] = []
    @Published var activeProfileName: String?
    @Published var isConnected: Bool = false
    @Published var isConnecting: Bool = false
    @Published var currentPhase: String = "DISCONNECTED"
    @Published var currentIP: String = ""
    @Published var currentTunDevice: String = ""
    @Published var errorMessage: String?
    @Published var activeAlert: VPNAlertInfo?
    @Published var isStaleSession: Bool = false
    @Published var autoRetryCountdown: Int = 0

    // Settings
    @Published var autoConnectOnLaunch: Bool = false
    @Published var startAtLogin: Bool = true

    var onStatusChanged: ((String) -> Void)?
    private var pollTimer: Timer?
    private var retryTimer: Timer?

    private var configURL: URL {
        FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config")
            .appendingPathComponent("vpn")
            .appendingPathComponent("config.json")
    }

    private var stateURL: URL {
        URL(fileURLWithPath: "/var/run/vpn/state.json")
    }

    private let operationQueue = DispatchQueue(label: "com.tms.vpn.operationQueue", qos: .userInitiated)

    init() {
        syncFromDisk()
        startPolling()
    }

    func startPolling() {
        pollTimer?.invalidate()
        pollTimer = Timer.scheduledTimer(withTimeInterval: 1.0, repeats: true) { [weak self] _ in
            Task { @MainActor in
                self?.syncFromDisk()
            }
        }
    }

    func syncFromDisk() {
        // 1. Read State (/var/run/vpn/state.json)
        var activeProf: String?
        var phase = "DISCONNECTED"
        var localIP = ""
        var tunDev = ""

        if let stateData = try? Data(contentsOf: stateURL),
           let st = try? JSONDecoder().decode(CLIState.self, from: stateData) {
            phase = st.phase ?? "DISCONNECTED"
            activeProf = st.profile
            localIP = st.local_ip ?? ""
            tunDev = st.tun_device ?? ""
            if phase == "FAILED" {
                let stage = st.fail_stage ?? ""
                let detail = st.fail_detail ?? ""
                
                if detail.contains("already logged in") || detail.contains("You are already logged in") {
                    self.activeAlert = VPNAlertInfo(
                        kind: .sessionStale,
                        title: "Session Stale",
                        message: "This account already has an active session on the server ('Already logged in').",
                        detail: detail
                    )
                    self.errorMessage = "Session Stale: this account already has an active session on the server."
                    self.isStaleSession = true
                    if self.autoRetryCountdown == 0 && self.retryTimer == nil && self.activeProfileName != nil {
                        self.startAutoRetry(profileName: self.activeProfileName!)
                    }
                } else if stage == "PPP_AUTH_FAILURE" || detail.contains("CHAP authentication rejected") {
                    self.activeAlert = VPNAlertInfo(
                        kind: .authFailed,
                        title: "Authentication Failed",
                        message: "PPP/CHAP authentication failed: wrong account name or password.",
                        detail: detail.isEmpty ? "CHAP authentication rejected by peer" : detail
                    )
                    self.errorMessage = "Authentication Failed: wrong account name or password."
                    self.isStaleSession = false
                } else if stage == "IKE_FAILED" || stage.contains("IKE") {
                    self.activeAlert = VPNAlertInfo(
                        kind: .ikeFailed,
                        title: "IKE Handshake Failed",
                        message: "IPsec IKE handshake failed: wrong server address or shared secret.",
                        detail: detail
                    )
                    self.errorMessage = "IKE Handshake Failed: wrong server address or shared secret."
                    self.isStaleSession = false
                } else if stage == "ROUTE_FAILURE" {
                    self.activeAlert = VPNAlertInfo(
                        kind: .routeFailed,
                        title: "Routing Error",
                        message: "Could not set up network routes. Run \`vpn repair\`, then try again.",
                        detail: detail
                    )
                    self.errorMessage = "Routing Error: could not set up network routes."
                    self.isStaleSession = false
                } else {
                    self.activeAlert = VPNAlertInfo(
                        kind: .generic,
                        title: stage.isEmpty ? "Connection Error" : stage,
                        message: detail.isEmpty ? "Connection failed" : detail,
                        detail: detail
                    )
                    self.errorMessage = "\(stage.isEmpty ? "Connection Error" : stage): \(detail)"
                    self.isStaleSession = false
                }
            } else {
                if phase == "CONNECTED" || phase == "CONNECTING" {
                    self.errorMessage = nil
                    self.activeAlert = nil
                    self.isStaleSession = false
                    self.cancelAutoRetry()
                }
            }
        }

        let oldPhase = self.currentPhase
        self.currentPhase = phase
        self.isConnected = (phase == "CONNECTED")
        self.isConnecting = (phase == "CONNECTING")
        self.currentIP = localIP
        self.currentTunDevice = tunDev

        if oldPhase != phase || self.onStatusChanged != nil {
            self.onStatusChanged?(phase)
        }

        // 2. Read Config (~/.config/vpn/config.json)
        if let configData = try? Data(contentsOf: configURL),
           let cfg = try? JSONDecoder().decode(CLIConfig.self, from: configData) {
            
            if activeProf == nil {
                activeProf = cfg.active_profile
            }
            self.activeProfileName = activeProf

            var items: [VPNProfileItem] = []
            if let profDict = cfg.profiles {
                for (pName, pVal) in profDict {
                    let isConn = (self.isConnected && (activeProf == pName))
                    let isConnIng = (self.isConnecting && (activeProf == pName))
                    let user = pVal.default_account ?? pVal.accounts?.keys.first ?? ""
                    items.append(VPNProfileItem(
                        name: pName,
                        server: pVal.server ?? "",
                        username: user,
                        isFullTunnel: pVal.full_tunnel ?? true,
                        isConnected: isConn,
                        isConnecting: isConnIng
                    ))
                }
            }
            self.profiles = items.sorted { $0.name.lowercased() < $1.name.lowercased() }
        } else {
            if self.profiles.isEmpty {
                self.profiles = []
            }
        }
    }

    func startAutoRetry(profileName: String) {
        self.cancelAutoRetry()
        self.autoRetryCountdown = 5
        self.retryTimer = Timer.scheduledTimer(withTimeInterval: 1.0, repeats: true) { [weak self] timer in
            Task { @MainActor in
                guard let self = self else { timer.invalidate(); return }
                if self.autoRetryCountdown > 1 {
                    self.autoRetryCountdown -= 1
                } else {
                    self.cancelAutoRetry()
                    self.cleanupAndForceConnect(profileName: profileName)
                }
            }
        }
    }

    func cancelAutoRetry() {
        self.retryTimer?.invalidate()
        self.retryTimer = nil
        self.autoRetryCountdown = 0
    }

    func toggleConnect(profile: VPNProfileItem) {
        if profile.isConnected || profile.isConnecting {
            disconnect()
        } else {
            connect(profileName: profile.name)
        }
    }

    func connect(profileName: String) {
        cancelAutoRetry()
        
        // 0ms INSTANT OPTIMISTIC UI FEEDBACK
        self.isConnecting = true
        self.isConnected = false
        self.currentPhase = "CONNECTING"
        self.activeProfileName = profileName
        self.errorMessage = nil
        self.activeAlert = nil
        self.isStaleSession = false
        self.profiles = self.profiles.map { p in
            var copy = p
            if p.name == profileName {
                copy.isConnecting = true
                copy.isConnected = false
            } else {
                copy.isConnecting = false
                copy.isConnected = false
            }
            return copy
        }
        self.onStatusChanged?("CONNECTING")

        // Serialized execution to prevent race condition when rapidly toggling off/on
        operationQueue.async { [weak self] in
            let cli = "/usr/local/bin/vpn"

            // 1. Graceful disconnect of any existing session
            let pDisc = Process()
            pDisc.executableURL = URL(fileURLWithPath: cli)
            pDisc.arguments = ["disconnect"]
            try? pDisc.run()
            pDisc.waitUntilExit()

            // 2. Kill any hung CLI process safely
            let pKill = Process()
            pKill.executableURL = URL(fileURLWithPath: "/usr/bin/pkill")
            pKill.arguments = ["-9", "-f", "vpn connect"]
            try? pKill.run()
            pKill.waitUntilExit()

            // 3. Short cooldown (0.3s) for interface & PID release
            Thread.sleep(forTimeInterval: 0.3)

            // 4. Launch new connection
            let task = Process()
            task.executableURL = URL(fileURLWithPath: cli)
            task.arguments = ["connect", "--profile", profileName]
            try? task.run()

            Task { @MainActor in
                self?.syncFromDisk()
            }
        }
    }

    func cleanupAndForceConnect(profileName: String) {
        cancelAutoRetry()
        self.isConnecting = true
        self.isConnected = false
        self.currentPhase = "CONNECTING"
        self.activeProfileName = profileName
        self.errorMessage = "Signing out the previous session and reconnecting..."
        self.profiles = self.profiles.map { p in
            var copy = p
            if p.name == profileName {
                copy.isConnecting = true
                copy.isConnected = false
            } else {
                copy.isConnecting = false
                copy.isConnected = false
            }
            return copy
        }
        self.onStatusChanged?("CONNECTING")

        operationQueue.async { [weak self] in
            let cli = "/usr/local/bin/vpn"

            // 1. Explicit CLI Disconnect / Logout
            let pDisc = Process()
            pDisc.executableURL = URL(fileURLWithPath: cli)
            pDisc.arguments = ["disconnect"]
            try? pDisc.run()
            pDisc.waitUntilExit()

            // 2. Terminate background processes
            let pKill = Process()
            pKill.executableURL = URL(fileURLWithPath: "/usr/bin/pkill")
            pKill.arguments = ["-9", "-f", "vpn connect"]
            try? pKill.run()
            pKill.waitUntilExit()

            // 3. Repair routing / DNS
            let pRep = Process()
            pRep.executableURL = URL(fileURLWithPath: cli)
            pRep.arguments = ["repair"]
            try? pRep.run()
            pRep.waitUntilExit()

            // 4. Give server 1.2s to flush RADIUS/L2TP session
            Thread.sleep(forTimeInterval: 1.2)

            // 5. Connect again
            let task = Process()
            task.executableURL = URL(fileURLWithPath: cli)
            task.arguments = ["connect", "--profile", profileName]
            try? task.run()

            Task { @MainActor in
                self?.syncFromDisk()
            }
        }
    }

    func disconnect() {
        cancelAutoRetry()
        
        // 0ms INSTANT OPTIMISTIC UI RESET
        self.isConnecting = false
        self.isConnected = false
        self.isStaleSession = false
        self.errorMessage = nil
        self.currentPhase = "DISCONNECTED"
        self.profiles = self.profiles.map { p in
            var copy = p
            copy.isConnected = false
            copy.isConnecting = false
            return copy
        }
        self.onStatusChanged?("DISCONNECTED")

        operationQueue.async { [weak self] in
            let task = Process()
            task.executableURL = URL(fileURLWithPath: "/usr/local/bin/vpn")
            task.arguments = ["disconnect"]
            try? task.run()
            task.waitUntilExit()

            let pKill = Process()
            pKill.executableURL = URL(fileURLWithPath: "/usr/bin/pkill")
            pKill.arguments = ["-9", "-f", "vpn connect"]
            try? pKill.run()
            pKill.waitUntilExit()

            Task { @MainActor in
                self?.syncFromDisk()
            }
        }
    }

    func repairNetwork() {
        cancelAutoRetry()
        DispatchQueue.global(qos: .userInitiated).async {
            let task = Process()
            task.executableURL = URL(fileURLWithPath: "/usr/local/bin/vpn")
            task.arguments = ["repair"]
            try? task.run()
        }
    }

    func deleteProfile(name: String) {
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let task = Process()
            task.executableURL = URL(fileURLWithPath: "/usr/local/bin/vpn")
            task.arguments = ["profile", "remove", name]
            try? task.run()
            task.waitUntilExit()

            Task { @MainActor in
                self?.syncFromDisk()
            }
        }
    }

    func saveProfile(name: String, server: String, user: String, psk: String, password: String, isFullTunnel: Bool, isNew: Bool) {
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let cli = "/usr/local/bin/vpn"
            
            // 1. Add/Update Profile
            let addProf = Process()
            addProf.executableURL = URL(fileURLWithPath: cli)
            var args = ["profile", "add", name, "--server", server, "--psk", psk]
            if isFullTunnel { args.append("--full-tunnel") }
            addProf.arguments = args
            try? addProf.run()
            addProf.waitUntilExit()

            // 2. Add/Update Account
            if !user.isEmpty {
                let addAcct = Process()
                addAcct.executableURL = URL(fileURLWithPath: cli)
                addAcct.arguments = ["account", "add", name, user, "--password", password, "--default"]
                try? addAcct.run()
                addAcct.waitUntilExit()
            }

            Task { @MainActor in
                self?.syncFromDisk()
            }
        }
    }
}

// MARK: - MacOSMenuBar Swift Component (Live SwiftUI View in macOS Status Bar)

struct MacOSMenuBar: View {
    @ObservedObject var vpn = VPNManager.shared
    @State private var isRotating: Bool = false
    @State private var isPulsing: Bool = false

    var body: some View {
        HStack(spacing: 0) {
            ZStack {
                if vpn.isConnecting {
                    RoundedRectangle(cornerRadius: 6)
                        .stroke(Color.orange.opacity(0.2), lineWidth: 1)
                        .overlay(
                            AngularGradient(
                                gradient: Gradient(colors: [
                                    Color.clear,
                                    Color.clear,
                                    Color.orange.opacity(0.3),
                                    Color.orange,
                                    Color(red: 1.0, green: 0.95, blue: 0.7),
                                    Color.clear
                                ]),
                                center: .center
                            )
                            .rotationEffect(.degrees(isRotating ? 360 : 0))
                            .animation(
                                Animation.linear(duration: 1.8).repeatForever(autoreverses: false),
                                value: isRotating
                            )
                            .mask(
                                RoundedRectangle(cornerRadius: 6)
                                    .stroke(lineWidth: 2.0)
                            )
                        )
                        .shadow(color: Color.orange.opacity(0.85), radius: 4)
                        .onAppear {
                            isRotating = true
                        }
                } else if vpn.isConnected {
                    RoundedRectangle(cornerRadius: 6)
                        .stroke(
                            Color(red: 0.2, green: 0.9, blue: 0.55),
                            lineWidth: isPulsing ? 1.8 : 1.0
                        )
                        .shadow(
                            color: Color(red: 0.2, green: 0.95, blue: 0.6).opacity(isPulsing ? 0.9 : 0.2),
                            radius: isPulsing ? 5 : 2
                        )
                        .opacity(isPulsing ? 1.0 : 0.75)
                        .animation(
                            Animation.easeInOut(duration: 1.2).repeatForever(autoreverses: true),
                            value: isPulsing
                        )
                        .onAppear {
                            isPulsing = true
                        }
                } else {
                    RoundedRectangle(cornerRadius: 6)
                        .stroke(Color.white.opacity(0.15), lineWidth: 1)
                }

                Image(systemName: vpn.isConnected ? "checkmark.shield.fill" : (vpn.isConnecting ? "shield.lefthalf.filled" : "shield"))
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundColor(
                        vpn.isConnected ? Color(red: 0.2, green: 0.95, blue: 0.6) :
                        (vpn.isConnecting ? Color.orange : Color.white.opacity(0.85))
                    )

                if vpn.isConnected {
                    Circle()
                        .fill(Color(red: 0.2, green: 0.98, blue: 0.65))
                        .frame(width: 5, height: 5)
                        .offset(x: 6, y: -6)
                } else if vpn.isConnecting {
                    Circle()
                        .fill(Color.orange)
                        .frame(width: 5, height: 5)
                        .offset(x: 6, y: -6)
                }
            }
            .frame(width: 22, height: 22)
        }
        .frame(width: 28, height: 22)
        .contentShape(Rectangle())
        .onTapGesture {
            AppDelegate.shared?.togglePopover(nil)
        }
    }
}

// MARK: - Animated Rotating Linear Border Component (SwiftUI Native 60/120fps)

struct ConnectingLinearBorder: View {
    var cornerRadius: CGFloat = 13
    @State private var isRotating = false

    var body: some View {
        RoundedRectangle(cornerRadius: cornerRadius)
            .stroke(Color.orange.opacity(0.15), lineWidth: 1.5)
            .overlay(
                AngularGradient(
                    gradient: Gradient(colors: [
                        Color.clear,
                        Color.clear,
                        Color.orange.opacity(0.25),
                        Color.orange,
                        Color(red: 1.0, green: 0.95, blue: 0.7),
                        Color.clear
                    ]),
                    center: .center
                )
                .rotationEffect(.degrees(isRotating ? 360 : 0))
                .animation(
                    Animation.linear(duration: 1.8).repeatForever(autoreverses: false),
                    value: isRotating
                )
                .mask(
                    RoundedRectangle(cornerRadius: cornerRadius)
                        .stroke(lineWidth: 2.2)
                )
            )
            .shadow(color: Color.orange.opacity(0.8), radius: 8)
            .onAppear {
                isRotating = true
            }
    }
}

// MARK: - Connected Green Pulse Border (SwiftUI Native)

struct ConnectedPulseBorder: View {
    var cornerRadius: CGFloat = 13
    @State private var isPulsing = false

    var body: some View {
        RoundedRectangle(cornerRadius: cornerRadius)
            .stroke(
                Color(red: 0.2, green: 0.9, blue: 0.55),
                lineWidth: isPulsing ? 2.0 : 1.2
            )
            .shadow(
                color: Color(red: 0.2, green: 0.95, blue: 0.6).opacity(isPulsing ? 0.9 : 0.25),
                radius: isPulsing ? 9 : 2
            )
            .opacity(isPulsing ? 1.0 : 0.75)
            .animation(
                Animation.easeInOut(duration: 1.2).repeatForever(autoreverses: true),
                value: isPulsing
            )
            .onAppear {
                isPulsing = true
            }
    }
}

// MARK: - Native Helper for Menu Popups without ugly Dropdown Chevrons

struct CustomMenuButton: View {
    var onEdit: () -> Void
    var onDelete: () -> Void

    var body: some View {
        Button(action: showNativeMenu) {
            Image(systemName: "ellipsis")
                .font(.system(size: 15, weight: .bold))
                .foregroundColor(Color(red: 0.6, green: 0.65, blue: 0.72))
                .frame(width: 28, height: 28)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func showNativeMenu() {
        let menu = NSMenu()
        let editItem = NSMenuItem(title: "Edit", action: #selector(MenuHelper.editAction), keyEquivalent: "")
        let deleteItem = NSMenuItem(title: "Delete", action: #selector(MenuHelper.deleteAction), keyEquivalent: "")
        deleteItem.attributedTitle = NSAttributedString(string: "Delete", attributes: [.foregroundColor: NSColor.systemRed])

        let helper = MenuHelper(onEdit: onEdit, onDelete: onDelete)
        editItem.target = helper
        deleteItem.target = helper
        
        menu.addItem(editItem)
        menu.addItem(NSMenuItem.separator())
        menu.addItem(deleteItem)

        if let event = NSApp.currentEvent {
            NSMenu.popUpContextMenu(menu, with: event, for: NSApp.keyWindow?.contentView ?? NSView())
        }
    }
}

@MainActor
final class MenuHelper: NSObject {
    let onEdit: () -> Void
    let onDelete: () -> Void

    init(onEdit: @escaping () -> Void, onDelete: @escaping () -> Void) {
        self.onEdit = onEdit
        self.onDelete = onDelete
        super.init()
    }

    @objc func editAction() { onEdit() }
    @objc func deleteAction() { onDelete() }
}

// MARK: - Main Menu Bar Popup View

struct ProfileCardRow: View {
    let profile: VPNProfileItem
    @ObservedObject var vpn: VPNManager
    var onEdit: () -> Void
    var onDelete: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            // Status Dot
            Circle()
                .fill(profile.isConnected ? Color(red: 0.2, green: 0.88, blue: 0.55) : (profile.isConnecting ? Color.orange : Color.gray.opacity(0.6)))
                .frame(width: 9, height: 9)
                .shadow(color: profile.isConnected ? Color.green.opacity(0.8) : (profile.isConnecting ? Color.orange.opacity(0.8) : Color.clear), radius: 4)

            // Profile Name & Subtitle
            VStack(alignment: .leading, spacing: 2) {
                Text(profile.name)
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundColor(profile.isConnected ? .white : (profile.isConnecting ? Color(red: 1.0, green: 0.9, blue: 0.7) : Color(red: 0.85, green: 0.88, blue: 0.92)))
                    .lineLimit(1)
                    .truncationMode(.tail)

                HStack(spacing: 4) {
                    Text(profile.server)
                        .font(.system(size: 11))
                        .foregroundColor(Color.gray)
                        .lineLimit(1)
                    if !profile.username.isEmpty {
                        Text("• \(profile.username)")
                            .font(.system(size: 11))
                            .foregroundColor(Color.gray.opacity(0.8))
                            .lineLimit(1)
                    }
                }
            }

            Spacer(minLength: 8)

            // Toggle Switch
            Toggle("", isOn: Binding(
                get: { profile.isConnected || profile.isConnecting },
                set: { _ in vpn.toggleConnect(profile: profile) }
            ))
            .toggleStyle(SwitchToggleStyle(tint: profile.isConnecting ? Color.orange : Color(red: 0.15, green: 0.8, blue: 0.55)))
            .labelsHidden()

            // Context Menu Button
            CustomMenuButton(
                onEdit: onEdit,
                onDelete: onDelete
            )
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(
            ZStack {
                RoundedRectangle(cornerRadius: 13)
                    .fill(
                        profile.isConnected ? Color(red: 0.04, green: 0.18, blue: 0.12) :
                        (profile.isConnecting ? Color(red: 0.2, green: 0.12, blue: 0.04) : Color(red: 0.1, green: 0.12, blue: 0.16).opacity(0.85))
                    )

                if profile.isConnecting {
                    ConnectingLinearBorder(cornerRadius: 13)
                } else if profile.isConnected {
                    ConnectedPulseBorder(cornerRadius: 13)
                } else {
                    RoundedRectangle(cornerRadius: 13)
                        .stroke(Color.white.opacity(0.08), lineWidth: 1)
                }
            }
        )
    }
}

struct MenuBarPopupView: View {
    @ObservedObject var vpn = VPNManager.shared
    @State private var showingAddModal = false
    @State private var editingProfile: VPNProfileItem?

    var body: some View {
        VStack(spacing: 0) {
            // Header Bar (Clean, NO Settings Icon)
            HStack(spacing: 12) {
                ZStack {
                    RoundedRectangle(cornerRadius: 12)
                        .fill(
                            vpn.isConnected ? Color(red: 0.04, green: 0.18, blue: 0.12) :
                            (vpn.isConnecting ? Color(red: 0.2, green: 0.12, blue: 0.04) : Color(red: 0.05, green: 0.16, blue: 0.22))
                        )

                    if vpn.isConnecting {
                        ConnectingLinearBorder(cornerRadius: 12)
                    } else if vpn.isConnected {
                        ConnectedPulseBorder(cornerRadius: 12)
                    } else {
                        RoundedRectangle(cornerRadius: 12)
                            .stroke(Color(red: 0.12, green: 0.55, blue: 0.65).opacity(0.6), lineWidth: 1)
                    }

                    Image(systemName: vpn.isConnected ? "checkmark.shield.fill" : (vpn.isConnecting ? "shield.lefthalf.filled" : "shield.fill"))
                        .font(.system(size: 20))
                        .foregroundColor(
                            vpn.isConnected ? Color(red: 0.2, green: 0.9, blue: 0.6) :
                            (vpn.isConnecting ? Color.orange : Color(red: 0.2, green: 0.75, blue: 0.95))
                        )
                    
                    if vpn.isConnected {
                        Circle()
                            .fill(Color(red: 0.2, green: 0.95, blue: 0.6))
                            .frame(width: 7, height: 7)
                            .offset(x: 9, y: -9)
                    } else if vpn.isConnecting {
                        Circle()
                            .fill(Color.orange)
                            .frame(width: 7, height: 7)
                            .offset(x: 9, y: -9)
                    }
                }
                .frame(width: 40, height: 40)

                HStack(spacing: 6) {
                    Text("TMS VPN")
                        .font(.system(size: 16, weight: .bold))
                        .foregroundColor(.white)
                    Circle()
                        .fill(vpn.isConnected ? Color(red: 0.2, green: 0.85, blue: 0.55) : (vpn.isConnecting ? Color.orange : Color.gray))
                        .frame(width: 7, height: 7)
                    Text(vpn.isConnected ? "Connected" : (vpn.isConnecting ? "Connecting..." : "Not Connected"))
                        .font(.system(size: 13, weight: .medium))
                        .foregroundColor(vpn.isConnected ? Color(red: 0.2, green: 0.85, blue: 0.55) : (vpn.isConnecting ? Color.orange : Color.gray))
                }

                Spacer()
            }
            .padding(.horizontal, 16)
            .padding(.top, 16)
            .padding(.bottom, 12)

            Divider().background(Color.white.opacity(0.08))

            // Active Connection Info Banner when connected
            if vpn.isConnected {
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("ACTIVE")
                            .font(.system(size: 10, weight: .bold))
                            .foregroundColor(Color(red: 0.2, green: 0.85, blue: 0.55))
                        HStack(spacing: 8) {
                            if !vpn.currentIP.isEmpty {
                                Text("IP: \(vpn.currentIP)")
                                    .font(.system(size: 11, weight: .medium))
                                    .foregroundColor(.white)
                            }
                            if !vpn.currentTunDevice.isEmpty {
                                Text("(\(vpn.currentTunDevice))")
                                    .font(.system(size: 11))
                                    .foregroundColor(.gray)
                            }
                        }
                    }
                    Spacer()
                    Button(action: { vpn.disconnect() }) {
                        Text("Disconnect")
                            .font(.system(size: 11, weight: .medium))
                            .foregroundColor(Color.red.opacity(0.9))
                            .padding(.horizontal, 8)
                            .padding(.vertical, 4)
                            .background(
                                RoundedRectangle(cornerRadius: 6)
                                    .fill(Color.red.opacity(0.12))
                            )
                    }
                    .buttonStyle(.plain)
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 8)
                .background(Color(red: 0.04, green: 0.15, blue: 0.1))

                Divider().background(Color.white.opacity(0.08))
            }

            // Subheader: Profile List Header
            HStack {
                Text("CONFIGURATIONS")
                    .font(.system(size: 11, weight: .bold))
                    .foregroundColor(Color(red: 0.52, green: 0.58, blue: 0.66))
                Spacer()
                Button(action: { showingAddModal = true }) {
                    HStack(spacing: 4) {
                        Image(systemName: "plus")
                            .font(.system(size: 10, weight: .bold))
                        Text("Add")
                            .font(.system(size: 11, weight: .semibold))
                    }
                    .foregroundColor(Color(red: 0.2, green: 0.85, blue: 0.95))
                    .padding(.horizontal, 10)
                    .padding(.vertical, 5)
                    .background(
                        RoundedRectangle(cornerRadius: 8)
                            .fill(Color(red: 0.05, green: 0.16, blue: 0.22))
                            .overlay(RoundedRectangle(cornerRadius: 8).stroke(Color(red: 0.12, green: 0.55, blue: 0.65).opacity(0.6), lineWidth: 1))
                    )
                }
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 16)
            .padding(.top, 14)
            .padding(.bottom, 8)

            // Profile List or Empty State
            if vpn.profiles.isEmpty {
                VStack(spacing: 8) {
                    Image(systemName: "network.badge.shield.half.filled")
                        .font(.system(size: 32))
                        .foregroundColor(Color.gray.opacity(0.5))
                        .padding(.top, 10)
                    Text("No VPN configurations")
                        .font(.system(size: 13, weight: .medium))
                        .foregroundColor(.gray)
                    Text("Click \"+ Add\" to set up your first VPN.")
                        .font(.system(size: 11))
                        .foregroundColor(Color.gray.opacity(0.7))
                        .multilineTextAlignment(.center)
                        .padding(.horizontal, 20)
                }
                .padding(.vertical, 20)
                .frame(maxWidth: .infinity)
            } else {
                VStack(spacing: 8) {
                    ForEach(vpn.profiles) { profile in
                        ProfileCardRow(
                            profile: profile,
                            vpn: vpn,
                            onEdit: { editingProfile = profile },
                            onDelete: { vpn.deleteProfile(name: profile.name) }
                        )
                    }
                }
                .padding(.horizontal, 16)
            }

            // Error or Stale Session Notice / Alert Card
            if let alert = vpn.activeAlert ?? (vpn.errorMessage != nil ? VPNAlertInfo(kind: vpn.isStaleSession ? .sessionStale : .generic, title: vpn.isStaleSession ? "Session Stale" : "Connection Error", message: vpn.errorMessage ?? "", detail: "") : nil) {
                VStack(alignment: .leading, spacing: 8) {
                    // Header Badge & Title
                    HStack(spacing: 6) {
                        Image(systemName: alert.kind == .sessionStale ? "clock.arrow.circlepath" : (alert.kind == .authFailed ? "lock.slash.fill" : "exclamationmark.triangle.fill"))
                            .font(.system(size: 13, weight: .bold))
                            .foregroundColor(alert.kind == .sessionStale ? Color.orange : (alert.kind == .authFailed ? Color(red: 1.0, green: 0.35, blue: 0.35) : Color.yellow))

                        Text(alert.title)
                            .font(.system(size: 12, weight: .bold))
                            .foregroundColor(alert.kind == .sessionStale ? Color.orange : (alert.kind == .authFailed ? Color(red: 1.0, green: 0.45, blue: 0.45) : Color.yellow))

                        Spacer()

                        Text(alert.kind == .sessionStale ? "Stale Session" : (alert.kind == .authFailed ? "Credentials" : "Warning"))
                            .font(.system(size: 9.5, weight: .bold))
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(
                                Capsule()
                                    .fill(alert.kind == .sessionStale ? Color.orange.opacity(0.2) : (alert.kind == .authFailed ? Color.red.opacity(0.2) : Color.yellow.opacity(0.2)))
                            )
                            .foregroundColor(alert.kind == .sessionStale ? Color.orange : (alert.kind == .authFailed ? Color(red: 1.0, green: 0.5, blue: 0.5) : Color.yellow))
                    }

                    // Message & Detail
                    VStack(alignment: .leading, spacing: 3) {
                        Text(alert.message)
                            .font(.system(size: 11.5, weight: .medium))
                            .foregroundColor(Color.white.opacity(0.92))
                            .fixedSize(horizontal: false, vertical: true)

                        if !alert.detail.isEmpty && alert.detail != alert.message {
                            Text(alert.detail)
                                .font(.system(size: 10, design: .monospaced))
                                .foregroundColor(Color.gray)
                                .lineLimit(2)
                        }

                        if alert.kind == .sessionStale && vpn.autoRetryCountdown > 0 {
                            Text("Cleaning up the previous session... retrying in \(vpn.autoRetryCountdown)s")
                                .font(.system(size: 11, weight: .semibold))
                                .foregroundColor(Color(red: 0.3, green: 0.85, blue: 1.0))
                                .padding(.top, 2)
                        }
                    }

                    // Action Controls
                    if alert.kind == .sessionStale {
                        HStack(spacing: 8) {
                            Button(action: {
                                vpn.cleanupAndForceConnect(profileName: vpn.activeProfileName ?? "")
                            }) {
                                HStack(spacing: 4) {
                                    Image(systemName: "arrow.clockwise")
                                    Text("Clean Up & Retry Now")
                                }
                                .font(.system(size: 11, weight: .semibold))
                                .foregroundColor(.white)
                                .padding(.horizontal, 10)
                                .padding(.vertical, 5)
                                .background(
                                    RoundedRectangle(cornerRadius: 6)
                                        .fill(Color.orange.opacity(0.9))
                                )
                            }
                            .buttonStyle(.plain)

                            Button(action: {
                                vpn.cancelAutoRetry()
                                vpn.disconnect()
                            }) {
                                Text("Cancel")
                                    .font(.system(size: 11))
                                    .foregroundColor(.gray)
                                    .padding(.horizontal, 8)
                                    .padding(.vertical, 5)
                            }
                            .buttonStyle(.plain)
                        }
                        .padding(.top, 2)
                    } else if alert.kind == .authFailed {
                        HStack(spacing: 8) {
                            if let actProf = vpn.profiles.first(where: { $0.name == vpn.activeProfileName }) {
                                Button(action: {
                                    editingProfile = actProf
                                }) {
                                    HStack(spacing: 4) {
                                        Image(systemName: "pencil")
                                        Text("Edit Password")
                                    }
                                    .font(.system(size: 11, weight: .semibold))
                                    .foregroundColor(.white)
                                    .padding(.horizontal, 10)
                                    .padding(.vertical, 5)
                                    .background(
                                        RoundedRectangle(cornerRadius: 6)
                                            .fill(Color(red: 0.8, green: 0.25, blue: 0.25))
                                    )
                                }
                                .buttonStyle(.plain)
                            }

                            Button(action: {
                                vpn.errorMessage = nil
                                vpn.activeAlert = nil
                            }) {
                                Text("Dismiss")
                                    .font(.system(size: 11))
                                    .foregroundColor(.gray)
                                    .padding(.horizontal, 8)
                                    .padding(.vertical, 5)
                            }
                            .buttonStyle(.plain)
                        }
                        .padding(.top, 2)
                    }
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 10)
                .background(
                    RoundedRectangle(cornerRadius: 10)
                        .fill(alert.kind == .sessionStale ? Color(red: 0.22, green: 0.14, blue: 0.04) : (alert.kind == .authFailed ? Color(red: 0.24, green: 0.07, blue: 0.07) : Color(red: 0.18, green: 0.12, blue: 0.05)))
                        .overlay(
                            RoundedRectangle(cornerRadius: 10)
                                .stroke(alert.kind == .sessionStale ? Color.orange.opacity(0.4) : (alert.kind == .authFailed ? Color.red.opacity(0.4) : Color.yellow.opacity(0.3)), lineWidth: 1)
                        )
                )
                .padding(.horizontal, 16)
                .padding(.top, 8)
            }

            Divider().background(Color.white.opacity(0.08)).padding(.top, 12)

            // Footer Bar (Clean, NO Settings button)
            HStack {
                Text("TMS VPN Client")
                    .font(.system(size: 11, weight: .medium))
                    .foregroundColor(Color.gray.opacity(0.7))

                Spacer()

                Button(action: { NSApplication.shared.terminate(nil) }) {
                    HStack(spacing: 5) {
                        Image(systemName: "rectangle.portrait.and.arrow.right")
                        Text("Quit").font(.system(size: 12, weight: .medium))
                        Text("⌘Q").font(.system(size: 10, weight: .semibold)).foregroundColor(Color.gray.opacity(0.6))
                    }
                    .foregroundColor(Color(red: 0.7, green: 0.74, blue: 0.8))
                }
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
        }
        .frame(width: 370)
        .background(Color(red: 0.07, green: 0.09, blue: 0.12))
        .sheet(isPresented: $showingAddModal) {
            ProfileFormSheet(isPresented: $showingAddModal, initialProfile: nil)
        }
        .sheet(item: $editingProfile) { prof in
            ProfileFormSheet(isPresented: Binding(get: { editingProfile != nil }, set: { if !$0 { editingProfile = nil } }), initialProfile: prof)
        }
    }
}

// MARK: - Add / Edit Profile Sheet (Polished Dark Theme matching Web Demo exactly)

struct CleanDarkTextField: View {
    var placeholder: String
    @Binding var text: String
    var isDisabled: Bool = false

    var body: some View {
        ZStack(alignment: .leading) {
            if text.isEmpty {
                Text(placeholder)
                    .foregroundColor(Color(red: 0.4, green: 0.48, blue: 0.58))
                    .font(.system(size: 12.5))
                    .padding(.horizontal, 12)
            }
            TextField("", text: $text)
                .textFieldStyle(.plain)
                .font(.system(size: 13))
                .foregroundColor(isDisabled ? Color.gray : Color.white)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .disabled(isDisabled)
        }
        .background(
            RoundedRectangle(cornerRadius: 10)
                .fill(Color(red: 0.05, green: 0.07, blue: 0.1).opacity(0.85))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .stroke(Color.white.opacity(0.12), lineWidth: 1)
        )
    }
}

struct CleanDarkSecureField: View {
    var placeholder: String
    @Binding var text: String

    var body: some View {
        ZStack(alignment: .leading) {
            if text.isEmpty {
                Text(placeholder)
                    .foregroundColor(Color(red: 0.4, green: 0.48, blue: 0.58))
                    .font(.system(size: 12.5))
                    .padding(.horizontal, 12)
            }
            SecureField("", text: $text)
                .textFieldStyle(.plain)
                .font(.system(size: 13))
                .foregroundColor(Color.white)
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
        }
        .background(
            RoundedRectangle(cornerRadius: 10)
                .fill(Color(red: 0.05, green: 0.07, blue: 0.1).opacity(0.85))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 10)
                .stroke(Color.white.opacity(0.12), lineWidth: 1)
        )
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

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Text(isEdit ? "Edit VPN Configuration" : "Add L2TP VPN Configuration")
                    .font(.system(size: 15, weight: .bold))
                    .foregroundColor(.white)
                Spacer()
                Button(action: { isPresented = false }) {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundColor(Color.gray.opacity(0.7))
                        .font(.system(size: 16))
                }
                .buttonStyle(.plain)
            }

            VStack(alignment: .leading, spacing: 5) {
                Text("Display name")
                    .font(.system(size: 11.5, weight: .semibold))
                    .foregroundColor(Color(red: 0.2, green: 0.85, blue: 0.95))
                CleanDarkTextField(placeholder: "Required", text: $name, isDisabled: isEdit)
            }

            VStack(alignment: .leading, spacing: 5) {
                Text("Server address")
                    .font(.system(size: 11.5, weight: .semibold))
                    .foregroundColor(Color(red: 0.2, green: 0.85, blue: 0.95))
                CleanDarkTextField(placeholder: "vpn.example.com or 1.2.3.4", text: $server)
            }

            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 5) {
                    Text("Account name")
                        .font(.system(size: 11.5, weight: .semibold))
                        .foregroundColor(Color(red: 0.2, green: 0.85, blue: 0.95))
                    CleanDarkTextField(placeholder: "Required", text: $user)
                }

                VStack(alignment: .leading, spacing: 5) {
                    Text("Password")
                        .font(.system(size: 11.5, weight: .semibold))
                        .foregroundColor(Color(red: 0.2, green: 0.85, blue: 0.95))
                    CleanDarkSecureField(placeholder: "Required", text: $password)
                }
            }

            VStack(alignment: .leading, spacing: 5) {
                Text("Shared secret")
                    .font(.system(size: 11.5, weight: .semibold))
                    .foregroundColor(Color(red: 0.2, green: 0.85, blue: 0.95))
                CleanDarkSecureField(placeholder: "Pre-shared key", text: $psk)
            }

            // Native macOS Switch Toggle for Send All Traffic
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text("Send all traffic over VPN")
                        .font(.system(size: 12, weight: .medium))
                        .foregroundColor(Color(red: 0.88, green: 0.92, blue: 0.96))
                    Text("Route all internet traffic through the VPN")
                        .font(.system(size: 10))
                        .foregroundColor(Color.gray)
                }
                Spacer()
                Toggle("", isOn: $isFullTunnel)
                    .toggleStyle(SwitchToggleStyle(tint: Color(red: 0.2, green: 0.85, blue: 0.95)))
                    .labelsHidden()
            }
            .padding(11)
            .background(
                RoundedRectangle(cornerRadius: 10)
                    .fill(Color.white.opacity(0.04))
            )

            HStack(spacing: 10) {
                Spacer()
                Button("Cancel") {
                    isPresented = false
                }
                .keyboardShortcut(.cancelAction)
                .buttonStyle(SecondaryButtonStyle())

                Button(isEdit ? "Save" : "Create") {
                    guard !name.trimmingCharacters(in: .whitespaces).isEmpty,
                          !server.trimmingCharacters(in: .whitespaces).isEmpty else { return }
                    
                    VPNManager.shared.saveProfile(
                        name: name.trimmingCharacters(in: .whitespaces),
                        server: server.trimmingCharacters(in: .whitespaces),
                        user: user.trimmingCharacters(in: .whitespaces),
                        psk: psk,
                        password: password,
                        isFullTunnel: isFullTunnel,
                        isNew: !isEdit
                    )
                    isPresented = false
                }
                .keyboardShortcut(.defaultAction)
                .buttonStyle(PrimaryButtonStyle())
            }
            .padding(.top, 8)
        }
        .padding(22)
        .frame(width: 380)
        .background(Color(red: 0.08, green: 0.1, blue: 0.14))
        .onAppear {
            if let p = initialProfile {
                name = p.name
                server = p.server
                user = p.username
                isFullTunnel = p.isFullTunnel
            }
        }
    }
}

// MARK: - Custom UI Styles

struct PrimaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 13, weight: .semibold))
            .foregroundColor(.black)
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
            .background(Color(red: 0.2, green: 0.85, blue: 0.95))
            .cornerRadius(9)
            .opacity(configuration.isPressed ? 0.8 : 1.0)
    }
}

struct SecondaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 13, weight: .medium))
            .foregroundColor(Color(red: 0.85, green: 0.88, blue: 0.92))
            .padding(.horizontal, 16)
            .padding(.vertical, 8)
            .background(Color.white.opacity(0.1))
            .cornerRadius(9)
            .opacity(configuration.isPressed ? 0.8 : 1.0)
    }
}

// MARK: - App Delegate & Menu Bar Setup

final class AppDelegate: NSObject, NSApplicationDelegate {
    static var shared: AppDelegate?
    var statusItem: NSStatusItem?
    var popover = NSPopover()
    var hostingView: NSHostingView<MacOSMenuBar>?

    func applicationDidFinishLaunching(_ notification: Notification) {
        AppDelegate.shared = self
        statusItem = NSStatusBar.system.statusItem(withLength: 28)
        
        // Host the live SwiftUI MacOSMenuBar component (with 2.0s linear rotation & pulse)
        let menuBarView = MacOSMenuBar()
        let hosting = NSHostingView(rootView: menuBarView)
        hosting.frame = NSRect(x: 0, y: 0, width: 28, height: 22)
        
        if let button = statusItem?.button {
            button.addSubview(hosting)
            hosting.autoresizingMask = [.width, .height]
            button.action = #selector(togglePopover(_:))
            button.target = self
        }
        self.hostingView = hosting

        popover.contentSize = NSSize(width: 370, height: 440)
        popover.behavior = .transient
        popover.contentViewController = NSHostingController(rootView: MenuBarPopupView())
    }

    @objc func togglePopover(_ sender: AnyObject?) {
        guard let button = statusItem?.button else { return }
        if popover.isShown {
            popover.performClose(sender)
        } else {
            MainActor.assumeIsolated {
                VPNManager.shared.syncFromDisk()
            }
            popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
            NSApp.activate(ignoringOtherApps: true)
        }
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.accessory)
_ = NSApplicationMain(CommandLine.argc, CommandLine.unsafeArgv)
`;

export const SwiftSourceViewer: React.FC = () => {
  const [copiedCode, setCopiedCode] = useState(false);
  const [copiedCmd, setCopiedCmd] = useState<string | null>(null);

  const curlAuto = 'curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install.sh | bash';
  const curlArm = 'curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install-arm64.sh | bash';
  const curlIntel = 'curl -fsSL https://raw.githubusercontent.com/TOMOSIA-VIETNAM/vpn/main/install-intel.sh | bash';

  const copyToClipboard = (text: string, id: string) => {
    navigator.clipboard.writeText(text);
    setCopiedCmd(id);
    setTimeout(() => setCopiedCmd(null), 2000);
  };

  return (
    <div id="swift-source-viewer" className="space-y-3.5 text-slate-200">
      {/* 1-Line Curl Installer Box */}
      <div className="p-4 rounded-xl bg-gradient-to-r from-[#0a263c] via-[#0e1e2d] to-[#121927] border border-cyan-500/40 shadow-lg space-y-3">
        <div className="flex items-center gap-2">
          <Terminal className="w-5 h-5 text-emerald-400 flex-shrink-0" />
          <span className="text-[13px] font-bold text-white tracking-wide">
            One-line install (downloads and installs both the VPN CLI and the menu bar app):
          </span>
        </div>

        {/* 3 Quick copy items */}
        <div className="space-y-2">
          {/* Universal Auto */}
          <div className="flex items-center justify-between gap-2 p-2.5 rounded-lg bg-black/40 border border-white/10">
            <div className="min-w-0 flex-1">
              <div className="text-[11px] font-medium text-cyan-300">Auto-detect (Apple Silicon & Intel):</div>
              <code className="text-[11.5px] font-mono text-emerald-300 truncate block select-all">{curlAuto}</code>
            </div>
            <button
              onClick={() => copyToClipboard(curlAuto, 'auto')}
              className="px-2.5 py-1.5 rounded-lg text-[11px] font-medium bg-cyan-600 hover:bg-cyan-500 text-white flex items-center gap-1 active:scale-95 transition-all flex-shrink-0"
            >
              {copiedCmd === 'auto' ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
              <span>{copiedCmd === 'auto' ? 'Copied' : 'Copy'}</span>
            </button>
          </div>

          {/* Apple Silicon */}
          <div className="flex items-center justify-between gap-2 p-2.5 rounded-lg bg-black/40 border border-white/10">
            <div className="min-w-0 flex-1">
              <div className="text-[11px] font-medium text-emerald-300">Apple Silicon only (M1/M2/M3/M4):</div>
              <code className="text-[11.5px] font-mono text-emerald-300 truncate block select-all">{curlArm}</code>
            </div>
            <button
              onClick={() => copyToClipboard(curlArm, 'arm')}
              className="px-2.5 py-1.5 rounded-lg text-[11px] font-medium bg-emerald-700 hover:bg-emerald-600 text-white flex items-center gap-1 active:scale-95 transition-all flex-shrink-0"
            >
              {copiedCmd === 'arm' ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
              <span>{copiedCmd === 'arm' ? 'Copied' : 'Copy'}</span>
            </button>
          </div>

          {/* Mac Intel */}
          <div className="flex items-center justify-between gap-2 p-2.5 rounded-lg bg-black/40 border border-white/10">
            <div className="min-w-0 flex-1">
              <div className="text-[11px] font-medium text-indigo-300">Intel Macs only (x86_64):</div>
              <code className="text-[11.5px] font-mono text-indigo-200 truncate block select-all">{curlIntel}</code>
            </div>
            <button
              onClick={() => copyToClipboard(curlIntel, 'intel')}
              className="px-2.5 py-1.5 rounded-lg text-[11px] font-medium bg-indigo-700 hover:bg-indigo-600 text-white flex items-center gap-1 active:scale-95 transition-all flex-shrink-0"
            >
              {copiedCmd === 'intel' ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}
              <span>{copiedCmd === 'intel' ? 'Copied' : 'Copy'}</span>
            </button>
          </div>
        </div>
      </div>

      {/* Single File Code Display */}
      <div className="rounded-xl bg-[#090d14] border border-white/10 overflow-hidden flex flex-col">
        <div className="px-4 py-2.5 bg-[#141a26] border-b border-white/10 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <FileCode className="w-4 h-4 text-cyan-400" />
            <span className="font-mono text-[13px] font-bold text-white">main.swift</span>
            <span className="text-[11px] px-2 py-0.5 rounded-full bg-emerald-500/20 text-emerald-300">
              Swift 6 · Xcode 26
            </span>
            <span className="text-[11px] text-slate-400">
              Illustrative snapshot — the real source is main.swift in the repository.
            </span>
          </div>

          <button
            onClick={() => {
              navigator.clipboard.writeText(MAIN_SWIFT_CODE);
              setCopiedCode(true);
              setTimeout(() => setCopiedCode(false), 2000);
            }}
            className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-[12px] font-medium bg-white/10 hover:bg-white/15 text-white transition-colors"
          >
            {copiedCode ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5" />}
            <span>{copiedCode ? 'Copied main.swift' : 'Copy all of main.swift'}</span>
          </button>
        </div>

        <div className="p-4 overflow-x-auto max-h-[460px] custom-scrollbar">
          <pre className="font-mono text-[12px] text-slate-200 leading-relaxed">
            <code>{MAIN_SWIFT_CODE}</code>
          </pre>
        </div>
      </div>
    </div>
  );
};
