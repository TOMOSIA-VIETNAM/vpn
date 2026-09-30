import Cocoa
import Combine
import Network
import SwiftUI
import UserNotifications

// MARK: - Branding

enum AppBranding {
    static let name = "TOMOSIA VPN"

    // All artwork is rendered by assets/render-icons.swift from one vector mark and
    // bundled by build.sh into Contents/Resources, so the .app icon, the popover
    // header and the status item always show the same logo.
    static let logo: NSImage? = Bundle.main.image(forResource: "Logo")

    /// Status item images: outline mark while disconnected, solid mark while connected.
    /// Templates, so macOS tints them to match the menu bar like system items.
    static let menuBarIdle: NSImage = template("menubar-idle", fallback: "shield")
    static let menuBarConnected: NSImage = template("menubar-connected", fallback: "shield.fill")

    // Stamped into Info.plist by build.sh from the release tag; nil when run
    // outside a bundle (e.g. a bare swiftc build), so the footer omits it.
    static let version: String? = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
    /// The current year, read from the system clock each launch.
    static let copyright: String = "© \(Calendar.current.component(.year, from: Date())) TOMOSIA VIET NAM"

    /// Status item mark with a colored badge at its lower right, like a system item's
    /// indicator: a green check while connected, an orange "!" while there is a problem.
    /// The mark itself is tinted white or black to match the menu bar at draw time (a template
    /// image cannot carry a colored badge), and a notch is cut out around the badge.
    enum MenuBarBadge { case connected, alert }

    /// Width of the status item. Wider than the 18 pt mark so the badge can hang off its
    /// lower right corner instead of sitting on top of the logo.
    static let menuBarWidth: CGFloat = 28

    static func menuBarImage(base: NSImage, badge: MenuBarBadge) -> NSImage {
        let markSide = base.size.height
        let size = NSSize(width: menuBarWidth, height: markSide)
        let image = NSImage(size: size, flipped: false) { rect in
            let markRect = NSRect(x: (rect.width - markSide) / 2, y: 0, width: markSide, height: markSide)
            base.draw(in: markRect)
            // Solid, not labelColor: that one is ~85% opaque, which made the mark look
            // thinner than the template images macOS tints itself.
            let dark = NSAppearance.currentDrawing().bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
            (dark ? NSColor.white : NSColor.black).set()
            markRect.fill(using: .sourceIn)

            // Badge: a filled circle with a white glyph, cut out of the mark around it.
            let r: CGFloat = 4.8
            let u = r / 12.5
            let c = NSPoint(x: rect.width - r - 2.2, y: r)
            NSGraphicsContext.current?.compositingOperation = .destinationOut
            let n = r * 1.15
            NSBezierPath(ovalIn: NSRect(x: c.x - n, y: c.y - n, width: n * 2, height: n * 2)).fill()
            NSGraphicsContext.current?.compositingOperation = .sourceOver

            // A deeper green than systemGreen, so the white check keeps its contrast.
            (badge == .connected ? NSColor(srgbRed: 0.10, green: 0.55, blue: 0.24, alpha: 1) : NSColor.systemOrange).setFill()
            NSBezierPath(ovalIn: NSRect(x: c.x - r, y: c.y - r, width: r * 2, height: r * 2)).fill()

            NSColor.white.setStroke()
            let glyph = NSBezierPath()
            glyph.lineWidth = 3.6 * u
            glyph.lineCapStyle = .round
            glyph.lineJoinStyle = .round
            switch badge {
            case .connected:
                glyph.move(to: NSPoint(x: c.x - 5.5 * u, y: c.y - 0.5 * u))
                glyph.line(to: NSPoint(x: c.x - 1.5 * u, y: c.y - 4.5 * u))
                glyph.line(to: NSPoint(x: c.x + 5.5 * u, y: c.y + 4 * u))
                glyph.stroke()
            case .alert:
                glyph.move(to: NSPoint(x: c.x, y: c.y + 6 * u))
                glyph.line(to: NSPoint(x: c.x, y: c.y - 0.5 * u))
                glyph.stroke()
                NSColor.white.setFill()
                let d = 4 * u
                NSBezierPath(ovalIn: NSRect(x: c.x - d / 2, y: c.y - 5.8 * u - d / 2, width: d, height: d)).fill()
            }
            return true
        }
        image.isTemplate = false
        return image
    }

    private static func template(_ name: String, fallback symbol: String) -> NSImage {
        let image = Bundle.main.image(forResource: name)
            ?? NSImage(systemSymbolName: symbol, accessibilityDescription: AppBranding.name)
            ?? NSImage()
        image.isTemplate = true
        return image
    }
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

    /// Asks Cloudflare's trace endpoint which address our traffic leaves from: the VPN
    /// server's with "Send all traffic", the local network's otherwise. Skipped when the
    /// last answer is younger than `maxAge`; a failed lookup is retried once.
    func refreshPublicIP(maxAge: TimeInterval = 0, retry: Bool = true) {
        guard isConnected else { return }
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
        guard available else { return }
        let content = UNMutableNotificationContent()
        content.title = title
        content.body = body
        content.sound = .default
        let request = UNNotificationRequest(identifier: "connection-status", content: content, trigger: nil)
        UNUserNotificationCenter.current().add(request)
    }
}

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
        .onHover { hovering = $0 }
    }
}

