# Shared application core

`app` is UI-independent and exposes the same configuration, portal operations and monitor for Windows GUI and Linux CLI. No package initialization sends network requests or changes Wi-Fi. Tests use local HTTP servers only, not the live authentication or logout endpoints.

## Configuration and security

- Defaults: `nyist-tec`, `https://auth.nyist.edu.cn`, AC ID `1`; startup, background and auto-connect options are opt-in. An empty username/password is valid for first-run configuration. Connecting/monitoring requires both.
- `ConfigPath` uses `os.UserConfigDir()/AuthingNYIST/config.json`.
- Windows saves protect the password with current-user DPAPI, with no UI prompt. Unprotected nonempty passwords in a Windows configuration are rejected instead of silently accepting a plaintext legacy file. A copied DPAPI configuration cannot be decrypted by another user or on Linux.
- Linux saves retain the password in a private configuration file (0600) inside a private directory (0700). This is access control, not encryption; protect backups and the local account. The caller should use the dedicated configuration directory, because saving tightens its containing directory's permissions.
- Saves validate first, write and sync a same-directory temporary file, then atomically replace the destination. Windows uses MoveFileEx with replace/write-through. No delete-before-rename window exists.
- No credentials, authentication URLs, raw portal errors or raw command output are logged or returned. HTTPS is the default and is never automatically downgraded; an explicitly configured HTTP host is supported for local tests and networks requiring it. Custom hosts are explicit trust decisions by the configuration author. Redirects are rejected, including HTTPS-to-HTTP redirects.

## Portal evidence and identity

Read-only inspection of the official portal on 2026-10-23 found:

- `http://auth.nyist.edu.cn/` redirected to `/index_1.html`, whose refresh points to `/srun_portal_pc?ac_id=1&theme=pro`.
- `/static/themes/pro/js/main.js` builds the authentication account from the entered username plus the selected domain, and uses an empty suffix when no domain selector exists. The inspected account page has no domain selector.
- `/cgi-bin/rad_user_info` returns `user_name` separately from `domain`; the current staff session's domain was `nyisttec`. No authentication or logout request was used to inspect it.

Therefore SSID choices do **not** invent or append realms. A supplied explicit `account@realm` is preserved. Status requires exact, case-sensitive account-name equality; an explicit configured realm must also match the returned realm (case-insensitive). A bare configured account accepts the portal-assigned realm. A different account is never treated as online for the requested account, authenticated over, or disconnected. Empty configured usernames never match anyone.

## Network selection

Only `Connect` invokes `EnsureNetwork` (the monitor calls `Connect`). `Check`, configuration operations and `Disconnect` never change the OS network. A different online account is protected before attempting a Wi-Fi switch. A matching account still honors the selected SSID; when that SSID already matches, it does not reconnect Wi-Fi. Status is checked again after network selection, avoiding an unnecessary authentication when the account remains online. Windows child commands hide their console windows.

- Windows inspects `netsh wlan show interfaces`; an exact SSID match makes no changes. Otherwise it runs `netsh wlan connect name=<SSID> ssid=<SSID>`, then waits up to 20 seconds for the requested SSID. A saved profile named exactly `nyist-tec` or `nyist-stu` must already exist; create it using Windows Wi-Fi settings. No Wi-Fi password is accepted by this package.
- Linux checks `/sys/class/net/*/wireless`. With no wireless device, the existing wired route is allowed, without requiring NetworkManager. On wireless hosts it requires `nmcli`, reads active SSIDs without rescanning, and activates an existing NetworkManager profile named after the requested SSID. It then verifies that the SSID actually matches. It never creates profiles or guesses Wi-Fi security settings.

Portal operations use context-aware requests, a 12-second HTTP timeout, bounded response bodies and no redirected authentication. A successful authentication is followed by an exact-account status check. The protocol reuses only the existing pure encoding functions, avoiding legacy request logging.

`Run` reports meaningful status, checks every 30 seconds, and retries at most five consecutive transient failures with 2/4/8/16-second waits before returning the fifth failure. Any portal authentication rejection is terminal (even if potentially transient), preventing password lockouts and hot loops. Conflicting accounts are terminal too. Cancellation interrupts HTTP, OS commands and waits; cancellation does not log out.
