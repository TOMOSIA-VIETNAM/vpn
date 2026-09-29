import Cocoa
import SwiftUI

// MARK: - Branding

enum AppBranding {
    // Bundled by build.sh into Contents/Resources/Logo.png so the popover header
    // and the .app icon (assets/AppIcon.icns) stay the same artwork.
    static let logo: NSImage? = {
        guard let path = Bundle.main.path(forResource: "Logo", ofType: "png") else { return nil }
        return NSImage(contentsOfFile: path)
    }()

    // Stamped into Info.plist by build.sh from the release tag; nil when run
    // outside a bundle (e.g. a bare swiftc build), so the footer omits it.
    static let version: String? = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
    /// The current year, read from the system clock each launch.
    static let copyright: String = "© \(Calendar.current.component(.year, from: Date())) TOMOSIA VIET NAM"
}

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
    var local_ip: String?
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

// MARK: - VPN Manager (Real CLI & File Sync)

@MainActor
final class VPNManager: ObservableObject {
    static let shared = VPNManager()

    @Published var profiles: [VPNProfileItem] = []
    @Published var activeProfileName: String?
    /// Tunnel MTU shared by every profile (`vpn mtu`); 1280 or 1400.
    @Published var mtu: Int = 1280
    /// Detailed per-packet logging for every connection (`vpn verbose`); off unless the user turns it on.
    @Published var verbose: Bool = false
    /// The daemon lost the tunnel and is re-establishing it by itself.
    @Published var isReconnecting: Bool = false
    /// Block traffic while a full-tunnel VPN reconnects (`vpn killswitch`); off by default.
    @Published var killSwitch: Bool = false
    /// One line explaining what is happening (and to the traffic) while reconnecting.
    @Published var reconnectNote: String?
    @Published var isConnected: Bool = false
    @Published var isConnecting: Bool = false
    @Published var currentPhase: String = "DISCONNECTED"
    @Published var currentIP: String = ""
    @Published var currentTunDevice: String = ""
    @Published var errorMessage: String?
    @Published var activeAlert: VPNAlertInfo?
    /// Feedback for profile edits/deletes; clears itself after a few seconds.
    @Published var notice: VPNNotice?
    /// The user turned the VPN off and the daemon has not finished tearing down yet.
    @Published var isDisconnecting: Bool = false
    private var noticeWorkItem: DispatchWorkItem?
    var onStatusChanged: ((String) -> Void)?
    private var pollTimer: Timer?
    private var retryWorkItem: DispatchWorkItem?
    private var retryCount = 0

    /// The phase the user just asked for, shown optimistically until the CLI's state file
    /// agrees. Without it the 1s poll read the not-yet-updated state.json and flipped the
    /// switch back for a tick (ON → OFF → ON), which is what looked like jank.
    private struct PendingIntent {
        var id: Int
        var phase: String
        var profile: String?
        var since: Date
        var timeout: TimeInterval
    }
    private var pendingIntent: PendingIntent?
    private var intentCounter = 0

    var linkState: LinkState {
        isConnecting ? .connecting : (isConnected ? .connected : .idle)
    }

    private let cli = "/usr/local/bin/vpn"

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

    /// Whether the popover is on screen. Only then (or while something is in flight) is a 1s
    /// refresh worth its wakeups; a menu bar app that just sits there should barely register
    /// in the battery report.
    private var popoverVisible = false
    private var repairedStaleDaemon = false

    func setPopoverVisible(_ visible: Bool) {
        popoverVisible = visible
        syncFromDisk()
        startPolling()
    }

    private func pollInterval() -> TimeInterval {
        if popoverVisible || pendingIntent != nil || currentPhase == "CONNECTING" || activeAlert != nil { return 1 }
        return currentPhase == "CONNECTED" ? 4 : 8
    }

    /// One-shot timer rescheduled after every tick so the interval can follow the state, with
    /// tolerance so the system can coalesce the wakeup with others.
    func startPolling() {
        pollTimer?.invalidate()
        let interval = pollInterval()
        let timer = Timer(timeInterval: interval, repeats: false) { [weak self] _ in
            Task { @MainActor in
                self?.syncFromDisk()
                self?.startPolling()
            }
        }
        timer.tolerance = interval * 0.3
        RunLoop.main.add(timer, forMode: .common)
        pollTimer = timer
    }

    /// Re-decodes a JSON file only when it changed on disk (a stat is far cheaper than a read
    /// plus decode, and nearly every poll finds both files untouched).
    private struct FileCache<T> { var stamp: Date?; var size: Int?; var value: T? }
    private var stateCache = FileCache<CLIState>(stamp: nil, size: nil, value: nil)
    private var configCache = FileCache<CLIConfig>(stamp: nil, size: nil, value: nil)

    private func loadJSON<T: Decodable>(_ url: URL, cache: inout FileCache<T>) -> T? {
        let attrs = try? FileManager.default.attributesOfItem(atPath: url.path)
        let stamp = attrs?[.modificationDate] as? Date
        let size = attrs?[.size] as? Int
        if let value = cache.value, stamp != nil, cache.stamp == stamp, cache.size == size { return value }
        guard let data = try? Data(contentsOf: url), let value = try? JSONDecoder().decode(T.self, from: data) else {
            cache = FileCache(stamp: nil, size: nil, value: nil)
            return nil
        }
        cache = FileCache(stamp: stamp, size: size, value: value)
        return value
    }

    /// @Published fires objectWillChange on every assignment, even of an equal value, so
    /// the 1s poll used to re-render the whole popover every second. Only write on change.
    private func update<T: Equatable>(_ keyPath: ReferenceWritableKeyPath<VPNManager, T>, _ value: T) {
        if self[keyPath: keyPath] != value {
            self[keyPath: keyPath] = value
        }
    }

    // Formatters are expensive to create and this runs on every poll.
    private static let isoFractional: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()
    private static let isoPlain: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    private static func parseDate(_ s: String?) -> Date? {
        guard let s = s else { return nil }
        return isoFractional.date(from: s) ?? isoPlain.date(from: s)
    }

