import Cocoa
import Combine
import Network
import SwiftUI
import UserNotifications

// MARK: - App Delegate & Menu Bar Setup

final class AppDelegate: NSObject, NSApplicationDelegate, NSPopoverDelegate, NSWindowDelegate, UNUserNotificationCenterDelegate {
    static var shared: AppDelegate?
    var statusItem: NSStatusItem?
    var popover = NSPopover()
    var mainWindow: NSWindow?
    private var linkStateObserver: AnyCancellable?
    private var outsideClickMonitor: Any?
    private var localClickMonitor: Any?

    /// The popover's window, or a sheet (Settings, Add profile) attached to it.
    private func isPopoverOrItsSheet(_ window: NSWindow?) -> Bool {
        guard let window, let host = popover.contentViewController?.view.window else { return false }
        var current: NSWindow? = window
        while let w = current {
            if w === host { return true }
            current = w.sheetParent
        }
        return false
    }

    /// close() rather than performClose(): the latter is refused while a Settings sheet is attached.
    /// No fade, so it doesn't lag behind the click.
    func closePopoverNow() {
        guard popover.isShown else { return }
        popover.animates = false
        popover.close()
    }

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
        // .transient misses clicks while a sheet is up or the app is inactive; close on any outside click.
        outsideClickMonitor = NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown]) { [weak self] _ in
            MainActor.assumeIsolated { self?.closePopoverNow() }
        }
        // Clicks inside this app but outside the popover (an alert, the main window) don't reach the global monitor.
        localClickMonitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown]) { [weak self] event in
            if let self, self.popover.isShown, !self.isPopoverOrItsSheet(event.window),
               event.window !== self.statusItem?.button?.window {
                self.closePopoverNow()
            }
            return event
        }
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
        // Let the helper prompt (first launch) go first; it is skipped, not stacked, if still open.
        DispatchQueue.main.asyncAfter(deadline: .now() + 3) { AppUpdater.checkAtLaunch() }
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
            let controller = NSHostingController(rootView: MenuBarPopupView(listHeight: 380, width: 380))
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
        popover.animates = true
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
