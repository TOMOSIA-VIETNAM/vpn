# Sources

Every claim spoken or shown in the video, and where it comes from. Paths are relative to the repository root.

| Claim | Source |
|---|---|
| TOMOSIA VPN is a menu bar L2TP/IPsec client for macOS 12+, Apple Silicon and Intel | `README.md`, `README.en.md` (badges and requirements) |
| The company VPN uses L2TP over IPsec with a shared secret | `internal/ike/`, `internal/l2tp/`; "L2TP over IPsec" in the New Configuration sheet (`assets/screenshots/new-configuration-*.png`) |
| One switch per configuration in the menu bar popover | `assets/screenshots/popover-*.png`; `main.swift` (popover view) |
| "Not Connected" / "Connected" status text | `main.swift`, `Theme.statusText` |
| Reconnects on its own after a drop; notifications "VPN connection lost" / "Reconnecting to Office…" and "VPN reconnected" / "Protected via Office again." | `main.swift`, `noteLinkChange` |
| Error titles and actions: "Authentication Failed" (wrong account name or password, Update Password), "IKE Handshake Failed" (check the shared secret, Try Again), "Server Not Responding" (network may be blocking UDP 500/4500, Try Again) | `main.swift` (failure alerts and the primary action for each failure) |
| Kill switch | `assets/screenshots/settings-*.png`; `main.swift` (Settings) |
| Passwords and shared secrets are kept in the Keychain | `main.swift` ("Password and shared secret stay saved in Keychain") |
| Download page | https://github.com/TOMOSIA-VIETNAM/vpn |

Illustrative, not app UI: the video-call window, the System Settings style "Connection failed" panel, the
"VPN Disconnected" notification in the opening and the Dock (generic app tiles, no third-party marks) show a
generic macOS desktop; they carry no real product names. The two cards "Built-in VPN: keeps dropping" and
"Other apps: need certificates" are neutral and name no product.

Background images: `assets/bg/office.jpg` and `assets/bg/cafe.jpg` are AI-generated backgrounds (Gemini image
generation), cropped at the bottom and right edge to remove the generator's corner mark and scaled to
1920x1080. They show no identifiable person, text or logo and are blurred in the composition. The network line
art and the wallpaper colour fields are drawn in `index.html`.

How the video is made: the script, visuals, music and sound effects are written as code and rendered with
HyperFrames and ffmpeg; the on-screen and spoken Vietnamese copy was polished with Gemini; the Vietnamese voice is Gemini TTS (voice Orus, `data/voices.json`).
