# Web and PWA passkey unlock

Klefki's web app can unlock its local encrypted library with a PRF-capable
passkey from a compatible platform authenticator, provider, or security key.
The browser/provider chooses fingerprint, face recognition, or PIN verification. Klefki cannot force fingerprint verification and does not
receive or store biometric templates.

## Enable and use

1. Open Klefki on its usual HTTPS domain and unlock with your password.
2. Open **Settings > Privacy & lock** and select **Enable passkey unlock**.
3. Create the passkey in the browser's device prompt, then verify it to finish
   setup. The device may prompt twice. Setup succeeds only when PRF key material
   is returned and the encrypted unlock wrapper passes a local round trip.
4. Lock Klefki. Select **Unlock with passkey** and complete device verification.
   **Cancel passkey verification** returns control to password unlock.
5. To disable it, unlock and select **Remove passkey unlock** in settings.
   This removes the local wrapper; delete the passkey separately in your device's
   credential manager if you no longer want it.

For development, use `http://localhost:8080`, rather than
`http://127.0.0.1:8080`. WebAuthn requires a domain hostname, so IP-address URLs
cannot use this feature even when the page is a secure context. Site storage is
separate for each origin: changing hostname or port does not migrate an existing
local library. Sync/export existing data before changing origins.

Setup is available in regular browser tabs and installed PWAs when WebAuthn
and the secure-origin requirements are available. A built-in authenticator is
not required: the browser can offer a passkey provider or security key. Requests
retain required user verification and discoverable credentials, but do not
restrict attachment or assume an internal transport. This follows the
[WebAuthn authenticator selection rules](https://www.w3.org/TR/webauthn-3/#dom-authenticatorselectioncriteria-authenticatorattachment).
PRF support is checked during the actual ceremony; passkey support alone does
not establish PRF support. Unsupported providers leave password access available. An unsuccessful setup may leave a newly created passkey in the
credential manager, but does not enable local passkey unlock.

## Storage and compatibility

Setup is per account and browser origin/profile. A versioned local-storage
record contains the credential ID, random PRF input, nonce, account binding, and
encrypted unlock wrapper. It contains no password, plaintext document key,
plaintext login credential, PRF output, or wrapping key.

The existing account container, recovery wrapper, document encryption, server
authentication, and sync protocol are unchanged. The PRF output feeds
HKDF-SHA-256, with the account-container hash as salt and a domain-separated
context binding the origin, RP ID, account, credential ID, and PRF input.
The resulting key protects the existing 32-byte document master key and 32-byte
server login credential together using XChaCha20-Poly1305. The wrapper is local
to the web app and is not synced or included in downloaded account files.

Assertions require user verification and check the credential ID, challenge,
origin, ceremony type, RP-ID hash, user-presence flag, and user-verification flag.
The browser mediates the authenticator; this is local key recovery, not a new
server passkey-login endpoint. Decryption requires the PRF-derived wrapping key
and an authenticated wrapper, rather than a successful verification flag alone.

After the app shell and documents are cached, local passkey unlock does not make
a server request. A passkey provider may have its own connectivity requirements;
physical-device offline behavior must be verified. Unlock retains normal sync
access through the separately protected existing login credential.

Locking or leaving the page aborts pending setup/unlock, clears active keys,
and prevents an old asynchronous result from reopening the library. Temporary
PRF outputs, wrapping keys, and key payload buffers are cleared where possible.
JavaScript and WebCrypto runtime copies cannot be reliably wiped.

## Recovery and security limits

Keep the password, encrypted account file, recovery code, and backups. Clearing
site data removes the local library and passkey wrapper; deleting or losing the
passkey removes this unlock route. Password unlock remains available. Browser
recovery-code unlock is still not implemented; recovery uses the existing
desktop/CLI tools and encrypted account file.

Your credential provider may sync the passkey. This feature does not promise
that access is bound to one physical device. Removing the local wrapper does
not revoke a copied wrapper or delete a provider's synced passkey. Other devices
need their own local setup. Account-container changes invalidate its binding.

The existing web-origin trust boundary remains: malicious JavaScript delivered
by the origin or a compromised endpoint can steal keys while unlocked. This
implementation has not received an independent security audit.

## Verification

Verified on 2026-10-15: all 22 passkey Chromium cases and 45 shared crypto checks
passed with the production build. The added desktop-web regression uses a real
Chromium WebAuthn ceremony with a virtual USB/PRF authenticator while the
platform-authenticator probe returns false. It verifies setup, document unlock
and password fallback without changing existing wrappers. Missing WebAuthn and
IP origins keep setup disabled with an explanation. Existing tamper, required
user verification, cancellation, offline, lock and sync-credential tests pass.
These are virtual-authenticator checks; physical security keys, credential
providers and installed-PWA prompts remain separate acceptance checks.


Verified on 2026-10-11: production web build passed, all 43 shared crypto checks
passed, all 21 passkey browser cases passed, and all six existing PWA regression
cases passed. Browser verification used Chromium with a virtual authenticator.

The production web build and shared crypto-vector checks are required alongside:

```sh
npm --prefix web run test:e2e -- passkey.spec.ts klefki.spec.ts
```

The passkey suite uses Chromium's real WebAuthn/PRF implementation with a virtual
platform authenticator. It covers offline reload and document decryption, sync
credentials, encrypted persistence, unsupported PRF, wrapper tampering, removal,
verification cancellation, missing user verification, and pending-operation
locking/page reload. The existing PWA suite checks desktop interoperability,
offline edits, upload retry, tamper rejection, multi-tab exclusion, and locking.

Physical Android fingerprint/PIN prompts, real credential providers, HTTPS
deployment, installed-PWA behavior, and physical-device offline unlock remain
acceptance checks. For a phone, enable on the actual HTTPS origin, lock/reopen,
verify fingerprint/PIN unlock, repeat in airplane mode, then remove setup and
confirm password unlock. Browser tests do not prove those device behaviors.

The implementation follows the [WebAuthn PRF extension](https://www.w3.org/TR/webauthn-3/#prf-extension).
