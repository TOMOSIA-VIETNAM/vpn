// Renders every raster brand asset from one vector definition of the mark.
//
//   swift assets/render-icons.swift
//
// Writes next to this file, into assets/:
//   AppIcon.icns                          .app icon (black tile, white mark)
//   logo.png                              the same tile without margin, for the popover header and README
//   menubar-idle.png / @2x                status item template image, outline mark (disconnected)
//   menubar-connected.png / @2x           status item template image, solid mark (connected)
//
// Menu bar images are black-on-transparent templates: macOS recolors them to match
// the menu bar (black on light, white on dark), like every system status item.
import Cocoa

let assets = URL(fileURLWithPath: #filePath).deletingLastPathComponent()

// MARK: Mark geometry (64 × 64 design grid, y pointing down)

let strokeWidth: CGFloat = 4.5

func shieldPath() -> NSBezierPath {
    let p = NSBezierPath()
    p.move(to: NSPoint(x: 32, y: 5.5))
    p.line(to: NSPoint(x: 53.5, y: 12.5))
    p.line(to: NSPoint(x: 53.5, y: 30))
    p.curve(to: NSPoint(x: 32, y: 58.8), controlPoint1: NSPoint(x: 53.5, y: 44.2), controlPoint2: NSPoint(x: 44.6, y: 54))
    p.curve(to: NSPoint(x: 10.5, y: 30), controlPoint1: NSPoint(x: 19.4, y: 54), controlPoint2: NSPoint(x: 10.5, y: 44.2))
    p.line(to: NSPoint(x: 10.5, y: 12.5))
    p.close()
    return p
}

func letterPath() -> NSBezierPath {
    let p = NSBezierPath()
    p.move(to: NSPoint(x: 22, y: 40))
    p.line(to: NSPoint(x: 22, y: 22.5))
    p.line(to: NSPoint(x: 32, y: 32.5))
    p.line(to: NSPoint(x: 42, y: 22.5))
    p.line(to: NSPoint(x: 42, y: 40))
    return p
}

enum MarkStyle { case outline, solid }

/// Draws the mark into `rect` (square) of the current context.
func drawMark(in rect: NSRect, style: MarkStyle, color: NSColor) {
    NSGraphicsContext.saveGraphicsState()
    let t = NSAffineTransform()
    t.translateX(by: rect.minX, yBy: rect.maxY)
    t.scaleX(by: rect.width / 64, yBy: -rect.height / 64)
    t.concat()

    let shield = shieldPath(), letter = letterPath()
    for p in [shield, letter] {
        p.lineWidth = strokeWidth
        p.lineJoinStyle = .round
        p.lineCapStyle = .round
    }
    color.set()
    switch style {
    case .outline:
        shield.stroke()
        letter.stroke()
    case .solid:
        shield.fill()
        shield.stroke()
        NSGraphicsContext.current?.compositingOperation = .destinationOut
        letter.stroke()
    }
    NSGraphicsContext.restoreGraphicsState()
}

// MARK: Rendering helpers

func render(pixels: Int, _ draw: (NSRect) -> Void) -> Data {
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels,
                               bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                               colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    NSGraphicsContext.current?.imageInterpolation = .high
    draw(NSRect(x: 0, y: 0, width: pixels, height: pixels))
    NSGraphicsContext.restoreGraphicsState()
    return rep.representation(using: .png, properties: [:])!
}

/// Black rounded tile with the white mark. `margin` is the transparent border as a
/// fraction of the canvas (macOS icon grid: 100 px on a 1024 px canvas).
func drawTile(in canvas: NSRect, margin: CGFloat) {
    let tile = canvas.insetBy(dx: canvas.width * margin, dy: canvas.height * margin)
    let radius = tile.width * 0.225
    let shape = NSBezierPath(roundedRect: tile, xRadius: radius, yRadius: radius)

    if margin > 0 {
        NSGraphicsContext.saveGraphicsState()
        let shadow = NSShadow()
        shadow.shadowColor = NSColor.black.withAlphaComponent(0.35)
        shadow.shadowOffset = NSSize(width: 0, height: -canvas.height * 0.01)
        shadow.shadowBlurRadius = canvas.height * 0.02
        shadow.set()
        NSColor.black.setFill()
        shape.fill()
        NSGraphicsContext.restoreGraphicsState()
    }

    NSGradient(starting: NSColor(white: 0.17, alpha: 1), ending: NSColor(white: 0.03, alpha: 1))!
        .draw(in: shape, angle: -90)

    // Hairline edge so the tile still reads on a black Dock or wallpaper.
    NSColor.white.withAlphaComponent(0.10).setStroke()
    let edge = NSBezierPath(roundedRect: tile.insetBy(dx: 0.5, dy: 0.5), xRadius: radius, yRadius: radius)
    edge.lineWidth = max(1, canvas.width / 512)
    edge.stroke()

    let markSide = tile.width * 0.56
    let mark = NSRect(x: tile.midX - markSide / 2, y: tile.midY - markSide / 2, width: markSide, height: markSide)
    drawMark(in: mark, style: .outline, color: .white)
}

func write(_ data: Data, _ name: String, in dir: URL = assets) throws {
    try data.write(to: dir.appendingPathComponent(name))
}

// MARK: Outputs

let menuBarPoints = 18
for (name, style) in [("menubar-idle", MarkStyle.outline), ("menubar-connected", .solid)] {
    for scale in [1, 2] {
        let png = render(pixels: menuBarPoints * scale) { drawMark(in: $0, style: style, color: .black) }
        try write(png, scale == 1 ? "\(name).png" : "\(name)@2x.png")
    }
}

try write(render(pixels: 256) { drawTile(in: $0, margin: 0) }, "logo.png")

let iconset = FileManager.default.temporaryDirectory.appendingPathComponent("AppIcon.iconset")
try? FileManager.default.removeItem(at: iconset)
try FileManager.default.createDirectory(at: iconset, withIntermediateDirectories: true)
for base in [16, 32, 128, 256, 512] {
    for scale in [1, 2] {
        let png = render(pixels: base * scale) { drawTile(in: $0, margin: 100.0 / 1024) }
        try write(png, scale == 1 ? "icon_\(base)x\(base).png" : "icon_\(base)x\(base)@2x.png", in: iconset)
    }
}
let iconutil = Process()
iconutil.executableURL = URL(fileURLWithPath: "/usr/bin/iconutil")
iconutil.arguments = ["-c", "icns", iconset.path, "-o", assets.appendingPathComponent("AppIcon.icns").path]
try iconutil.run()
iconutil.waitUntilExit()
guard iconutil.terminationStatus == 0 else { fatalError("iconutil failed") }
print("Rendered brand assets into \(assets.path)")
