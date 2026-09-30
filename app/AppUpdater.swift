import Cocoa
import Combine
import Network
import SwiftUI
import UserNotifications

// MARK: - App updates

/// Looks for a newer signed release when the app opens and, on the user's say-so, replaces
/// the app with it. Trust stays in the Go CLI: `vpn update --check` and `--download-app`
/// verify the release's ed25519-signed manifest and the zip's SHA-256, so this file never
/// installs anything the release key did not sign.
///
/// The swap runs in a detached shell script after this process has fully exited, and the
/// new app is started as a brand-new process: no window, PID, connection or in-memory state
/// carries over from the old one.
enum AppUpdater {
    private static let autoCheckKey = "autoCheckForUpdates"
    private static let remindAfterKey = "updateRemindAfter"
    /// "Remind Me Later" stays quiet this long.
    private static let remindDelay: TimeInterval = 24 * 60 * 60

    /// On by default, including for installs that never wrote the setting.
    static var autoCheck: Bool {
        get { UserDefaults.standard.object(forKey: autoCheckKey) as? Bool ?? true }
        set { UserDefaults.standard.set(newValue, forKey: autoCheckKey) }
    }

    nonisolated(unsafe) private static var busy = false

    private struct Reply: Decodable { var version: String; var sha256: String? }

