// Composes the "If macOS blocks the app" walkthrough images used by the READMEs.
//
//   swift assets/render-gatekeeper.swift
//
// Reads the four real macOS screenshots in assets/gatekeeper-src/ and writes, into assets/screenshots/:
//   gatekeeper-steps-{en,vi,ja}-{light,dark}.png
// Each image is one card per step (number badge, short caption, screenshot with the button to press
// circled), so the README needs a single <picture> instead of a table.
import Cocoa

let assets = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
let src = assets.appendingPathComponent("gatekeeper-src")
let out = assets.appendingPathComponent("screenshots")

// MARK: Copy

struct Copy {
    let captions: [String]
    let noteTitle: String
    let noteBody: String
}

let copy: [String: Copy] = [
    "en": Copy(
        captions: ["Click Done", "Privacy & Security → Open Anyway", "Click Open Anyway again", "Enter your Mac password"],
        noteTitle: "Good to know",
        noteBody: "The first time you connect, the app asks for your admin password once more. That installs the network helper the VPN tunnel needs."),
    "vi": Copy(
        captions: ["Bấm Done", "Privacy & Security → Open Anyway", "Bấm Open Anyway lần nữa", "Nhập mật khẩu máy Mac"],
        noteTitle: "Lưu ý",
        noteBody: "Lần đầu kết nối, app hỏi mật khẩu quản trị thêm một lần. Đó là để cài network helper mà đường hầm VPN cần."),
    "ja": Copy(
        captions: ["Done をクリック", "プライバシーとセキュリティ → Open Anyway", "もう一度 Open Anyway", "Mac のパスワードを入力"],
        noteTitle: "補足",
        noteBody: "初回接続時に、管理者パスワードをもう一度求められます。VPN トンネルに必要なネットワークヘルパーをインストールするためです。"),
]

// MARK: Themes

func hex(_ v: UInt32) -> NSColor {
    NSColor(srgbRed: CGFloat((v >> 16) & 255) / 255, green: CGFloat((v >> 8) & 255) / 255, blue: CGFloat(v & 255) / 255, alpha: 1)
}

struct Theme {
    let card, border, text, subtext, badge, note: NSColor
}

let themes: [String: Theme] = [
    "light": Theme(card: hex(0xF6F8FA), border: hex(0xD0D7DE), text: hex(0x1F2328), subtext: hex(0x59636E), badge: hex(0x0969DA), note: hex(0x6E7781)),
    "dark": Theme(card: hex(0x151B23), border: hex(0x30363D), text: hex(0xE6EDF3), subtext: hex(0x9198A1), badge: hex(0x388BFD), note: hex(0x7D8590)),
]
let ring = hex(0xFF9F0A)

// MARK: Layout (canvas pixels)

let W: CGFloat = 1760, gap: CGFloat = 32, pad: CGFloat = 32, radius: CGFloat = 28
let colW = (W - 2 * gap) / 3
let head: CGFloat = 64, imgTop: CGFloat = pad + head + 24
let row1H = imgTop + 400 + pad, row2H = imgTop + 460 + pad
let H = row1H + gap + row2H

struct Shot {
    let file: String
    let maxH: CGFloat
    let clip: CGFloat       // corner radius in source pixels
    let mark: NSRect        // button to circle, in source pixels (top-left origin)
}

let shots = [
    Shot(file: "gatekeeper-1-not-opened", maxH: 400, clip: 26, mark: NSRect(x: 32, y: 392, width: 220, height: 56)),
    Shot(file: "gatekeeper-2-open-anyway", maxH: 400, clip: 14, mark: NSRect(x: 731, y: 184, width: 201, height: 46)),
    Shot(file: "gatekeeper-3-confirm", maxH: 460, clip: 26, mark: NSRect(x: 32, y: 524, width: 456, height: 56)),
    Shot(file: "gatekeeper-4-password", maxH: 460, clip: 26, mark: NSRect(x: 28, y: 543, width: 464, height: 60)),
]

// MARK: Drawing helpers

func text(_ s: String, in r: NSRect, size: CGFloat, weight: NSFont.Weight, color: NSColor, center: Bool = false) {
    let style = NSMutableParagraphStyle()
    style.lineBreakMode = .byWordWrapping
    style.alignment = center ? .center : .left
    style.lineSpacing = 4
    NSAttributedString(string: s, attributes: [
        .font: NSFont.systemFont(ofSize: size, weight: weight), .foregroundColor: color, .paragraphStyle: style,
    ]).draw(with: r, options: [.usesLineFragmentOrigin])
}

func card(_ r: NSRect, _ t: Theme) {
    let p = NSBezierPath(roundedRect: r.insetBy(dx: 1, dy: 1), xRadius: radius, yRadius: radius)
    t.card.setFill(); p.fill()
    t.border.setStroke(); p.lineWidth = 2; p.stroke()
}