    func syncFromDisk() {
        // 1. Read State (/var/run/vpn/state.json)
        var activeProf: String?
        var phase = "DISCONNECTED"
        var localIP = ""
        var tunDev = ""
        var failStage = ""
        var failDetail = ""
        var updatedAt: Date?
        var reconnecting = false
        var daemonGone = false

        if let st = loadJSON(stateURL, cache: &stateCache) {
            phase = st.phase ?? "DISCONNECTED"
            reconnecting = st.reconnecting ?? false
            // CONNECTED is written once and only rewritten by the daemon itself, so
            // if that process is gone (crashed, killed) the file would otherwise stay
            // green forever. Ignored while a connect/disconnect we just asked for is
            // still settling.
            if pendingIntent == nil, phase == "CONNECTED" || phase == "CONNECTING",
               let pid = st.pid, pid > 0, kill(pid_t(pid), 0) != 0, errno != EPERM {
                phase = "DISCONNECTED"
                reconnecting = false
                daemonGone = true
            }
            activeProf = st.profile
            localIP = st.local_ip ?? ""
            tunDev = st.tun_device ?? ""
            failStage = st.fail_stage ?? ""
            failDetail = st.fail_detail ?? ""
            updatedAt = Self.parseDate(st.updated_at)
        }

        if let intent = pendingIntent {
            let confirmed: Bool
            if intent.phase == "CONNECTING" {
                // A FAILED left over from an earlier attempt must not count as the answer.
                let freshFailure = phase == "FAILED" && (updatedAt.map { $0 >= intent.since.addingTimeInterval(-1) } ?? false)
                confirmed = phase == "CONNECTING" || phase == "CONNECTED" || freshFailure
            } else {
                confirmed = phase == "DISCONNECTED"
            }
            if confirmed || Date().timeIntervalSince(intent.since) > intent.timeout {
                pendingIntent = nil
            } else {
                phase = intent.phase
                activeProf = intent.profile ?? activeProf
            }
        }

        if phase == "FAILED" {
            applyFailure(stage: failStage, detail: failDetail)
            // A stale ("already logged in") failure makes applyFailure begin
            // a fresh CONNECTING intent right above, silently, instead of
            // showing an alert — re-apply the same masking the block at the
            // top of this function does, so the plain `update(\.currentPhase,
            // phase)` etc. below reflect that *within this same tick*
            // instead of briefly showing FAILED for the ~1s until the next
            // poll catches up (which is exactly the flash this exists to
            // avoid).
            if let intent = pendingIntent {
                phase = intent.phase
                activeProf = intent.profile ?? activeProf
            }
        } else if phase == "CONNECTED" {
            update(\.errorMessage, nil)
            update(\.activeAlert, nil)
            cancelAutoRetry()
        } else if phase == "CONNECTING" {
            update(\.errorMessage, nil)
            update(\.activeAlert, nil)
            // Deliberately not cancelAutoRetry() here: a silent stale-session
            // retry (see applyFailure) displays as CONNECTING too while it
            // waits out its backoff, and this branch runs on every 1s poll
            // tick — cancelling here would kill that backoff before it ever
            // gets to retry. connect()/disconnect()/toggleConnect() already
            // cancel any pending retry themselves when the user acts.
        }

        if daemonGone {
            update(\.errorMessage, "The VPN process stopped unexpectedly. Turn the VPN on again to restore protection.")
            update(\.activeAlert, VPNAlertInfo(
                kind: .processStopped,
                title: "VPN Stopped Unexpectedly",
                message: "The VPN process quit, so your traffic is no longer protected. Turn the VPN on again to reconnect.",
                detail: ""
            ))
            // A dead daemon can leave routes behind — with the kill switch, blocked ones. Clean up
            // once, in the background; `repair` refuses to touch a live connection.
            if !repairedStaleDaemon {
                repairedStaleDaemon = true
                let cli = self.cli
                operationQueue.async { Self.run(cli, ["repair"]) }
            }
        } else {
            repairedStaleDaemon = false
        }
        let fullTunnelActive = profiles.first(where: { $0.name == activeProf })?.isFullTunnel ?? true
        update(\.reconnectNote, (reconnecting && phase == "CONNECTING")
            ? (killSwitch && fullTunnelActive
                ? "Connection lost — reconnecting. Internet is blocked (kill switch) until it is back."
                : "Connection lost — reconnecting. Traffic is NOT protected until it is back.")
            : nil)
        update(\.isReconnecting, reconnecting && phase == "CONNECTING")
        let oldPhase = currentPhase
        update(\.currentPhase, phase)
        update(\.isConnected, phase == "CONNECTED")
        update(\.isConnecting, phase == "CONNECTING")
        update(\.isDisconnecting, pendingIntent?.phase == "DISCONNECTED")
        update(\.currentIP, localIP)
        update(\.currentTunDevice, tunDev)

        if oldPhase != phase {
            onStatusChanged?(phase)
        }

        // 2. Read Config (~/.config/vpn/config.json)
        if let cfg = loadJSON(configURL, cache: &configCache) {
            if activeProf == nil {
                activeProf = cfg.active_profile
            }
            update(\.activeProfileName, activeProf)
            update(\.mtu, cfg.mtu ?? 1280)
            update(\.verbose, cfg.verbose ?? false)
            update(\.killSwitch, cfg.kill_switch ?? false)

            var items: [VPNProfileItem] = []
            for (pName, pVal) in cfg.profiles ?? [:] {
                items.append(VPNProfileItem(
                    name: pName,
                    displayName: pVal.display_name ?? "",
                    server: pVal.server ?? "",
                    username: pVal.default_account ?? pVal.accounts?.keys.first ?? "",
                    isFullTunnel: pVal.full_tunnel ?? true,
                    isConnected: isConnected && activeProf == pName,
                    isConnecting: isConnecting && activeProf == pName
                ))
            }
            update(\.profiles, items.sorted { $0.title.lowercased() < $1.title.lowercased() })
        }
    }

    private func applyFailure(stage: String, detail: String) {
        if detail.contains("already logged in") || detail.contains("You are already logged in") {
            // Silent, automatic recovery: the app already knows exactly how
            // to fix this itself (disconnect + repair + reconnect — see
            // scheduleStaleSessionRetry), so there's nothing here for a
            // person to decide. No alert, no button — the UI just stays on
            // "Connecting..." for as long as this keeps happening. An
            // explicit OFF tap still works at any point (toggleConnect/
            // disconnect() both cancel this via cancelAutoRetry()).
            guard let profileName = activeProfileName else { return }
            _ = beginIntent(phase: "CONNECTING", profile: profileName, timeout: 60)
            scheduleStaleSessionRetry(profileName: profileName)
            return
        }

        let alert: VPNAlertInfo
        let message: String
        if detail.contains("authenticator response") {
            // The server accepted the login but could not prove it knows the
            // password (MS-CHAPv2 S= check) — an impersonation signal, not a
            // typo. Must precede the generic PPP_AUTH_FAILURE branch, which
            // would tell the user to re-enter their password.
            alert = VPNAlertInfo(
                kind: .generic,
                title: "Server Verification Failed",
                message: "The server could not prove its identity and may be an impostor. Do not re-enter your password — switch networks and contact your administrator.",
                detail: detail
            )
            message = "Server Verification Failed: the server may be an impostor."
        } else if stage == "PPP_AUTH_FAILURE" || detail.contains("CHAP authentication rejected") {
            alert = VPNAlertInfo(
                kind: .authFailed,
                title: "Authentication Failed",
                message: "PPP/CHAP authentication failed: wrong account name or password.",
                detail: detail.isEmpty ? "CHAP authentication rejected by peer" : detail
            )
            message = "Authentication Failed: wrong account name or password."
        } else if stage.contains("IKE") && detail.contains("no response") {
            // The server never answered at all — a network or server problem,
            // not a wrong secret (that only shows up after the server replies).
            alert = VPNAlertInfo(
                kind: .ikeFailed,
                title: "Server Not Responding",
                message: "The VPN server did not answer. This network may be blocking VPN traffic (UDP 500/4500), or the server is down — try another network.",
                detail: detail
            )
            message = "Server Not Responding: the VPN server did not answer."
        } else if stage.contains("IKE") && detail.contains("IKE_PROPOSAL_MISMATCH") {
            alert = VPNAlertInfo(
                kind: .ikeFailed,
                title: "Unsupported Server Settings",
                message: "The server accepts none of the encryption settings this client offers. Send the logs (vpn logs) to your administrator.",
                detail: detail
            )
            message = "Unsupported Server Settings: no common encryption proposal."
        } else if stage == "IKE_FAILED" || stage.contains("IKE") {
            alert = VPNAlertInfo(
                kind: .ikeFailed,
                title: "IKE Handshake Failed",
                message: "IPsec IKE handshake failed: check the shared secret.",
                detail: detail
            )
            message = "IKE Handshake Failed: check the shared secret."
        } else if stage == "ROUTE_FAILURE" {
            alert = VPNAlertInfo(
                kind: .routeFailed,
                title: "Routing Error",
                message: "Could not set up network routes. Run `vpn repair`, then try again.",
                detail: detail
            )
            message = "Routing Error: could not set up network routes."
        } else {
            alert = VPNAlertInfo(
                kind: .generic,
                title: stage.isEmpty ? "Connection Error" : stage,
                message: detail.isEmpty ? "Connection failed" : detail,
                detail: detail
            )
            message = "\(stage.isEmpty ? "Connection Error" : stage): \(detail)"
        }

        update(\.activeAlert, alert)
        update(\.errorMessage, message)
    }

    /// Schedules the next silent recovery attempt for a stale ("already
    /// logged in") server session, with exponential backoff (1s, 2s, 4s,
    /// 8s, capped at 15s) so repeated conflicts don't hammer the server
    /// while it's still releasing the old session — each new failure calls
    /// this again (via applyFailure), growing the delay, until it succeeds
    /// or cancelAutoRetry() stops it.
    private func scheduleStaleSessionRetry(profileName: String) {
        retryWorkItem?.cancel()
        retryCount += 1
        let delay = min(pow(2.0, Double(retryCount - 1)), 15.0)
        let work = DispatchWorkItem { [weak self] in
            self?.cleanupAndForceConnect(profileName: profileName, silent: true)
        }
        retryWorkItem = work
        DispatchQueue.main.asyncAfter(deadline: .now() + delay, execute: work)
    }

