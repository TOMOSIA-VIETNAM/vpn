import Cocoa
import Combine
import Network
import SwiftUI
import UserNotifications

// MARK: - Models for CLI Config and State

struct CLIAccount: Codable {
    var username: String?
}

struct CLIProfile: Codable {
    var display_name: String?
    var server: String?
    var server_id: String?
    var full_tunnel: Bool?
    var default_account: String?
    var accounts: [String: CLIAccount]?
}

struct CLIConfig: Codable {
    var mtu: Int?
    var verbose: Bool?
    var kill_switch: Bool?
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
    var fail_stage: String?
    var fail_detail: String?
    var updated_at: String?
    var reconnecting: Bool?
}

struct VPNProfileItem: Identifiable, Hashable {
    var id: String { name }
    /// The profile's key: CLI arguments and Keychain entries use it, so it never changes.
    var name: String
    /// What the user named it; empty means "same as the key".
    var displayName: String = ""
    var title: String { displayName.isEmpty ? name : displayName }
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
    case processStopped
    /// A live connection ended without the user turning it off in this app.
    case connectionLost
    case generic
}

/// A short, non-error outcome of something the user did (saved, blocked, removed).
struct VPNNotice: Equatable {
    enum Level { case success, info, error }
    var level: Level
    var message: String
}

struct VPNAlertInfo: Equatable {
    var kind: VPNAlertKind
    var title: String
    var message: String
    var detail: String
}