func badge(_ label: String, at r: NSRect, fill: NSColor) {
    fill.setFill(); NSBezierPath(ovalIn: r).fill()
    text(label, in: NSRect(x: r.minX, y: r.minY + 8, width: r.width, height: r.height), size: 28, weight: .bold, color: .white, center: true)
}

func header(_ label: String, _ caption: String, card r: NSRect, _ t: Theme, badgeFill: NSColor) {
    let d: CGFloat = 52
    badge(label, at: NSRect(x: r.minX + pad, y: r.minY + pad + (head - d) / 2, width: d, height: d), fill: badgeFill)
    let x = r.minX + pad + d + 16
    // Center the caption vertically against the badge whether it takes one line or two.
    let box = NSRect(x: x, y: r.minY + pad, width: r.maxX - pad - x, height: head)
    let one = NSAttributedString(string: caption, attributes: [.font: NSFont.systemFont(ofSize: 28, weight: .semibold)])
        .boundingRect(with: NSSize(width: box.width, height: 1000), options: [.usesLineFragmentOrigin]).height
    text(caption, in: NSRect(x: box.minX, y: box.minY + max(0, (head - one) / 2), width: box.width, height: head), size: 28, weight: .semibold, color: t.text)
}

func screenshot(_ shot: Shot, mode: String, in r: NSRect) {
    let url = src.appendingPathComponent("\(shot.file)-\(mode).png")
    guard let img = NSImage(contentsOf: url), let rep = img.representations.first else { fatalError("missing \(url.path)") }
    let pw = CGFloat(rep.pixelsWide), ph = CGFloat(rep.pixelsHigh)
    let box = NSRect(x: r.minX + pad, y: r.minY + imgTop, width: r.width - 2 * pad, height: shot.maxH)
    let s = min(box.width / pw, box.height / ph)
    let d = NSRect(x: box.midX - pw * s / 2, y: box.midY - ph * s / 2, width: pw * s, height: ph * s)

    NSGraphicsContext.saveGraphicsState()
    NSBezierPath(roundedRect: d, xRadius: shot.clip * s, yRadius: shot.clip * s).addClip()
    img.draw(in: d, from: .zero, operation: .sourceOver, fraction: 1, respectFlipped: true, hints: nil)
    NSGraphicsContext.restoreGraphicsState()

    let m = shot.mark
    let mr = NSRect(x: d.minX + m.minX * s, y: d.minY + m.minY * s, width: m.width * s, height: m.height * s).insetBy(dx: -5, dy: -5)
    let p = NSBezierPath(roundedRect: mr, xRadius: 12, yRadius: 12)
    p.lineWidth = 5; ring.setStroke(); p.stroke()
}

// MARK: Render

func render(lang: String, mode: String) {
    let t = themes[mode]!, c = copy[lang]!
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(W), pixelsHigh: Int(H), bitsPerSample: 8, samplesPerPixel: 4,
                               hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    let ctx = NSGraphicsContext(bitmapImageRep: rep)!
    let flipped = NSGraphicsContext(cgContext: ctx.cgContext, flipped: true)
    // Flip the CG context so drawing uses a top-left origin.
    ctx.cgContext.translateBy(x: 0, y: H); ctx.cgContext.scaleBy(x: 1, y: -1)
    NSGraphicsContext.current = flipped

    let row2Y = row1H + gap
    let cards: [NSRect] = [
        NSRect(x: 0, y: 0, width: colW, height: row1H),
        NSRect(x: colW + gap, y: 0, width: W - colW - gap, height: row1H),
        NSRect(x: 0, y: row2Y, width: colW, height: row2H),
        NSRect(x: colW + gap, y: row2Y, width: colW, height: row2H),
        NSRect(x: 2 * (colW + gap), y: row2Y, width: colW, height: row2H),
    ]
    for (i, r) in cards.enumerated() { card(r, t) }
    for i in 0..<4 {
        header("\(i + 1)", c.captions[i], card: cards[i], t, badgeFill: t.badge)
        screenshot(shots[i], mode: mode, in: cards[i])
    }
    let n = cards[4]
    header("i", c.noteTitle, card: n, t, badgeFill: t.note)
    text(c.noteBody, in: NSRect(x: n.minX + pad, y: n.minY + imgTop, width: n.width - 2 * pad, height: 400), size: 27, weight: .regular, color: t.subtext)

    let data = rep.representation(using: .png, properties: [:])!
    let dest = out.appendingPathComponent("gatekeeper-steps-\(lang)-\(mode).png")
    try! data.write(to: dest)
    print("wrote \(dest.lastPathComponent) (\(Int(W))×\(Int(H)))")
}

for lang in ["en", "vi", "ja"] { for mode in ["light", "dark"] { render(lang: lang, mode: mode) } }