    func cancelAutoRetry() {
        retryWorkItem?.cancel()
        retryWorkItem = nil
        retryCount = 0
    }

    /// Coalesces rapid taps on the switch: N taps within `toggleDebounceInterval`
    /// of each other now produce at most one real `vpn connect`/`disconnect`
    /// call — the one matching the *last* tap, once tapping settles. Before
    /// this, every single tap fired its own real CLI invocation (a real
    /// negotiation attempt against the server), which is exactly what
    /// produced "already logged in" conflicts and visible state flicker
    /// under fast repeated ON/OFF/ON toggling. The optimistic UI still
    /// updates on every tap via beginIntent below, so the switch stays
    /// instantly responsive regardless of the debounce.
    private var toggleDebounce: DispatchWorkItem?
    private static let toggleDebounceInterval: TimeInterval = 0.5

    func toggleConnect(profile: VPNProfileItem) {
        let wantsOn = !(profile.isConnected || profile.isConnecting)
        let profileName = profile.name
        cancelAutoRetry()
        update(\.errorMessage, nil)

        let id = wantsOn
            ? beginIntent(phase: "CONNECTING", profile: profileName, timeout: 45)
            : beginIntent(phase: "DISCONNECTED", profile: nil, timeout: 10)

        toggleDebounce?.cancel()
        let work = DispatchWorkItem { [weak self] in
            if wantsOn {
                self?.performConnect(profileName: profileName, id: id)
            } else {
                self?.performDisconnect(id: id)
            }
        }
        toggleDebounce = work
        DispatchQueue.main.asyncAfter(deadline: .now() + Self.toggleDebounceInterval, execute: work)
    }

    /// 0ms optimistic UI update, held by a PendingIntent until the CLI catches up.
    private func beginIntent(phase: String, profile: String?, timeout: TimeInterval) -> Int {
        intentCounter += 1
        pendingIntent = PendingIntent(id: intentCounter, phase: phase, profile: profile, since: Date(), timeout: timeout)
        startPolling() // something is in flight: refresh fast until the CLI catches up

        update(\.currentPhase, phase)
        update(\.isConnecting, phase == "CONNECTING")
        update(\.isConnected, false)
        update(\.isDisconnecting, phase == "DISCONNECTED")
        update(\.activeAlert, nil)
        if let profile = profile {
            update(\.activeProfileName, profile)
        }
        update(\.profiles, profiles.map { p in
            var copy = p
            copy.isConnected = false
            copy.isConnecting = phase == "CONNECTING" && p.name == profile
            return copy
        })
        onStatusChanged?(phase)
        return intentCounter
    }

    /// Releases the intent once the CLI command it was waiting on has finished, so the
    /// next poll shows the real outcome instead of waiting for the timeout.
    private func endIntent(_ id: Int) {
        if pendingIntent?.id == id {
            pendingIntent = nil
            update(\.isDisconnecting, false)
        }
        syncFromDisk()
    }

    /// `secret`, when given, goes to the CLI's stdin — never into `args`: the argv of a
    /// running process is visible to every local user via `ps`, and the CLI's secret prompt
    /// reads one line from stdin when it isn't a terminal. An empty secret sends nothing, so
    /// the CLI sees EOF and refuses, exactly as it did for an empty `--psk`/`--password`.
    nonisolated private static func run(_ path: String, _ args: [String], secret: String? = nil) {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: path)
        p.arguments = args
        let input = Pipe()
        if secret != nil {
            p.standardInput = input
        }
        do {
            try p.run()
        } catch {
            return
        }
        if let secret = secret {
            if !secret.isEmpty {
                input.fileHandleForWriting.write(Data((secret + "\n").utf8))
            }
            try? input.fileHandleForWriting.close()
        }
        p.waitUntilExit()
    }

    /// Launches `vpn connect` without blocking the serial queue — the spawner only exits once
    /// the daemon reports success or failure, which can take the full connect timeout, and a
    /// toggle-off queued behind it would otherwise wait that long. `vpn connect` tears down any
    /// previous session itself, gracefully and by exact PID, before negotiating a new one (see
    /// engine.EnsureDisconnected on the Go side) — this no longer needs to disconnect first.
    nonisolated private static func launchConnect(cli: String, profileName: String, onExit: @escaping @Sendable () -> Void) {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: cli)
        task.arguments = ["connect", "--profile", profileName]
        task.terminationHandler = { _ in onExit() }
        do {
            try task.run()
        } catch {
            onExit()
        }
    }

    func connect(profileName: String) {
        toggleDebounce?.cancel()
        cancelAutoRetry()
        update(\.errorMessage, nil)
        let id = beginIntent(phase: "CONNECTING", profile: profileName, timeout: 45)
        performConnect(profileName: profileName, id: id)
    }

    /// The actual `vpn connect` invocation, given an intent already begun by
    /// the caller (connect() for an explicit/immediate call, or
    /// toggleConnect()'s debounced switch). Serialized on operationQueue so
    /// rapid off/on toggles can't interleave their CLI calls.
    private func performConnect(profileName: String, id: Int) {
        let cli = self.cli
        operationQueue.async {
            Self.launchConnect(cli: cli, profileName: profileName) {
                Task { @MainActor in self.endIntent(id) }
            }
        }
    }

    /// `silent` is true when this is the automatic stale-session recovery
    /// (see scheduleStaleSessionRetry) — no "cleaning up..." message, and
    /// deliberately *not* cancelAutoRetry() here: doing so would reset
    /// retryCount back to 0 on every attempt, defeating the backoff (each
    /// new failure schedules the next retry itself, via applyFailure).
    func cleanupAndForceConnect(profileName: String, silent: Bool = false) {
        let id = beginIntent(phase: "CONNECTING", profile: profileName, timeout: 45)
        if !silent {
            cancelAutoRetry()
            update(\.errorMessage, "Signing out the previous session and reconnecting...")
        }

        let cli = self.cli
        operationQueue.async {
            // `repair` refuses to run against a live connect process, so this
            // still needs an explicit disconnect first (unlike plain connect,
            // which handles that itself) — see engine.Repair's doc comment.
            Self.run(cli, ["disconnect"])
            Self.run(cli, ["repair"])
            // Give the server time to flush the RADIUS/L2TP session asynchronously without blocking the queue thread.
            DispatchQueue.global().asyncAfter(deadline: .now() + 0.8) {
                Self.launchConnect(cli: cli, profileName: profileName) {
                    Task { @MainActor in self.endIntent(id) }
                }
            }
        }
    }

    func disconnect() {
        toggleDebounce?.cancel()
        cancelAutoRetry()
        update(\.errorMessage, nil)
        let id = beginIntent(phase: "DISCONNECTED", profile: nil, timeout: 10)
        performDisconnect(id: id)
    }

    /// The actual `vpn disconnect` invocation — see performConnect's comment.
    private func performDisconnect(id: Int) {
        let cli = self.cli
        operationQueue.async {
            Self.run(cli, ["disconnect"])
            Task { @MainActor in self.endIntent(id) }
        }
    }

    /// Applies to every profile; takes effect on the next connect, not the running tunnel.
    func setMTU(_ value: Int) {
        guard value == 1280 || value == 1400 else { return }
        update(\.mtu, value)
        let cli = self.cli
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            Self.run(cli, ["mtu", String(value)])
            Task { @MainActor in self?.syncFromDisk() }
        }
    }

    /// Applies to every profile; takes effect on the next connect, not the running tunnel.
    func setVerbose(_ on: Bool) {
        update(\.verbose, on)
        let cli = self.cli
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            Self.run(cli, ["verbose", on ? "on" : "off"])
            Task { @MainActor in self?.syncFromDisk() }
        }
    }

    /// Applies to every full-tunnel profile; takes effect on the next connect.
    func setKillSwitch(_ on: Bool) {
        update(\.killSwitch, on)
        let cli = self.cli
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            Self.run(cli, ["killswitch", on ? "on" : "off"])
            Task { @MainActor in self?.syncFromDisk() }
        }
    }

    // MARK: Profile edits

    /// Editing or deleting a profile under a live tunnel would change the server, account
    /// or secrets it is running from, so both are refused until it is disconnected. The
    /// CLI enforces the same rule; this check just explains it before anything is run.
    func isLocked(_ profile: VPNProfileItem) -> Bool {
        profile.isConnected || profile.isConnecting
            || (activeProfileName == profile.name && (isConnected || isConnecting))
    }

    func showNotice(_ level: VPNNotice.Level, _ message: String) {
        noticeWorkItem?.cancel()
        update(\.notice, VPNNotice(level: level, message: message))
        let work = DispatchWorkItem { [weak self] in self?.update(\.notice, nil) }
        noticeWorkItem = work
        DispatchQueue.main.asyncAfter(deadline: .now() + (level == .error ? 8 : 4), execute: work)
    }

    func dismissNotice() {
        noticeWorkItem?.cancel()
        update(\.notice, nil)
    }

    func deleteProfile(name: String) {
        guard let profile = profiles.first(where: { $0.name == name }) else { return }
        if isLocked(profile) {
            showNotice(.error, "Can't delete \"\(profile.title)\" while it is connected. Turn the VPN off first.")
            return
        }
        let cli = self.cli
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            let result = Self.runCapturing(cli, ["profile", "remove", name])
            Task { @MainActor in
                self?.syncFromDisk()
                if result.ok {
                    self?.showNotice(.success, "Deleted \"\(profile.title)\".")
                } else {
                    self?.showNotice(.error, "Couldn't delete \"\(profile.title)\": \(result.message)")
                }
            }
        }
    }

    /// `name` is the profile key; `displayName` the label shown in the app (edits only).
    /// When editing, an empty `psk` / `password` means "keep the stored one" — the host,
    /// username and tunnel mode can change without re-entering either secret.
    func saveProfile(name: String, displayName: String? = nil, server: String, user: String, psk: String, password: String, isFullTunnel: Bool, isNew: Bool) {
        if !isNew, let profile = profiles.first(where: { $0.name == name }), isLocked(profile) {
            showNotice(.error, "Can't edit \"\(profile.title)\" while it is connected. Turn the VPN off first.")
            return
        }
        let label = (displayName?.isEmpty == false ? displayName : nil) ?? name
        let cli = self.cli
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            var failure: String?
            func step(_ args: [String], secret: String? = nil) {
                guard failure == nil else { return }
                let r = Self.runCapturing(cli, args, secret: secret)
                if !r.ok { failure = r.message }
            }

            if isNew {
                step(["profile", "add", name, "--server", server, "--full-tunnel=\(isFullTunnel)"], secret: psk)
                if !user.isEmpty {
                    step(["account", "add", name, user, "--default"], secret: password)
                }
            } else {
                // The label is independent of everything else in the form, so it goes first.
                if let displayName = displayName {
                    step(["profile", "rename", name] + (displayName.isEmpty ? [] : [displayName]))
                }
                var edit = ["profile", "edit", name, "--server", server, "--full-tunnel=\(isFullTunnel)"]
                if !user.isEmpty { edit += ["--user", user] }
                if !psk.isEmpty { edit.append("--set-psk") }
                step(edit, secret: psk.isEmpty ? nil : psk)
                if !password.isEmpty, !user.isEmpty {
                    step(["account", "add", name, user, "--default"], secret: password)
                }
            }

            Task { @MainActor in
                self?.syncFromDisk()
                if let failure {
                    self?.showNotice(.error, "Couldn't save \"\(label)\": \(failure)")
                } else {
                    self?.showNotice(.success, isNew ? "Added \"\(label)\"." : "Saved \"\(label)\".")
                }
            }
        }
    }

    /// Like `run`, but reports whether the CLI succeeded and its `Error:` line if not.
    nonisolated private static func runCapturing(_ path: String, _ args: [String], secret: String? = nil) -> (ok: Bool, message: String) {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: path)
        p.arguments = args
        let input = Pipe()
        let errPipe = Pipe()
        p.standardError = errPipe
        p.standardOutput = FileHandle.nullDevice
        if secret != nil {
            p.standardInput = input
        }
        do {
            try p.run()
        } catch {
            return (false, "the vpn command-line tool is not installed")
        }
        // Written on its own queue: this thread drains stderr below, and a child that
        // fills the stderr pipe before it has read stdin would otherwise deadlock both.
        if let secret = secret {
            DispatchQueue.global(qos: .userInitiated).async {
                if !secret.isEmpty {
                    input.fileHandleForWriting.write(Data((secret + "\n").utf8))
                }
                try? input.fileHandleForWriting.close()
            }
        }
        let errData = errPipe.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        guard p.terminationStatus != 0 else { return (true, "") }
        let text = String(decoding: errData, as: UTF8.self)
        let line = text.split(separator: "\n").last(where: { $0.hasPrefix("Error:") }) ?? text.split(separator: "\n").last
        let msg = line.map { String($0).replacingOccurrences(of: "Error: ", with: "") } ?? "exit status \(p.terminationStatus)"
        // An older `vpn` installed next to a newer app doesn't know the subcommand.
        if msg.hasPrefix("unknown `profile` subcommand") {
            return (false, "the installed vpn tool is older than this app. Reinstall the CLI and the app together.")
        }
        return (false, msg)
    }
}