    /// Runs the installed CLI and decodes its one JSON line; nil on any failure.
    private static func cli(_ args: [String]) -> Reply? {
        guard FileManager.default.isExecutableFile(atPath: CLIInstaller.installPath) else { return nil }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: CLIInstaller.installPath)
        p.arguments = args
        let out = Pipe()
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        guard (try? p.run()) != nil else { return nil }
        let data = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        guard p.terminationStatus == 0 else { return nil }
        return try? JSONDecoder().decode(Reply.self, from: data)
    }

    private static func isNewer(_ latest: String, than current: String) -> Bool {
        guard let a = CLIInstaller.numbers(latest), let b = CLIInstaller.numbers(current) else { return false }
        for i in 0..<max(a.count, b.count) {
            let x = i < a.count ? a[i] : 0, y = i < b.count ? b[i] : 0
            if x != y { return x > y }
        }
        return false
    }

    /// Only a real, writable bundle can replace itself; a bare `swiftc` build cannot.
    /// Where the app really lives. A quarantined app (downloaded in a browser or from a DMG) is run by
    /// macOS from a read-only copy under .../AppTranslocation/..., so Bundle.main.bundleURL can't be
    /// written to or replaced; ask the system for the original path.
    private static var bundleURL: URL {
        let url = Bundle.main.bundleURL
        guard url.path.contains("/AppTranslocation/") else { return url }
        typealias OriginalPath = @convention(c) (CFURL, UnsafeMutablePointer<Unmanaged<CFError>?>?) -> Unmanaged<CFURL>?
        if let handle = dlopen("/System/Library/Frameworks/Security.framework/Security", RTLD_LAZY),
           let symbol = dlsym(handle, "SecTranslocateCreateOriginalPathForURL") {
            let original = unsafeBitCast(symbol, to: OriginalPath.self)
            if let path = original(url as CFURL, nil)?.takeRetainedValue() as URL? { return path }
        }
        let guess = URL(fileURLWithPath: "/Applications").appendingPathComponent(url.lastPathComponent)
        return FileManager.default.fileExists(atPath: guess.path) ? guess : url
    }

    private static var canSelfUpdate: Bool {
        AppBranding.version != nil && bundleURL.pathExtension == "app"
            && !bundleURL.path.contains("/AppTranslocation/")
            && FileManager.default.isWritableFile(atPath: bundleURL.deletingLastPathComponent().path)
    }

    /// At launch: silent unless a newer release exists and the user hasn't snoozed it.
    static func checkAtLaunch() {
        guard autoCheck else { return }
        check(manual: false)
    }

    /// `manual` (Settings → Check Now) ignores the snooze and reports "up to date" or a failure.
    static func check(manual: Bool) {
        guard !busy, let current = AppBranding.version else { return }
        busy = true
        DispatchQueue.global(qos: .utility).async {
            let latest = cli(["update", "--check"])?.version
            DispatchQueue.main.async {
                busy = false
                guard let latest else {
                    if manual { inform("Couldn't check for updates", "Check your internet connection and that the network helper is installed, then try again.") }
                    return
                }
                guard isNewer(latest, than: current) else {
                    if manual { inform("You're up to date", "\(AppBranding.name) \(current) is the latest version.") }
                    return
                }
                if !manual {
                    if CLIInstaller.isPrompting { return }
                    if let until = UserDefaults.standard.object(forKey: remindAfterKey) as? Date, until > Date() { return }
                }
                prompt(latest: latest, current: current)
            }
        }
    }

    /// The menu bar popover (and a Settings sheet in it) would otherwise cover the alert.
    private static func bringAlertToFront() {
        AppDelegate.shared?.closePopoverNow()
        NSApp.activate(ignoringOtherApps: true)
    }

    /// A floating window instead of NSAlert.runModal: it is not app-modal, so the menu bar item and
    /// the rest of the app stay usable while it is open. (NSAlert's own buttons ignore real clicks
    /// when it is shown without runModal.) Button 0 is the default, the last one answers Escape.
    nonisolated(unsafe) private static var dialog: NSPanel?
    nonisolated(unsafe) private static var dialogResize: AnyCancellable?

    private static func closeDialog() {
        dialog?.orderOut(nil)
        dialog = nil
        dialogResize = nil
    }

    private static func show(_ model: DialogModel) {
        closeDialog()
        let panel = NSPanel(contentRect: .zero, styleMask: [.titled], backing: .buffered, defer: false)
        panel.isReleasedWhenClosed = false
        panel.level = .floating
        // NSPanel hides itself when the app loses focus; this window carries download progress and errors.
        panel.hidesOnDeactivate = false
        panel.titleVisibility = .hidden
        panel.titlebarAppearsTransparent = true
        let host = NSHostingView(rootView: UpdateDialog(model: model))
        panel.contentView = host
        panel.setContentSize(host.fittingSize)
        // The window follows its content as it goes from prompt to progress to error.
        dialogResize = model.objectWillChange.receive(on: RunLoop.main).sink { [weak panel, weak host] _ in
            guard let panel, let host else { return }
            DispatchQueue.main.async {
                let top = panel.frame.maxY
                panel.setContentSize(host.fittingSize)
                panel.setFrameTopLeftPoint(NSPoint(x: panel.frame.minX, y: top))
            }
        }
        panel.center()
        dialog = panel
        NSApp.activate(ignoringOtherApps: true)
        panel.makeKeyAndOrderFront(nil)
    }

    private static func inform(_ title: String, _ text: String) {
        bringAlertToFront()
        let model = DialogModel(title: title, text: text, buttons: ["OK"])
        model.choose = { _ in closeDialog() }
        show(model)
    }

    private static let releasesURL = URL(string: "https://github.com/TOMOSIA-VIETNAM/vpn/releases/latest")!

    private static func prompt(latest: String, current: String) {
        bringAlertToFront()
        let version = latest.trimmingCharacters(in: CharacterSet(charactersIn: "v"))
        let note = canSelfUpdate
            ? "Updating disconnects the VPN, then the app restarts by itself."
            : "Download the new version from the release page."
        let model = DialogModel(
            title: "\(AppBranding.name) \(version) is available",
            text: "You're on \(current).",
            note: note,
            link: ("What's new", URL(string: "https://github.com/TOMOSIA-VIETNAM/vpn/releases/tag/v\(version)")!),
            buttons: [canSelfUpdate ? "Update" : "Open Release Page", "Remind Me Later"])

        // Stays on screen while downloading, and shows why if it can't finish.
        func startUpdate() {
            model.busy = "Downloading the update…"
            install(current: current, progress: { model.busy = $0 }) { message in
                model.busy = nil
                model.title = "Couldn't update"
                model.text = message
                model.note = nil
                model.link = ("Download it manually", releasesURL)
                model.buttons = ["Try Again", "Close"]
                model.choose = { index in
                    if index == 0 { startUpdate() } else { closeDialog() }
                }
            }
        }

        model.choose = { index in
            guard index == 0 else {
                UserDefaults.standard.set(Date().addingTimeInterval(remindDelay), forKey: remindAfterKey)
                return closeDialog()
            }
            UserDefaults.standard.removeObject(forKey: remindAfterKey)
            guard canSelfUpdate else {
                closeDialog()
                NSWorkspace.shared.open(releasesURL)
                return
            }
            startUpdate()
        }
        show(model)
    }


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

    private static func shellQuote(_ s: String) -> String { "'" + s.replacingOccurrences(of: "'", with: "'\\''") + "'" }

    private static func install(current: String, progress: @escaping (String) -> Void, failed: @escaping (String) -> Void) {
        guard !busy else { return failed("Another update check is still running. Try again in a moment.") }
        busy = true
        func fail(_ message: String) { DispatchQueue.main.async { failed("\(message.prefix(1).uppercased())\(message.dropFirst())") } }
        DispatchQueue.global(qos: .userInitiated).async {
            let fm = FileManager.default
            let dest = bundleURL
            // Next to the app, so the final swap is a same-volume rename.
            let stage = dest.deletingLastPathComponent().appendingPathComponent(".tmsvpn-update-\(getpid())")
            let zip = fm.temporaryDirectory.appendingPathComponent("tmsvpn-update-\(getpid()).zip").path
            func cleanUp() {
                try? fm.removeItem(atPath: zip)
                try? fm.removeItem(at: stage)
                DispatchQueue.main.async { busy = false }
            }
            guard let reply = cli(["update", "--download-app", zip]), let digest = reply.sha256 else {
                cleanUp(); return fail("the download failed or could not be verified.")
            }
            // The release could have moved on since the check, and must still be an upgrade.
            guard isNewer(reply.version, than: current) else { cleanUp(); return fail("no newer version to install.") }
            guard sha256(zip) == digest else { cleanUp(); return fail("the downloaded file changed on disk.") }
            try? fm.removeItem(at: stage)
            guard (try? fm.createDirectory(at: stage, withIntermediateDirectories: true)) != nil else {
                cleanUp(); return fail("can't write next to the app.")
            }
            let unzip = Process()
            unzip.executableURL = URL(fileURLWithPath: "/usr/bin/ditto")
            unzip.arguments = ["-x", "-k", zip, stage.path]
            guard (try? unzip.run()) != nil else { cleanUp(); return fail("can't unpack the update.") }
            unzip.waitUntilExit()
            // Whatever the bundle is called in the zip (releases have renamed it before).
            let unpacked = (try? fm.contentsOfDirectory(atPath: stage.path))?.filter { $0.hasSuffix(".app") } ?? []
            let fresh = stage.appendingPathComponent(unpacked.count == 1 ? unpacked[0] : dest.lastPathComponent)
            let plist = NSDictionary(contentsOf: fresh.appendingPathComponent("Contents/Info.plist"))
            guard unzip.terminationStatus == 0, fm.fileExists(atPath: fresh.path),
                  let packed = plist?["CFBundleShortVersionString"] as? String,
                  "v" + packed == reply.version || packed == reply.version else {
                cleanUp(); return fail("the update package is not what the release lists.")
            }
            try? fm.removeItem(atPath: zip)
            DispatchQueue.main.async { progress("Installing…") }

            // Runs after this process is gone: wait for the exit, swap the bundle, drop the
            // staging folder, then start the new app fresh. A failed swap restores the old app.
            let script = [
                "PID=\(getpid())",
                "n=0; while /bin/kill -0 $PID 2>/dev/null && [ $n -lt 300 ]; do /bin/sleep 0.1; n=$((n+1)); done",
                "DEST=\(shellQuote(dest.path))",
                "if /bin/mv \"$DEST\" \"$DEST.old\" && /bin/mv \(shellQuote(fresh.path)) \"$DEST\"; then /bin/rm -rf \"$DEST.old\"; else /bin/rm -rf \"$DEST\"; /bin/mv \"$DEST.old\" \"$DEST\"; fi",
                "/bin/rm -rf \(shellQuote(stage.path))",
                "/usr/bin/touch \"$DEST\"",
                "/usr/bin/open \"$DEST\"",
            ].joined(separator: "\n")
            let sh = Process()
            sh.executableURL = URL(fileURLWithPath: "/bin/sh")
            sh.arguments = ["-c", script]
            sh.standardInput = FileHandle.nullDevice
            sh.standardOutput = FileHandle.nullDevice
            sh.standardError = FileHandle.nullDevice
            guard (try? sh.run()) != nil else { cleanUp(); return fail("can't start the installer.") }
            // Terminating runs applicationWillTerminate, which disconnects an active VPN.
            DispatchQueue.main.async { NSApp.terminate(nil) }
        }
    }
}
