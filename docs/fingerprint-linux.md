# Linux fingerprint unlock

Linux desktop builds provide optional fingerprint unlock for each klefki on this
machine. Unlock with the password, open **Settings → Fingerprint unlock**, and
choose **Enable fingerprint unlock**. Touch the enrolled finger when prompted.
Lock the app, then touch the enrolled finger while the lock screen is in focus.
Klefki starts listening automatically and shows a fingerprint beside the password
field. The icon stays still while waiting and animates only when the reader reports
a finger on the sensor. Removing the finger stops the animation, for setup and
unlocking. Hovering or focusing the icon does not animate it. Typing a password, switching to recovery, or leaving the window
stops the scan. Click the icon to cancel an active scan or retry after a failure
or timeout. Returning focus to the lock screen starts another scan if the password
field is empty. A scan session allows three mismatches, automatically restarting
the reader after the first two. Poor-contact retry prompts do not consume an
attempt. The same option works after
restarting Klefki if the Linux keyring is available. Disable it in Settings to
remove the saved keyring entry. Password and recovery unlock remain available.

Close any running Klefki instance and launch the rebuilt executable:

```sh
./desktop/build/bin/klefki-desktop
```

## Requirements

- An enrolled reader supported by `fprintd`, accessible on the system D-Bus.
- A default Secret Service keyring, such as GNOME Keyring or a compatible KWallet.
- `secret-tool` from libsecret, installed in the desktop app's PATH.

Klefki uses your existing Linux enrollment. It does not enroll, export, store,
change, or synchronize fingerprint templates. No root privileges are required.
A busy reader, three mismatches, denied permission, missing keyring, cancellation,
or 45-second timeout leaves the klefki locked. A locked keyring may request its
own password through a native prompt.

## Security boundary

This is Linux keyring-assisted convenience unlock. fprintd verifies the enrolled
finger before Klefki retrieves its keys from the keyring. Secret Service does not
cryptographically bind secret retrieval to that fingerprint result: another app
running as the same user may retrieve entries from an unlocked keyring. This
feature does not protect against a compromised logged-in user session. At-rest
protection of the keyring depends on its provider and configuration; an
unprotected keyring does not become protected merely by enabling this feature.
The Settings UI states this boundary before enablement.

The keyring holds the master key and the derived sync credential, not the user's
password, recovery code, or session tokens. They travel through process pipes,
never process arguments or logs. The local entry is bound to the account ID and
SHA-256 of the complete encrypted account file. Replacing/changing the account
file invalidates saved access. Settings persist only public binding identifiers.
The server protocol, approved v1 crypto formats and key hierarchy are unchanged.
Keyring entries are device-local and are not exported in the account file.

Fingerprint enablement requires a password-based unlocked session and a successful
scan. Recovery-only access cannot enable it. Locking cancels pending scans and
invalidates their results. Normal app exit retains the opt-in keyring entry;
explicit disabling deletes it.

## Verification and manual checks

Automated service tests use a fake fingerprint/keyring provider and cover success,
failure before key retrieval, corrupted entries, disablement, recovery-only
rejection, lock/cancellation, and restart. Adapter tests validate fprintd signal
shape, automatic stop/start after mismatches, success on the third attempt,
failure after three mismatches, poor-contact retries, and cancellation.
Frontend tests cover fingerprint unlock and password fallback on mismatch.
These do not prove a physical fingerprint match.

On the Linux desktop, verify:

1. Enable fingerprint in Settings and complete an enrolled-finger scan.
2. Lock; confirm the reader starts without clicking; use the wrong finger;
   confirm no documents open and the reader accepts two more attempts without
   clicking. Check that a match on the third attempt unlocks; three mismatches
   stop the scan and password still works.
3. Unlock with the enrolled finger; preview a previously imported document.
4. Close/reopen the app and repeat fingerprint unlock.
5. Cancel using the icon, type a password, switch to recovery, and leave the window
   during separate scans; confirm the klefki stays locked. Retry using the icon
   or return focus with an empty password field and confirm another attempt works.
6. Disable fingerprint, lock/restart, and confirm password/recovery access works.
7. Test an unavailable reader or keyring; confirm a readable error and password fallback.

References: [fprintd D-Bus API](https://fprint.freedesktop.org/fprintd-dev/Device.html)
and [Secret Service specification](https://specifications.freedesktop.org/secret-service/latest-single/).

## Implementation evidence (2026-10-10)

Passed focused core/CLI race and integration tests, fingerprint service tests,
Go vet for the desktop service, six frontend interaction tests, frontend build,
Linux adapter tests, and the Wails Linux production build. The adapter's explicit
host availability test passed against the actual enrolled reader and default
keyring. No real fingerprint scan or production-keyring secret write/read was
performed by the agent. The user subsequently confirmed real fingerprint unlock,
contact-only animation, and unlock after restarting the app on 2026-10-10.
See [M4 acceptance](m4-acceptance.md) for the complete desktop walkthrough and
the distinction between user-reported native results and automated checks.
