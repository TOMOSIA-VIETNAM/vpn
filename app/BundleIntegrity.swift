import Foundation
import Security

/// Checks that an app bundle still matches its code signature: every file the signature
/// seals, including the bundled `vpn` tool, is exactly what was signed.
///
/// `CLIInstaller` runs this before it asks for an administrator password, because what it
/// installs from the bundle becomes a setuid-root binary. It stops a damaged download or a
/// file changed after signing from being installed as root. It is not a defence against an
/// attacker who can also re-sign the bundle: that needs a Developer ID signature, which
/// macOS itself then protects (see CONTRIBUTING.md, "Developer ID and notarization").
enum BundleIntegrity {
    /// Returns nil when the bundle verifies, else a short reason.
    static func problem(at url: URL) -> String? {
        var code: SecStaticCode?
        let created = SecStaticCodeCreateWithPath(url as CFURL, [], &code)
        guard created == errSecSuccess, let code else { return describe(created) }
        let flags = SecCSFlags(rawValue: kSecCSCheckAllArchitectures | kSecCSCheckNestedCode)
        let status = SecStaticCodeCheckValidity(code, flags, nil)
        return status == errSecSuccess ? nil : describe(status)
    }

    private static func describe(_ status: OSStatus) -> String {
        (SecCopyErrorMessageString(status, nil) as String?) ?? "code signature error \(status)"
    }
}