// MARK: - Link State & Palette

enum LinkState: Equatable {
    case idle, connecting, connected
}

extension VPNProfileItem {
    var linkState: LinkState {
        isConnecting ? .connecting : (isConnected ? .connected : .idle)
    }
}

enum VPNColors {
    static let green = Color(red: 0.2, green: 0.9, blue: 0.55)
    static let greenBright = Color(red: 0.6, green: 1.0, blue: 0.8)
    static let amber = Color(red: 0.98, green: 0.62, blue: 0.1)
    static let amberBright = Color(red: 1.0, green: 0.9, blue: 0.62)
    static let switchOff = Color(red: 0.2, green: 0.25, blue: 0.33)
}

// MARK: - Animation Primitives
//
// Every looping effect below is driven by TimelineView and derives its phase from
// wall-clock time rather than `repeatForever` + `onAppear`. The old approach restarted
// or froze whenever SwiftUI re-created the view (every state change or poll re-render).

/// Outline for a card / badge: a static stroke whose color follows the link
/// state — faint when idle, amber while connecting, green once connected —
/// crossfading between states.
struct StatusBorder: View {
    var state: LinkState
    var cornerRadius: CGFloat
    var idleColor: Color = Color.white.opacity(0.08)

    var body: some View {
        RoundedRectangle(cornerRadius: cornerRadius)
            .strokeBorder(baseColor, lineWidth: 1)
            .animation(.easeInOut(duration: 0.35), value: state)
            .allowsHitTesting(false)
    }

    private var baseColor: Color {
        switch state {
        case .idle: return idleColor
        case .connecting: return VPNColors.amber.opacity(0.4)
        case .connected: return VPNColors.green.opacity(0.6)
        }
    }
}

/// Status dot with a soft expanding halo while `pulsing`.
struct StatusDot: View {
    var color: Color
    var size: CGFloat
    var pulsing: Bool = false
    var glowing: Bool = false

    var body: some View {
        ZStack {
            if pulsing {
                TimelineView(.animation(minimumInterval: 1.0 / 20.0)) { timeline in
                    let p = CGFloat((timeline.date.timeIntervalSinceReferenceDate / 1.4).truncatingRemainder(dividingBy: 1))
                    Circle()
                        .fill(color.opacity(0.45 * Double(1 - p)))
                        .frame(width: size, height: size)
                        .scaleEffect(1 + 1.6 * p)
                }
                .transition(.opacity)
            }
            Circle()
                .fill(color)
                .frame(width: size, height: size)
                .shadow(color: color.opacity(pulsing || glowing ? 0.85 : 0), radius: 4)
        }
        .animation(.easeInOut(duration: 0.3), value: pulsing)
    }
}

