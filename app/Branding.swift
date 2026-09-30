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