/// On/off switch for a connection. Drawn by hand because the stock switch turns gray
/// whenever its window is not key, which a popover often is not (for example right after
/// it opens, or while another app has focus), so a live connection looked switched off.
struct ConnectSwitch: View {
    var isOn: Bool
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
        .accessibilityLabel("VPN connection")
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
            StatusDot(color: Theme.color(for: state), size: 7, pulsing: state == .connecting)

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

            ConnectSwitch(isOn: state != .idle) { vpn.toggleConnect(profile: profile) }
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

    var body: some View {
        HStack {
            item("Public IP", ip.isEmpty ? "Checking…" : ip)
            Spacer(minLength: 0)
        }
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
                        StatusDot(color: headerColor, size: 6, pulsing: state == .connecting || vpn.isDisconnecting)
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

            if state == .connected {
                ConnectionDetails(ip: vpn.publicIP)
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
        .frame(width: 340)
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
                    Text("On").font(.system(size: 12)).foregroundColor(Theme.secondaryText)
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
        alert.messageText = missing ? "Finish setting up \(AppBranding.name)" : "Update the \(AppBranding.name) network helper"
        alert.informativeText = "\(AppBranding.name) needs its network helper (\(installPath)) to open VPN tunnels. Installing it needs your administrator password — macOS will ask for it next."
        alert.addButton(withTitle: missing ? "Install" : "Update")
        alert.addButton(withTitle: "Not Now")
        guard alert.runModal() == .alertFirstButtonReturn else {
            MainActor.assumeIsolated {
                VPNManager.shared.showNotice(.error, "The network helper isn't installed, so VPN can't connect yet. Relaunch \(AppBranding.name) to set it up.")
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

final class AppDelegate: NSObject, NSApplicationDelegate, NSPopoverDelegate, NSWindowDelegate, UNUserNotificationCenterDelegate {
    static var shared: AppDelegate?
    var statusItem: NSStatusItem?
    var popover = NSPopover()
    var mainWindow: NSWindow?
    private var linkStateObserver: AnyCancellable?

    func applicationDidFinishLaunching(_ notification: Notification) {
        AppDelegate.shared = self
        installEditMenu()
        statusItem = NSStatusBar.system.statusItem(withLength: AppBranding.menuBarWidth)
        if let button = statusItem?.button {
            button.imagePosition = .imageOnly
            button.imageScaling = .scaleNone
            button.action = #selector(togglePopover(_:))
            button.target = self
        }
        if ConnectionNotifier.available {
            UNUserNotificationCenter.current().delegate = self
            // Asked once at launch; macOS shows the prompt only the first time.
            ConnectionNotifier.requestAuthorization()
        }
        MainActor.assumeIsolated {
            updateStatusIcon()
            // objectWillChange fires before the new value is stored; hopping to the next
            // main-loop turn reads the value after the change.
            linkStateObserver = VPNManager.shared.objectWillChange
                .receive(on: RunLoop.main)
                .sink { [weak self] _ in
                    MainActor.assumeIsolated { self?.updateStatusIcon() }
                }
        }

        popover.behavior = .transient
        popover.delegate = self
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
            window.title = AppBranding.name
            window.styleMask = [.titled, .closable, .miniaturizable]
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

    /// Status item shows the solid mark with a green dot when connected, the outline mark with an
    /// orange circled "!" while there is an error (see VPNManager.showsErrorBadge), the dimmed outline while
    /// connecting, and the plain outline when idle.
    @MainActor private func updateStatusIcon() {
        guard let button = statusItem?.button else { return }
        let vpn = VPNManager.shared
        let state = vpn.linkState
        let badged = vpn.showsErrorBadge
        if state == .connected {
            button.image = AppBranding.menuBarImage(base: AppBranding.menuBarConnected, badge: .connected)
        } else if badged {
            button.image = AppBranding.menuBarImage(base: AppBranding.menuBarIdle, badge: .alert)
        } else {
            button.image = AppBranding.menuBarIdle
        }
        button.appearsDisabled = state == .connecting && !badged
        let status: String
        if !badged {
            status = Theme.statusText(for: state, reconnecting: vpn.isReconnecting)
        } else if vpn.connectionLost {
            status = vpn.networkOnline ? "Connection lost — reconnecting" : "Connection lost — no network"
        } else {
            status = vpn.activeAlert?.title ?? "Connection failed"
        }
        button.toolTip = "\(AppBranding.name) — \(status)"
    }

    // Show banners even while the app is frontmost (its window is open).
    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .sound])
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
            // Activate first and make the popover key, so its controls draw in their
            // active state from the first frame instead of after the first click.
            NSApp.activate(ignoringOtherApps: true)
            popover.show(relativeTo: button.bounds, of: button, preferredEdge: .minY)
            popover.contentViewController?.view.window?.makeKey()
        }
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.setActivationPolicy(.accessory)
_ = NSApplicationMain(CommandLine.argc, CommandLine.unsafeArgv)