/// Switch with a spring-driven knob and crossfading tint, replacing the stock
/// SwitchToggleStyle whose tint snapped when flipping between amber and green.
struct GlowSwitch: View {
    var isOn: Bool
    var tint: Color
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                Capsule()
                    .fill(isOn ? tint : VPNColors.switchOff)
                    .shadow(color: isOn ? tint.opacity(0.5) : .clear, radius: 6)
                Circle()
                    .fill(Color.white)
                    .frame(width: 18, height: 18)
                    .shadow(color: Color.black.opacity(0.3), radius: 1.5, y: 0.5)
                    .offset(x: isOn ? 9 : -9)
            }
            .frame(width: 42, height: 24)
            .contentShape(Capsule())
            .animation(.spring(response: 0.3, dampingFraction: 0.75), value: isOn)
            .animation(.easeInOut(duration: 0.3), value: tint)
        }
        .buttonStyle(.plain)
        .focusable(false)
    }
}

// MARK: - MacOSMenuBar Swift Component (Live SwiftUI View in macOS Status Bar)

struct MacOSMenuBar: View {
    @ObservedObject var vpn = VPNManager.shared

    var body: some View {
        let state = vpn.linkState
        HStack(spacing: 0) {
            ZStack {
                StatusBorder(state: .idle, cornerRadius: 6, idleColor: Color.white.opacity(0.15))

                if let logo = AppBranding.logo {
                    Image(nsImage: logo)
                        .resizable()
                        .scaledToFit()
                        .frame(width: 16, height: 16)
                } else {
                    Image(systemName: "shield")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundColor(Color.white.opacity(0.85))
                }

                if state != .idle {
                    Circle()
                        .fill(state == .connected ? VPNColors.green : VPNColors.amber)
                        .frame(width: 5, height: 5)
                        .offset(x: 6, y: -6)
                        .transition(.scale.combined(with: .opacity))
                }
            }
            .frame(width: 22, height: 22)
            .animation(.easeInOut(duration: 0.3), value: state)
        }
        .frame(width: 28, height: 22)
        .contentShape(Rectangle())
        .onTapGesture {
            AppDelegate.shared?.togglePopover(nil)
        }
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
        Button(action: showNativeMenu) {
            Image(systemName: "ellipsis")
                .font(.system(size: 15, weight: .bold))
                .foregroundColor(Color(red: 0.6, green: 0.65, blue: 0.72).opacity(locked ? 0.6 : 1))
                .frame(width: 28, height: 28)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .focusable(false)
        .help(locked ? "Turn the VPN off to edit or delete this configuration" : "Edit or delete")
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

    /// Deleting also wipes the profile's saved secrets from Keychain, so ask first.
    @objc func deleteAction() {
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

// MARK: - Main Menu Bar Popup View

/// Per-kind look of the alert card: what it is called, its color and icon.
struct AlertStyle {
    var icon: String
    var tint: Color
    var badge: String

    init(kind: VPNAlertKind) {
        switch kind {
        case .authFailed:
            self = AlertStyle(icon: "lock.slash.fill", tint: Color(red: 1.0, green: 0.42, blue: 0.42), badge: "Credentials")
        case .ikeFailed:
            self = AlertStyle(icon: "network.slash", tint: Color(red: 1.0, green: 0.75, blue: 0.25), badge: "Server")
        case .routeFailed:
            self = AlertStyle(icon: "arrow.triangle.branch", tint: Color(red: 1.0, green: 0.75, blue: 0.25), badge: "Network")
        case .processStopped:
            self = AlertStyle(icon: "bolt.slash.fill", tint: Color(red: 1.0, green: 0.55, blue: 0.2), badge: "Stopped")
        case .sessionStale:
            self = AlertStyle(icon: "clock.arrow.circlepath", tint: Color.orange, badge: "Stale Session")
        case .generic:
            self = AlertStyle(icon: "exclamationmark.triangle.fill", tint: Color.yellow, badge: "Error")
        }
    }

    private init(icon: String, tint: Color, badge: String) {
        self.icon = icon; self.tint = tint; self.badge = badge
    }
}

struct AlertActionButton: View {
    var title: String
    var icon: String?
    var tint: Color
    var filled: Bool
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 4) {
                if let icon { Image(systemName: icon) }
                Text(title)
            }
            .font(.system(size: 11, weight: .semibold))
            .foregroundColor(filled ? .black : tint)
            .padding(.horizontal, filled ? 10 : 8)
            .padding(.vertical, 5)
            .background(RoundedRectangle(cornerRadius: 6).fill(filled ? tint : Color.clear))
        }
        .buttonStyle(.plain)
        .focusable(false)
    }
}

/// One-line result of a profile edit / delete.
struct NoticeBanner: View {
    var notice: VPNNotice
    var onDismiss: () -> Void

    private var tint: Color {
        switch notice.level {
        case .success: return VPNColors.green
        case .info: return Color(red: 0.2, green: 0.85, blue: 0.95)
        case .error: return Color(red: 1.0, green: 0.5, blue: 0.4)
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
        HStack(alignment: .center, spacing: 10) {
            Image(systemName: icon)
                .font(.system(size: 13, weight: .bold))
                .foregroundColor(tint)
            Text(notice.message)
                .font(.system(size: 11.5, weight: .medium))
                .foregroundColor(Color.white.opacity(0.92))
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button(action: onDismiss) {
                Image(systemName: "xmark")
                    .font(.system(size: 9, weight: .bold))
                    .foregroundColor(Color.gray)
            }
            .buttonStyle(.plain)
            .focusable(false)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .background(
            RoundedRectangle(cornerRadius: 10)
                .fill(tint.opacity(0.12))
                .overlay(RoundedRectangle(cornerRadius: 10).stroke(tint.opacity(0.4), lineWidth: 1))
        )
    }
}

struct ProfileCardRow: View {
    let profile: VPNProfileItem
    @ObservedObject var vpn: VPNManager
    var onEdit: () -> Void
    var onDelete: () -> Void

    var body: some View {
        let state = profile.linkState
        HStack(spacing: 12) {
            StatusDot(
                color: state == .connected ? VPNColors.green : (state == .connecting ? VPNColors.amber : Color.gray.opacity(0.6)),
                size: 9,
                pulsing: state == .connecting,
                glowing: state == .connected
            )

            // Profile Name & Subtitle
            VStack(alignment: .leading, spacing: 2) {
                Text(profile.title)
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundColor(state == .connected ? .white : (state == .connecting ? Color(red: 1.0, green: 0.9, blue: 0.7) : Color(red: 0.85, green: 0.88, blue: 0.92)))
                    .lineLimit(1)
                    .truncationMode(.tail)

                ZStack(alignment: .leading) {
                    if state == .connecting {
                        Text(vpn.isReconnecting ? "Reconnecting…" : "Connecting to server…")
                            .font(.system(size: 11, weight: .medium))
                            .foregroundColor(VPNColors.amber)
                            .lineLimit(1)
                            .transition(.opacity)
                    } else {
                        HStack(spacing: 4) {
                            Text(profile.server)
                                .font(.system(size: 11))
                                .foregroundColor(Color.gray)
                                .lineLimit(1)
                            if !profile.username.isEmpty {
                                Text("· \(profile.username)")
                                    .font(.system(size: 11))
                                    .foregroundColor(Color.gray.opacity(0.8))
                                    .lineLimit(1)
                            }
                        }
                        .transition(.opacity)
                    }
                }
            }

            Spacer(minLength: 8)

            GlowSwitch(
                isOn: state != .idle,
                tint: state == .connecting ? VPNColors.amber : VPNColors.green,
                action: { vpn.toggleConnect(profile: profile) }
            )

            // Context Menu Button
            CustomMenuButton(
                profileTitle: profile.title,
                locked: vpn.isLocked(profile),
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
                        state == .connected ? Color(red: 0.04, green: 0.18, blue: 0.12) :
                        (state == .connecting ? Color(red: 0.2, green: 0.12, blue: 0.04) : Color(red: 0.1, green: 0.12, blue: 0.16).opacity(0.85))
                    )
                    .shadow(
                        color: state == .connected ? VPNColors.green.opacity(0.3) : (state == .connecting ? VPNColors.amber.opacity(0.3) : .clear),
                        radius: 10
                    )

                StatusBorder(state: state, cornerRadius: 13)
            }
        )
        .animation(.easeInOut(duration: 0.3), value: state)
    }
}

struct MenuBarPopupView: View {
    @ObservedObject var vpn = VPNManager.shared
    @State private var showingAddModal = false
    @State private var showingSettings = false
    @State private var editingProfile: VPNProfileItem?
    var listHeight: CGFloat? = nil

