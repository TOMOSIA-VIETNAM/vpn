import Cocoa
import Combine
import Network
import SwiftUI
import UserNotifications

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
    /// Refuse legacy algorithms (3DES, MD5, DH under 2048 bits) (`vpn strict`); off by default.
    @Published var strictCrypto: Bool = false
    /// Privacy or security caveats about the live connection, from the CLI's state file
    /// (no DNS pushed, weak encryption negotiated).
    @Published var warnings: [String] = []
    /// One line explaining what is happening (and to the traffic) while reconnecting.
    @Published var reconnectNote: String?
    @Published var isConnected: Bool = false
    @Published var isConnecting: Bool = false
    @Published var currentPhase: String = "DISCONNECTED"
    /// This Mac's address as the internet sees it, looked up while connected (see
    /// refreshPublicIP). Empty until the first lookup answers.
    @Published var publicIP: String = ""
    private var publicIPCheckedAt: Date?
    private var publicIPLookup: URLSessionDataTask?
    @Published var errorMessage: String?
    @Published var activeAlert: VPNAlertInfo?
    /// Feedback for profile edits/deletes; clears itself after a few seconds.
    @Published var notice: VPNNotice?
    /// The user turned the VPN off and the daemon has not finished tearing down yet.
    @Published var isDisconnecting: Bool = false
    /// Set when a live connection drops on its own (not turned off in this app); cleared once
    /// it is back, when the user acts, or when the alert is dismissed.
    @Published var connectionLost: Bool = false
    /// Whether this Mac has any usable network path (Wi-Fi, Ethernet…), from NWPathMonitor.
    @Published var networkOnline: Bool = true
    private let pathMonitor = NWPathMonitor()
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
        pathMonitor.pathUpdateHandler = { path in
            let online = path.status == .satisfied
            DispatchQueue.main.async {
                MainActor.assumeIsolated {
                    let vpn = VPNManager.shared
                    vpn.update(\.networkOnline, online)
                    // Refresh now instead of on the next (slow) poll tick: the wording of a
                    // reconnect depends on whether there is a network at all.
                    vpn.syncFromDisk()
                    vpn.refreshPublicIP()
                }
            }
        }
        pathMonitor.start(queue: .global(qos: .utility))
    }

    /// Whether the status item shows the error badge ("!"). It marks a problem that is still
    /// there: a failure alert, or a dropped connection that is reconnecting or has no network
    /// under it. Once the network is back and the VPN simply is off, the icon shows that
    /// plainly; the popover still explains the drop.
    var showsErrorBadge: Bool {
        guard linkState != .connected else { return false }
        if let kind = activeAlert?.kind, kind != .connectionLost { return true }
        return connectionLost && (linkState == .connecting || !networkOnline)
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
        if visible && isConnected { refreshPublicIP(maxAge: 60) }
    }

    private static let publicIPLookupKey = "publicIPLookup"

    /// The one request this app makes on its own (to Cloudflare); on by default, user can turn it off.
    static var publicIPLookupEnabled: Bool {
        get { UserDefaults.standard.object(forKey: publicIPLookupKey) as? Bool ?? true }
        set { UserDefaults.standard.set(newValue, forKey: publicIPLookupKey) }
    }

    /// Asks Cloudflare's trace endpoint which address our traffic leaves from: the VPN
    /// server's with "Send all traffic", the local network's otherwise. Skipped when the
    /// last answer is younger than `maxAge`; a failed lookup is retried once.
    func refreshPublicIP(maxAge: TimeInterval = 0, retry: Bool = true) {
        guard isConnected, Self.publicIPLookupEnabled else { return }
        if let at = publicIPCheckedAt, Date().timeIntervalSince(at) < maxAge, !publicIP.isEmpty { return }
        publicIPLookup?.cancel()
        var request = URLRequest(url: URL(string: "https://1.1.1.1/cdn-cgi/trace")!, timeoutInterval: 6)
        request.cachePolicy = .reloadIgnoringLocalCacheData
        let task = URLSession(configuration: .ephemeral).dataTask(with: request) { data, _, error in
            let ip = data.flatMap { String(data: $0, encoding: .utf8) }?
                .split(separator: "\n")
                .first { $0.hasPrefix("ip=") }
                .map { String($0.dropFirst(3)) }
            let cancelled = (error as? URLError)?.code == .cancelled
            DispatchQueue.main.async {
                MainActor.assumeIsolated {
                    let vpn = VPNManager.shared
                    guard vpn.isConnected, !cancelled else { return }
                    if let ip {
                        vpn.update(\.publicIP, ip)
                        vpn.publicIPCheckedAt = Date()
                    } else if retry {
                        DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
                            MainActor.assumeIsolated { VPNManager.shared.refreshPublicIP(retry: false) }
                        }
                    }
                }
            }
        }
        publicIPLookup = task
        task.resume()
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
        var failStage = ""
        var failDetail = ""
        var updatedAt: Date?
        var reconnecting = false
        var daemonGone = false

        if let st = loadJSON(stateURL, cache: &stateCache) {
            phase = st.phase ?? "DISCONNECTED"
            reconnecting = st.reconnecting ?? false
            update(\.warnings, phase == "CONNECTED" ? (st.warnings ?? []) : [])
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
        let lostWhat = !networkOnline
            ? "No network — the VPN reconnects once this Mac is back online."
            : "Connection lost — reconnecting."
        update(\.reconnectNote, (reconnecting && phase == "CONNECTING")
            ? (killSwitch && fullTunnelActive
                ? "\(lostWhat) Internet is blocked (kill switch) until it is back."
                : "\(lostWhat) Traffic is NOT protected until it is back.")
            : nil)
        update(\.isReconnecting, reconnecting && phase == "CONNECTING")
        let oldPhase = currentPhase
        update(\.currentPhase, phase)
        update(\.isConnected, phase == "CONNECTED")
        update(\.isConnecting, phase == "CONNECTING")
        update(\.isDisconnecting, pendingIntent?.phase == "DISCONNECTED")
        if phase != "CONNECTED" {
            publicIPLookup?.cancel()
            update(\.publicIP, "")
            publicIPCheckedAt = nil
        }

        if oldPhase != phase {
            noteLinkChange(from: oldPhase, to: phase, profile: activeProf ?? activeProfileName)
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
            update(\.strictCrypto, cfg.strict_crypto ?? false)

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

    /// Makes a connection that drops on its own visible: an alert card when nothing else
    /// explains it, the status item error badge, and a system notification. Changes the user makes in this app go through beginIntent, which moves
    /// currentPhase itself, so they never reach here as CONNECTED → something else.
    private func noteLinkChange(from old: String, to new: String, profile: String?) {
        let title = profiles.first(where: { $0.name == profile })?.title ?? profile ?? "VPN"
        if old == "CONNECTED" && new != "CONNECTED" {
            update(\.connectionLost, true)
            if new == "DISCONNECTED" && activeAlert == nil {
                update(\.activeAlert, VPNAlertInfo(
                    kind: .connectionLost,
                    title: "VPN Disconnected",
                    message: "\(title) was disconnected outside this app or the connection dropped. Your traffic is no longer protected.",
                    detail: ""
                ))
            }
            let body = new == "CONNECTING"
                ? (reconnectNote ?? "Reconnecting to \(title)…")
                : (activeAlert?.message ?? "Your traffic is no longer protected.")
            ConnectionNotifier.post(title: "VPN connection lost", body: body)
        } else if new == "CONNECTED" {
            if connectionLost {
                ConnectionNotifier.post(title: "VPN reconnected", body: "Protected via \(title) again.")
            }
            update(\.connectionLost, false)
            // Routes and DNS have just changed; the address the internet sees follows them.
            refreshPublicIP()
        }
    }

    /// Dismiss button of the alert card.
    func dismissAlert() {
        update(\.errorMessage, nil)
        update(\.activeAlert, nil)
        update(\.connectionLost, false)
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
        update(\.connectionLost, false)
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

    /// Applies to every profile; takes effect on the next connect.
    func setStrictCrypto(_ on: Bool) {
        update(\.strictCrypto, on)
        let cli = self.cli
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            Self.run(cli, ["strict", on ? "on" : "off"])
            Task { @MainActor in self?.syncFromDisk() }
        }
    }

    /// Manual `vpn repair`: puts back the routes and DNS a crashed connection (or the kill
    /// switch holding traffic) left behind. The CLI refuses while a connection is live.
    func restoreNetwork() {
        let cli = self.cli
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            Self.run(cli, ["repair"])
            Task { @MainActor in
                self?.syncFromDisk()
                self?.showNotice(.success, "Network settings restored.")
            }
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
