import Cocoa
import Combine
import Network
import SwiftUI
import UserNotifications

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

    static func numbers(_ v: String) -> [Int]? {
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

    /// True while the install prompt is on screen, so the update prompt doesn't stack on it.
    nonisolated(unsafe) static var isPrompting = false

    private static func promptAndInstall(bundled: String, missing: Bool) {
        isPrompting = true
        defer { isPrompting = false }
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
        // What gets installed runs as root: refuse a bundle that no longer matches its signature.
        if let why = BundleIntegrity.problem(at: Bundle.main.bundleURL) {
            return "this copy of \(AppBranding.name) is damaged or was modified (\(why)). Download it again."
        }
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