    @ViewBuilder private var profileList: some View {
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
    }

    private var activeTitle: String? {
        vpn.profiles.first(where: { $0.name == vpn.activeProfileName })?.title
    }

    private var headerStatus: String {
        if vpn.isDisconnecting { return "Disconnecting…" }
        switch vpn.linkState {
        case .connected: return "Connected"
        case .connecting: return vpn.isReconnecting ? "Reconnecting…" : "Connecting…"
        case .idle: return vpn.activeAlert != nil ? "Connection Failed" : "Not Connected"
        }
    }

    private var headerColor: Color {
        if vpn.isDisconnecting { return VPNColors.amber }
        switch vpn.linkState {
        case .connected: return VPNColors.green
        case .connecting: return VPNColors.amber
        case .idle: return vpn.activeAlert != nil ? Color(red: 1.0, green: 0.45, blue: 0.45) : Color.gray
        }
    }

    private var headerDetail: String {
        if vpn.isDisconnecting { return "Restoring your network settings" }
        switch vpn.linkState {
        case .connected:
            return activeTitle.map { "Protected via \($0)" } ?? "Your traffic is protected"
        case .connecting:
            return activeTitle.map { "\(vpn.isReconnecting ? "Restoring" : "Connecting to") \($0)" } ?? "Establishing the tunnel"
        case .idle:
            return vpn.profiles.isEmpty ? "Add a configuration to get started" : "Your traffic is not protected"
        }
    }

    var body: some View {
        let state = vpn.linkState
        VStack(spacing: 0) {
            // Header Bar (Clean, NO Settings Icon)
            HStack(spacing: 12) {
                ZStack {
                    RoundedRectangle(cornerRadius: 12)
                        .fill(Color(red: 0.05, green: 0.16, blue: 0.22))
                        .shadow(color: Color(red: 0.08, green: 0.72, blue: 0.82).opacity(0.25), radius: 8)

                    StatusBorder(state: .idle, cornerRadius: 12,
                                 idleColor: Color(red: 0.12, green: 0.55, blue: 0.65).opacity(0.6))

                    if let logo = AppBranding.logo {
                        Image(nsImage: logo)
                            .resizable()
                            .scaledToFit()
                            .frame(width: 24, height: 24)
                    } else {
                        Image(systemName: "shield.fill")
                            .font(.system(size: 20))
                            .foregroundColor(Color(red: 0.2, green: 0.75, blue: 0.95))
                    }
                }
                .frame(width: 40, height: 40)

                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        Text("TOMOSIA VPN")
                            .font(.system(size: 16, weight: .bold))
                            .foregroundColor(.white)
                        StatusDot(
                            color: headerColor,
                            size: 7,
                            pulsing: state == .connecting || vpn.isDisconnecting,
                            glowing: state == .connected
                        )
                        Text(headerStatus)
                            .font(.system(size: 13, weight: .medium))
                            .foregroundColor(headerColor)
                            .id(headerStatus)
                            .transition(.opacity)
                    }
                    // Under the title: a one-line hint for what the current state means.
                    Text(headerDetail)
                        .font(.system(size: 11))
                        .foregroundColor(state == .connected ? VPNColors.green.opacity(0.75) : Color.gray)
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .id(headerDetail)
                        .transition(.opacity)
                }

                Spacer()
            }
            .padding(.horizontal, 16)
            .padding(.top, 16)
            .padding(.bottom, 12)

            Divider().background(Color.white.opacity(0.08))

            if let note = vpn.reconnectNote {
                Text(note)
                    .font(.system(size: 11, weight: .medium))
                    .foregroundColor(VPNColors.amberBright)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 8)
                    .background(Color(red: 0.22, green: 0.14, blue: 0.04))
                    .transition(.opacity)
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
                .focusable(false)
            }
            .padding(.horizontal, 16)
            .padding(.top, 14)
            .padding(.bottom, 8)

            // Profile List or Empty State
            // The window passes a fixed list height (room for about 5 profiles, scrolls
            // beyond that); the menu bar popover keeps its original content-sized list.
            if let listHeight {
                ScrollView(showsIndicators: false) { profileList }
                    .frame(height: listHeight)
            } else {
                profileList
            }

            // Outcome of the last edit / delete (saved, blocked while connected, failed).
            if let notice = vpn.notice {
                NoticeBanner(notice: notice, onDismiss: { vpn.dismissNotice() })
                    .padding(.horizontal, 16)
                    .padding(.top, 8)
                    .transition(.opacity)
            }

            // Error Notice / Alert Card. A stale ("already logged in") server
            // session never reaches here — it's recovered silently, with no
            // alert at all (see VPNManager.applyFailure) — so `.sessionStale`
            // no longer appears as a real activeAlert.kind.
            if let alert = vpn.activeAlert ?? (vpn.errorMessage != nil ? VPNAlertInfo(kind: .generic, title: "Connection Error", message: vpn.errorMessage ?? "", detail: "") : nil) {
                let style = AlertStyle(kind: alert.kind)
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 6) {
                        Image(systemName: style.icon)
                            .font(.system(size: 13, weight: .bold))
                            .foregroundColor(style.tint)

                        Text(alert.title)
                            .font(.system(size: 12, weight: .bold))
                            .foregroundColor(style.tint)
                            .lineLimit(2)

                        Spacer()

                        Text(style.badge)
                            .font(.system(size: 9.5, weight: .bold))
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Capsule().fill(style.tint.opacity(0.2)))
                            .foregroundColor(style.tint)
                    }

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
                    }

                    HStack(spacing: 8) {
                        let actProf = vpn.profiles.first(where: { $0.name == vpn.activeProfileName })
                        if alert.kind == .authFailed, let actProf {
                            AlertActionButton(title: "Update Password", icon: "pencil", tint: style.tint, filled: true) {
                                editingProfile = actProf
                            }
                        } else if alert.kind != .processStopped, let actProf {
                            AlertActionButton(title: "Try Again", icon: "arrow.clockwise", tint: style.tint, filled: true) {
                                vpn.connect(profileName: actProf.name)
                            }
                        }
                        AlertActionButton(title: "Dismiss", icon: nil, tint: Color.gray, filled: false) {
                            vpn.errorMessage = nil
                            vpn.activeAlert = nil
                        }
                    }
                    .padding(.top, 2)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 10)
                .background(
                    RoundedRectangle(cornerRadius: 10)
                        .fill(style.tint.opacity(0.12))
                        .overlay(RoundedRectangle(cornerRadius: 10).stroke(style.tint.opacity(0.4), lineWidth: 1))
                )
                .padding(.horizontal, 16)
                .padding(.top, 8)
            }

            Divider().background(Color.white.opacity(0.08)).padding(.top, 12)

            // Footer bar: settings (MTU, logging, kill switch) open in a sheet like the profile form.
            HStack {
                // Name once, not twice: the version is a pill, the copyright line carries the name.
                HStack(spacing: 8) {
                    if let version = AppBranding.version {
                        Text("v\(version)")
                            .font(.system(size: 10, weight: .semibold, design: .monospaced))
                            .foregroundColor(Color(red: 0.2, green: 0.85, blue: 0.95).opacity(0.9))
                            .padding(.horizontal, 7)
                            .padding(.vertical, 2)
                            .background(
                                Capsule()
                                    .fill(Color(red: 0.05, green: 0.16, blue: 0.22))
                                    .overlay(Capsule().stroke(Color(red: 0.12, green: 0.55, blue: 0.65).opacity(0.5), lineWidth: 1))
                            )
                    }
                    Text(AppBranding.copyright)
                        .font(.system(size: 10.5))
                        .foregroundColor(Color.gray.opacity(0.65))
                        .lineLimit(1)
                }
                .help(AppBranding.version.map { "TOMOSIA VPN \($0)" } ?? "TOMOSIA VPN")

                Spacer()

                Button(action: { showingSettings = true }) {
                    Image(systemName: "gearshape.fill")
                        .font(.system(size: 13))
                        .foregroundColor(Color(red: 0.7, green: 0.74, blue: 0.8))
                        .padding(5)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .focusable(false)
                .help("Settings")
                .padding(.trailing, 6)

                Button(action: { NSApplication.shared.terminate(nil) }) {
                    HStack(spacing: 5) {
                        Image(systemName: "rectangle.portrait.and.arrow.right")
                        Text("Quit").font(.system(size: 12, weight: .medium))
                        Text("⌘Q").font(.system(size: 10, weight: .semibold)).foregroundColor(Color.gray.opacity(0.6))
                    }
                    .foregroundColor(Color(red: 0.7, green: 0.74, blue: 0.8))
                }
                .buttonStyle(.plain)
                .focusable(false)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
        }
        .frame(width: 400)
        .background(Color(red: 0.07, green: 0.09, blue: 0.12))
        .animation(.easeInOut(duration: 0.3), value: state)
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

// MARK: - Settings Sheet

/// One settings card: title + explanation on the left, the control on the right —
/// the same look as the "Send all traffic over VPN" card in the profile form.
struct SettingCard<Control: View>: View {
    var title: String
    var detail: String
    @ViewBuilder var control: () -> Control

    var body: some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(.system(size: 12, weight: .medium))
                    .foregroundColor(Color(red: 0.88, green: 0.92, blue: 0.96))
                Text(detail)
                    .font(.system(size: 10))
                    .foregroundColor(Color.gray)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 8)
            control()
        }
        .padding(11)
        .background(
            RoundedRectangle(cornerRadius: 10)
                .fill(Color.white.opacity(0.04))
        )
    }
}

