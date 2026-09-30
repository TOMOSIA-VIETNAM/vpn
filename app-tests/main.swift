// Tests for the menu bar app's logic that needs no window: run by test-app.sh, which
// compiles this file together with everything in app/ except its main.swift.
//
//   ./test-app.sh            (after ./build.sh, to include the bundle signature checks)

import Foundation

var failures = 0
var checks = 0

func check(_ ok: @autoclosure () -> Bool, _ what: String, line: UInt = #line) {
    checks += 1
    if !ok() {
        failures += 1
        print("FAIL (line \(line)): \(what)")
    }
}

// MARK: Version ordering (decides whether to offer an update or reinstall the helper)

check(CLIInstaller.numbers("v1.2.3") ?? [] == [1, 2, 3], "v1.2.3 parses")
check(CLIInstaller.numbers("1.2.3") ?? [] == [1, 2, 3], "a version without v parses")
check(CLIInstaller.numbers("dev") == nil, "a dev build has no numeric version")
check(CLIInstaller.numbers("") == nil, "an empty version does not parse")
check(CLIInstaller.numbers("v1.2.x") == nil, "a non-numeric part does not parse")

check(AppUpdater.isNewer("v1.2.4", than: "v1.2.3"), "a higher patch is newer")
check(AppUpdater.isNewer("v1.10.0", than: "v1.9.9"), "parts compare as numbers, not text")
check(AppUpdater.isNewer("v2.0.0", than: "1.9.9"), "the v prefix is optional on either side")
check(!AppUpdater.isNewer("v1.2.3", than: "v1.2.3"), "the same version is not newer")
check(!AppUpdater.isNewer("v1.2.2", than: "v1.2.3"), "an older release is never offered (no downgrade)")
check(!AppUpdater.isNewer("garbage", than: "v1.2.3"), "an unparseable release is not newer")
check(!AppUpdater.isNewer("v9.9.9", than: "dev"), "a dev build is not replaced")
check(AppUpdater.isNewer("v1.3", than: "v1.2.9"), "a missing part counts as zero")

// MARK: Files written by the CLI

let decoder = JSONDecoder()

let stateJSON = """
{"phase":"CONNECTED","profile":"Office","account":"a","server":"203.0.113.7","pid":4242,
 "tun_device":"utun4","updated_at":"2026-09-30T15:58:24+07:00","saved_routes":true,
 "warnings":["Weak encryption in use (3DES), chosen by the server."],"some_future_field":1}
"""
if let st = try? decoder.decode(CLIState.self, from: Data(stateJSON.utf8)) {
    check(st.phase == "CONNECTED" && st.pid == 4242, "state: phase and pid")
    check(st.warnings?.count == 1, "state: warnings reach the app")
    check(st.reconnecting == nil, "state: an absent flag stays absent")
} else {
    check(false, "state.json with warnings and an unknown field decodes")
}
check((try? decoder.decode(CLIState.self, from: Data(#"{"phase":"DISCONNECTED","updated_at":"x"}"#.utf8)))?.warnings == nil,
      "state written by an older CLI (no warnings) still decodes")

let configJSON = """
{"mtu":1400,"kill_switch":true,"strict_crypto":true,"active_profile":"office",
 "profiles":{"office":{"display_name":"Office","server":"vpn.example.com","full_tunnel":true,
   "default_account":"a","accounts":{"a":{"username":"a"}}}}}
"""
if let cfg = try? decoder.decode(CLIConfig.self, from: Data(configJSON.utf8)) {
    check(cfg.mtu == 1400 && cfg.kill_switch == true && cfg.strict_crypto == true, "config: global settings")
    check(cfg.active_profile == "office" && cfg.profiles?["office"] != nil, "config: profiles")
} else {
    check(false, "config.json decodes")
}
check((try? decoder.decode(CLIConfig.self, from: Data(#"{"profiles":{}}"#.utf8)))?.strict_crypto == nil,
      "config written before strict encryption existed still decodes")

// MARK: Bundle signature (guards what the helper installer puts in /usr/local/bin as root)

if CommandLine.arguments.count > 1 {
    let app = URL(fileURLWithPath: CommandLine.arguments[1])
    let fm = FileManager.default
    check(BundleIntegrity.problem(at: app) == nil, "the built app verifies: \(BundleIntegrity.problem(at: app) ?? "")")

    let tmp = fm.temporaryDirectory.appendingPathComponent("tmsvpn-apptest-\(getpid())")
    try? fm.removeItem(at: tmp)
    try! fm.createDirectory(at: tmp, withIntermediateDirectories: true)
    defer { try? fm.removeItem(at: tmp) }

    // A changed helper binary, a changed resource and a changed executable must each be caught.
    for victim in ["Contents/Resources/vpn", "Contents/Resources/Logo.png", "Contents/MacOS/TOMOSIA-VPN"] {
        let copy = tmp.appendingPathComponent(victim.replacingOccurrences(of: "/", with: "_") + ".app")
        try! fm.copyItem(at: app, to: copy)
        let file = copy.appendingPathComponent(victim)
        guard var bytes = try? Data(contentsOf: file), !bytes.isEmpty else {
            print("skip: \(victim) is not in this build")
            continue
        }
        // One flipped byte a quarter of the way in: inside the first architecture's code for the
        // executables (the very middle of a universal binary is unsigned padding between slices).
        bytes[bytes.count / 4] ^= 0xFF
        try! bytes.write(to: file)
        check(BundleIntegrity.problem(at: copy) != nil, "a modified \(victim) is detected")
    }
    check(BundleIntegrity.problem(at: tmp.appendingPathComponent("missing.app")) != nil, "a missing bundle is a problem, not a pass")
} else {
    print("note: no app bundle given — skipping the bundle signature checks")
}

print(failures == 0 ? "ok: \(checks) checks passed" : "\(failures) of \(checks) checks FAILED")
exit(failures == 0 ? 0 : 1)