/// Global settings, applied by the CLI on the next connect.
struct SettingsSheet: View {
    @Binding var isPresented: Bool
    @ObservedObject var vpn = VPNManager.shared

    private let tint = Color(red: 0.2, green: 0.85, blue: 0.95)

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Text("Settings")
                    .font(.system(size: 15, weight: .bold))
                    .foregroundColor(.white)
                Spacer()
                Button(action: { isPresented = false }) {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundColor(Color.gray.opacity(0.7))
                        .font(.system(size: 16))
                }
                .buttonStyle(.plain)
                .focusable(false)
            }

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
                    .toggleStyle(SwitchToggleStyle(tint: tint))
                    .labelsHidden()
            }

            SettingCard(
                title: "Kill switch",
                detail: "If a full-tunnel VPN drops, block internet traffic until it reconnects instead of letting it leak. If stuck, turn the VPN off or run `vpn repair`."
            ) {
                Toggle("", isOn: Binding(get: { vpn.killSwitch }, set: { vpn.setKillSwitch($0) }))
                    .toggleStyle(SwitchToggleStyle(tint: tint))
                    .labelsHidden()
            }

            Text("Changes apply on the next connection.")
                .font(.system(size: 10))
                .foregroundColor(Color.gray)

            HStack {
                Spacer()
                Button("Done") { isPresented = false }
                    .keyboardShortcut(.defaultAction)
                    .buttonStyle(PrimaryButtonStyle())
                    .focusable(false)
            }
            .padding(.top, 2)
        }
        .padding(22)
        .frame(width: 380)
        .background(Color(red: 0.08, green: 0.1, blue: 0.14))
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

    private var secretPlaceholder: String { isEdit ? "Leave blank to keep" : "Required" }

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
                .focusable(false)
            }

            VStack(alignment: .leading, spacing: 5) {
                Text("Display name")
                    .font(.system(size: 11.5, weight: .semibold))
                    .foregroundColor(Color(red: 0.2, green: 0.85, blue: 0.95))
                CleanDarkTextField(placeholder: "Required", text: $name)
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
                    CleanDarkSecureField(placeholder: secretPlaceholder, text: $password)
                }
            }

            VStack(alignment: .leading, spacing: 5) {
                Text("Shared secret")
                    .font(.system(size: 11.5, weight: .semibold))
                    .foregroundColor(Color(red: 0.2, green: 0.85, blue: 0.95))
                CleanDarkSecureField(placeholder: secretPlaceholder, text: $psk)
            }

            if isEdit {
                Text("Password and shared secret stay saved in Keychain. Fill them in only to change them.")
                    .font(.system(size: 10))
                    .foregroundColor(Color.gray)
                    .fixedSize(horizontal: false, vertical: true)
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
                .buttonStyle(PrimaryButtonStyle())
                .disabled(!canSave)
                .opacity(canSave ? 1 : 0.45)
                .focusable(false)
            }
            .padding(.top, 8)
        }
        .padding(22)
        .frame(width: 380)
        .background(Color(red: 0.08, green: 0.1, blue: 0.14))
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

// MARK: - CLI Installer (first launch after a drag-to-Applications install)

/// The app drives `/usr/local/bin/vpn`, which has to be setuid-root. A DMG install has no
/// installer step, so the CLI ships inside the app bundle and is installed here, once, after
/// asking the user (macOS shows its own administrator password prompt).
enum CLIInstaller {
    static let installPath = "/usr/local/bin/vpn"

    private static var bundledPath: String? {
        Bundle.main.path(forResource: "vpn", ofType: nil)
    }

    /// `vpn version` prints "vpn v1.2.3"; returns "v1.2.3".
    private static func version(of path: String) -> String? {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: path)
        p.arguments = ["version"]
        let out = Pipe()
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        guard (try? p.run()) != nil else { return nil }
        let data = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        guard p.terminationStatus == 0 else { return nil }
        return String(decoding: data, as: UTF8.self)
            .split(separator: " ").last.map { String($0).trimmingCharacters(in: .whitespacesAndNewlines) }
    }

    private static func numbers(_ v: String) -> [Int]? {
        let parts = v.trimmingCharacters(in: CharacterSet(charactersIn: "v")).split(separator: ".").map { Int($0) }
        return parts.contains(where: { $0 == nil }) || parts.isEmpty ? nil : parts.map { $0! }
    }

    /// Missing, or older than the copy in the app. A newer installed one (after `vpn update`)
    /// or a non-numeric dev build is left alone.
    private static func reason(bundled: String) -> String? {
        guard FileManager.default.isExecutableFile(atPath: installPath), let installed = version(of: installPath) else {
            return "missing"
        }
        guard let have = numbers(installed), let want = version(of: bundled).flatMap(numbers) else { return nil }
        for i in 0..<max(have.count, want.count) {
            let a = i < have.count ? have[i] : 0, b = i < want.count ? want[i] : 0
            if a != b { return a < b ? "outdated" : nil }
        }
        return nil
    }

    static func checkAtLaunch() {
        guard let bundled = bundledPath else { return }
        DispatchQueue.global(qos: .utility).async {
            guard let why = reason(bundled: bundled) else { return }
            DispatchQueue.main.async { promptAndInstall(bundled: bundled, missing: why == "missing") }
        }
    }

    private static func promptAndInstall(bundled: String, missing: Bool) {
        NSApp.activate(ignoringOtherApps: true)
        let alert = NSAlert()
        alert.messageText = missing ? "Finish setting up TOMOSIA VPN" : "Update the TOMOSIA VPN network helper"
        alert.informativeText = "TOMOSIA VPN needs its network helper (\(installPath)) to open VPN tunnels. Installing it needs your administrator password — macOS will ask for it next."
        alert.addButton(withTitle: missing ? "Install" : "Update")
        alert.addButton(withTitle: "Not Now")
        guard alert.runModal() == .alertFirstButtonReturn else {
            MainActor.assumeIsolated {
                VPNManager.shared.showNotice(.error, "The network helper isn't installed, so VPN can't connect yet. Relaunch TOMOSIA VPN to set it up.")
            }
            return
        }
        DispatchQueue.global(qos: .userInitiated).async {
            let error = install(bundled: bundled)
            DispatchQueue.main.async {
                MainActor.assumeIsolated {
                    if let error {
                        VPNManager.shared.showNotice(.error, "Couldn't install the network helper: \(error)")
                    } else {
                        VPNManager.shared.showNotice(.success, "Network helper installed. You can connect now.")
                        VPNManager.shared.syncFromDisk()
                    }
                }
            }
        }
    }

    private static func shellQuote(_ s: String) -> String { "'" + s.replacingOccurrences(of: "'", with: "'\\''") + "'" }

    private static func sha256(_ path: String) -> String? {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/shasum")
        p.arguments = ["-a", "256", path]
        let out = Pipe()
        p.standardOutput = out
        guard (try? p.run()) != nil else { return nil }
        let data = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        return String(decoding: data, as: UTF8.self).split(separator: " ").first.map(String.init)
    }

    /// Returns nil on success, else a short reason. The binary is first copied into a
    /// root-owned staging directory and re-checked against the hash taken before the password
    /// prompt, so it can't be swapped in the (user-writable) app bundle in between.
    private static func install(bundled: String) -> String? {
        guard let digest = sha256(bundled) else { return "can't read the bundled tool" }
        let uid = getuid()
        let script = [
            "set -e",
            "STAGE=$(/usr/bin/mktemp -d /tmp/tmsvpn-install.XXXXXX)",
            "trap '/bin/rm -rf \"$STAGE\"' EXIT",
            "/usr/bin/install -o root -g wheel -m 0755 \(shellQuote(bundled)) \"$STAGE/vpn\"",
            "[ \"$(/usr/bin/shasum -a 256 \"$STAGE/vpn\" | /usr/bin/awk '{print $1}')\" = \(shellQuote(digest)) ]",
            "/bin/mkdir -p /usr/local/bin",
            "/usr/bin/install -o root -g wheel -m 4755 \"$STAGE/vpn\" \(installPath)",
            "echo \(uid) > /etc/vpn-owner-uid",
            "/usr/sbin/chown root:wheel /etc/vpn-owner-uid",
            "/bin/chmod 600 /etc/vpn-owner-uid",
            "/bin/chmod 755 /var/run/vpn 2>/dev/null || true",
        ].joined(separator: "; ")
        let escaped = script.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/osascript")
        p.arguments = ["-e", "do shell script \"\(escaped)\" with administrator privileges"]
        let err = Pipe()
        p.standardError = err
        p.standardOutput = FileHandle.nullDevice
        guard (try? p.run()) != nil else { return "can't ask for administrator rights" }
        let data = err.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        if p.terminationStatus == 0 { return nil }
        let text = String(decoding: data, as: UTF8.self)
        return text.contains("-128") ? "cancelled" : (text.split(separator: "\n").last.map(String.init) ?? "installation failed")
    }
}

// MARK: - App Delegate & Menu Bar Setup

final class AppDelegate: NSObject, NSApplicationDelegate, NSPopoverDelegate, NSWindowDelegate {
    static var shared: AppDelegate?
    var statusItem: NSStatusItem?
    var popover = NSPopover()
    var mainWindow: NSWindow?
    var hostingView: NSHostingView<MacOSMenuBar>?

    func applicationDidFinishLaunching(_ notification: Notification) {
        AppDelegate.shared = self
        installEditMenu()
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

        popover.behavior = .transient
        popover.delegate = self
        // The UI is drawn for a dark surface (white text, hand-picked dark fills).
        // Pin the popover to Dark Aqua so system controls (dividers, text fields,
        // sheets, the popover arrow) look the same on every Mac instead of
        // following each machine's Light/Dark setting.
        popover.appearance = NSAppearance(named: .darkAqua)
        let popupController = NSHostingController(rootView: MenuBarPopupView())
        popover.contentViewController = popupController
        // Size to the view's actual content first, same reasoning as showMainWindow()
        // below: a guessed contentSize (e.g. 440) that doesn't match what SwiftUI
        // actually lays out (e.g. 334 for the empty state) gets corrected by
        // NSHostingController *after* the popover is shown, and NSPopover keeps the
        // bottom edge fixed while shrinking — dropping the popover away from the
        // status item instead of staying flush against it.
        popover.contentSize = popupController.view.fittingSize

        // Opened from Finder/Launchpad → show a regular window.
        showMainWindow()
        CLIInstaller.checkAtLaunch()
    }

    // Clicking the app icon again (Launchpad, Finder, Dock) while it is running
    // in the menu bar brings the window back.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        showMainWindow()
        return true
    }

    // Closing the window must not quit: the app carries on in the menu bar.
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    func applicationWillTerminate(_ notification: Notification) {
        // When quitting the app, gracefully disconnect and restore routes if connected
        let isConnected = MainActor.assumeIsolated { VPNManager.shared.isConnected }
        if isConnected {
            let p = Process()
            p.executableURL = URL(fileURLWithPath: "/usr/local/bin/vpn")
            p.arguments = ["disconnect"]
            try? p.run()
            p.waitUntilExit()
        }
    }

    func showMainWindow() {
        if popover.isShown { popover.performClose(nil) }
        if mainWindow == nil {
            let controller = NSHostingController(rootView: MenuBarPopupView(listHeight: 380))
            let window = NSWindow(contentViewController: controller)
            window.title = "TOMOSIA VPN"
            window.styleMask = [.titled, .closable, .miniaturizable]
            window.appearance = NSAppearance(named: .darkAqua)
            window.isReleasedWhenClosed = false
            window.delegate = self
            // Size the window to its content first: center() uses the current frame,
            // and SwiftUI resizes the hosted view afterwards, which left the window
            // hanging from the top of the screen next to the status bar.
            window.setContentSize(controller.view.fittingSize)
            // NSWindow.center() deliberately sits above true center; place it exactly.
            let placeCentered = { [weak window] in
                guard let window, let area = (window.screen ?? NSScreen.main)?.visibleFrame else { return }
                window.setFrameOrigin(NSPoint(x: area.midX - window.frame.width / 2,
                                              y: area.midY - window.frame.height / 2))
            }
            placeCentered()
            DispatchQueue.main.async(execute: placeCentered)
            mainWindow = window
        }
        // A regular app gets a Dock icon and a menu bar while its window is open.
        NSApp.setActivationPolicy(.regular)
        mainWindow?.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        MainActor.assumeIsolated { VPNManager.shared.setPopoverVisible(true) }
    }

    func windowWillClose(_ notification: Notification) {
        // Back to a menu-bar-only app: no Dock icon, status item stays.
        NSApp.setActivationPolicy(.accessory)
        MainActor.assumeIsolated { VPNManager.shared.setPopoverVisible(false) }
    }

    // A menu-bar-only (accessory) app never shows this in the UI — there's
    // no menu bar to show it in — but AppKit still needs a real Edit menu
    // with the standard cut:/copy:/paste:/selectAll: selectors and their
    // ⌘X/⌘C/⌘V/⌘A key equivalents to route those shortcuts (and enable
    // "Paste" in a text field's right-click menu) at all. Without this,
    // NSApp.mainMenu is nil and every text field in the Add/Edit sheets
    // can be typed into but never pasted into.
    private func installEditMenu() {
        let mainMenu = NSMenu()

        let appMenuItem = NSMenuItem()
        mainMenu.addItem(appMenuItem)
        let appMenu = NSMenu()
        appMenuItem.submenu = appMenu
        appMenu.addItem(NSMenuItem(title: "Quit", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q"))

        let editMenuItem = NSMenuItem()
        mainMenu.addItem(editMenuItem)
        let editMenu = NSMenu(title: "Edit")
        editMenuItem.submenu = editMenu
        editMenu.addItem(NSMenuItem(title: "Undo", action: Selector(("undo:")), keyEquivalent: "z"))
        let redo = NSMenuItem(title: "Redo", action: Selector(("redo:")), keyEquivalent: "z")
        redo.keyEquivalentModifierMask = [.command, .shift]
        editMenu.addItem(redo)
        editMenu.addItem(NSMenuItem.separator())
        editMenu.addItem(NSMenuItem(title: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x"))
        editMenu.addItem(NSMenuItem(title: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c"))
        editMenu.addItem(NSMenuItem(title: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v"))
        editMenu.addItem(NSMenuItem(title: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a"))

        NSApp.mainMenu = mainMenu
    }

    func popoverDidShow(_ notification: Notification) {
        MainActor.assumeIsolated { VPNManager.shared.setPopoverVisible(true) }
    }

    func popoverDidClose(_ notification: Notification) {
        MainActor.assumeIsolated { VPNManager.shared.setPopoverVisible(false) }
    }

    @objc func togglePopover(_ sender: AnyObject?) {
        guard let button = statusItem?.button else { return }
        if popover.isShown {
            popover.performClose(sender)
        } else if mainWindow?.isVisible == true {
            mainWindow?.makeKeyAndOrderFront(nil)
            NSApp.activate(ignoringOtherApps: true)
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
