# Security model

How S3 Bucket Browser handles your credentials, your data, and destructive
operations. Threat model: a local tool operated by the person who owns the
S3 credentials — we protect secrets at rest, avoid surprise network traffic,
and make destructive actions hard to do by accident.

## Secrets

- **OS keyring first.** Profile secrets are stored in the OS keychain —
  Windows Credential Manager, macOS Keychain, Linux SecretService (via
  `zalando/go-keyring`). Plaintext secrets never touch disk on hosts with a
  keyring.
- **Fallback file, locked down.** On hosts without a keyring (headless
  servers, CI, test sandboxes) — or with `S3B_NO_KEYRING=1` — secrets fall
  back to the config file created with `0600` permissions.
- **Masked everywhere.** Secrets are masked in all human output *and* in
  `--json` output (`s3b profile list` shows masked values only). They are
  never written to logs.
- **Env overrides, not storage.** `S3B_ACCESS_KEY` / `S3B_SECRET_KEY` supply
  credentials for a single invocation; they are not persisted.
- **Portable mode** keeps the config beside the binary — nothing lands in
  your home directory unless you ask for it.

## Shared hosts & data at rest (secure storage)

Deep-dive: what the app writes to disk, who can read it on a **multi-user
host** (terminal server, jump box, shared workstation, USB-stick portable
installs), and the opt-in global **Secure Storage** mode that hardens all
of it. Secure Storage is off by default (the protections below add keyring
dependency and small per-write overhead) and is enabled globally from
*Settings → Security*.

### What lives where, and the per-host exposure

| Asset | At rest in | Default protection | Exposure on a shared host |
|---|---|---|---|
| Data sources & profiles — hostnames, usernames, endpoints, buckets | `profiles.json` in the config dir (`$S3B_CONFIG`, portable `./config`, else OS user-config) | `0600` file in `0700` dir | Owner-only on normal installs, **but portable mode places it next to the binary** — on a USB stick or shared folder it is readable by every account on the machine, and it maps your whole storage topology |
| Secrets — S3 secret keys/session tokens, SFTP/FTP passwords | OS keyring when available; else `profiles.json` | per-user OS keychain; fallback plaintext in the `0600` file | Fallback file is plaintext to anything running as the user or reading backups; portable mode makes it portable plaintext |
| Objects opened in the external editor | `os.TempDir()/s3b-edit/<bucket>` | directory `0700` | Shared `/tmp` on multi-user Linux: the directory was historically `0755`; downloaded **object contents** persisted there after quit or crash |
| Cross-source transfers (Data Source ↔ Data Source) | spool file `s3b-xfer-*` during the copy | `0600` per-file, removed on completion | Owner-only while running, **but a crash leaves the spool behind in `/tmp`** with half the object's contents |
| Activity log (`events.jsonl`) | config dir, or a user-chosen custom dir | `0600` | Object keys, bucket and source URIs in plaintext; custom dirs may sit outside the protected config dir |
| GUI preferences, favorites, dual-pane binding | WebView storage (Windows: `%APPDATA%\<exe>` UDF; Linux/macOS: user data dir) | per-user profile ACLs | Per-user protected; roaming profiles replicate it to the domain |
| Pre-signed URLs | OS clipboard | none — by design | A pre-signed URL is a bearer credential; any process in the session can read it, and it lingers long after use |
| `*.s3bprofile` containers | wherever the user saves them | scrypt + AES-256-GCM (password) | Fine — user-chosen password encryption |
| Decrypted secrets in memory | process RAM | — | Page file / hibernation file / core dumps are OS-level surfaces; not mitigable from the app — see *Residual risks* |

### What Secure Storage changes

Enabled globally from *Settings → Security* (self-describing: the store
file itself carries the envelope, so the CLI and the GUI can never drift
on what mode they are in):

- **`profiles.json` becomes one encrypted envelope** (`s3bsf1` magic,
  AES-256-GCM). The 32-byte master key is random, generated once, and
  stored in the OS keyring under the same `s3b` service the other secrets
  use — so the file is useless without *this user's* keychain session.
  Source metadata (hostnames, usernames, buckets) and any fallback
  secrets are encrypted together. This also fixes portable mode: a stolen
  USB stick carries ciphertext, not your infrastructure map.
- **Keyring becomes mandatory, not optional.** Enabling requires an OS
  keyring; loading an encrypted store on a host without one (or with
  `S3B_NO_KEYRING=1`) fails loudly with remediation text — it never
  silently falls back to plaintext. Disabling decrypts the store back to
  plain JSON and deletes the master key.
- **Editor workspace moves out of the system temp dir** into the config
  dir (`edit/`, `0700`), and both it and the transfer spool area
  (`tmp/`, `0700`) are **wiped on every start** — crash leftovers never
  survive the next launch.
- **File logging turns itself off** when the mode is enabled (the log is
  plaintext by nature; you can consciously re-enable it afterwards).
- **Pre-signed URLs on the clipboard are auto-cleared** 60 seconds after
  copying (best-effort; some desktops do not allow apps to clear the
  clipboard — the app retries on quit).

The editor temp dir being `0700` with `0600` files is enforced in all
modes (it was `0755` before this audit — a plain bug on multi-user
Linux). The transfer spool keeps `0600` per-file semantics in both modes.

### The envelope, precisely

`profiles.json` under secure storage is one self-describing line:

```
s3bsf1|<base64 nonce, 12 bytes>|<base64 AES-256-GCM ciphertext>
```

- The 32-byte master key is generated once from `crypto/rand` on first
  enable and stored base64-encoded in the OS keyring under the same
  `s3b` service the profile secrets use (account `secure/master`). It
  never touches disk.
- Every save seals the marshaled store with a **fresh random nonce**,
  so two saves of identical content produce different files — no
  equality oracle for an attacker comparing snapshots.
- The magic prefix makes the mode **self-describing**: every loader
  (GUI, CLI, tests) sniffs the same bytes, so no preference flag can
  ever drift out of sync with the actual file.
- AES-256-GCM is authenticated encryption: flipped ciphertext, flipped
  nonce or wrong key all fail the tag check. Decrypt errors are
  deliberately generic ("wrong key or tampered file") so the failure
  channel leaks nothing about the plaintext.

### Enabling and disabling

The toggle lives in *Settings → Security* (GUI-only by design — flipping
the mode rewrites the whole store, a conscious seated decision). The
panel also shows the live status: the keyring backend in use and where
the editor/transfer workspaces currently sit.

**Enable** — requires an OS keyring; without one it is refused loudly
and the file is left untouched:

1. the master key is created in the keyring (or the existing one reused);
2. `profiles.json` is rewritten as the envelope — from then on the
   plaintext store never exists on disk;
3. file logging switches itself off (the log is plaintext by nature).
   The previous directory and level/scope filters are kept, so
   consciously re-enabling logging afterwards restores them;
4. the temp workspaces move into the config dir and every location —
   plain-mode and secure-mode alike — is wiped, so nothing from the old
   placement survives the switch.

**Disable** decrypts the store back to plain `0600` JSON (lossless) and
deletes the master-key entry. Secrets still migrate to the keyring
whenever one is available, exactly as before the mode existed.

### Failure modes and recovery

| Situation | Behavior | Recovery |
|---|---|---|
| Encrypted store, host has no keyring (headless, `S3B_NO_KEYRING=1`) | Load fails loudly with remediation text — never a silent fallback to plaintext | Log into a desktop session / unset `S3B_NO_KEYRING`, or disable secure storage on a host with a keyring |
| Master-key entry deleted or corrupt | Load fails: "the master key is missing from the OS keyring" | Restore the keyring from the OS-level backup (Credential Manager / Keychain / SecretService), or restore `profiles.json` from a backup — there is no backdoor by design |
| Tampered or wrong-key file | GCM tag failure, generic error | Restore `profiles.json` from a backup |
| Keyring present, file plain | Normal plaintext mode | — |

Backups: the envelope file copies like any file, but it is decryptable
only together with *this user's* keyring session. For credentials that
must outlive a machine, use the password-encrypted `.s3bprofile` export
(scrypt + AES-256-GCM) — that container is exactly the offline-recovery
path, independent of any keyring.

Operational notes:

- Overhead is one AES-256-GCM seal/unseal per store read/write —
  microseconds on a file measured in kilobytes; transfers are untouched.
  This is why the mode can stay off by default and cost nothing when
  unused.
- Two instances of the app run by the *same* user share the keyring, so
  both read the sealed store fine. Secure Storage draws the line
  between **users**, not between processes of one user (see residual
  risks).

### What the security tests prove

Pinned by `pkg/core/profile/secure_test.go` and `pkg/api/secure_test.go`
(every CI pass, race-detector run included):

- **Round-trip & no leaks**: enable → save → reload → edit → save →
  disable is lossless; the raw file bytes never contain profile names,
  endpoints or key ids — a leak scan over the whole file, not a spot
  check.
- **Crypto hygiene**: fresh nonce per seal; flipped ciphertext/nonce,
  wrong key and malformed envelopes are all rejected.
- **Loud-failure contract**: an encrypted store on a keyring-less host
  fails with `ErrSecureNeedsKeyring` plus remediation text and the file
  stays intact; enabling without a keyring is refused without rewriting
  the store.
- **Hygiene of the disable path**: the master key is actually deleted;
  the store decrypts back to plain JSON.
- **File hygiene**: `0600` envelope permissions (Unix).
- **Crash-leftover wipe**: every workspace location of both modes
  (editor, transfer spool, clipboard staging) is removed.
- **Clipboard scrub**: arms only under secure storage and only for
  signed URLs; a later plain copy does not disturb a pending scrub.

### Residual risks (documented, not mitigated)

- Memory: decrypted credentials live in process memory while the app
  runs; swap/hibernation/core dumps can capture them. OS-level controls
  (encrypted swap, disable hibernation, restricted dump dirs) are the
  mitigation; a userspace app cannot promise otherwise.
- Same-user malware: anything running as *you* can read the keyring entry
  while you are logged in, hook the webview, or read process memory.
  Secure Storage defends against *other accounts and offline/removed
  media* — the realistic multi-user-host threat — not a compromised user
  session.
- The OS clipboard is shared within a login session by design; the
  60-second scrub reduces the window but cannot hide it from concurrent
  processes. File-list sharing with File Explorer (Ctrl+C/Ctrl+V in
  either direction, including the staging mirror of remote copies) rides
  the same channel and can be disabled entirely — Settings → File transfers
  → *Explorer copy & paste* — on locked-down machines; the in-app
  clipboard then never touches the OS clipboard.
- WebView storage (favorites, source bindings) stays per-user-protected
  but unencrypted; it never contains credentials, only names/paths.

## Network behavior

- **No telemetry. Ever.** No metrics, no crash reports, no update pings, no
  accounts. The binary makes no outbound request except to the S3 endpoints
  you configure (plus the connectivity checks you explicitly run:
  `s3b profile test`, `s3b doctor`).
- **Presigning is local.** Pre-signed URLs are computed in-process by the
  AWS SDK's presigner; no third-party service is involved.
- **Provider awareness.** The capability matrix knows provider quirks
  (R2 path-style, MinIO synthetic ACLs, B2 policy gaps) and surfaces them
  before a request fails — no probing "just in case".

## TLS

- TLS verification is on by default for `https://` endpoints.
- `s3b profile add --insecure` skips TLS verification for that profile
  (labs / self-signed MinIO setups). Setting it prints a loud warning.
  It is per-profile and explicit — never a default.

## Destructive operations — the safety ladder

Count first, act second: every bulk destructive operation reports exactly
what it will do before doing it.

| Level | Operations | Gate |
|---|---|---|
| **L0** normal | Delete selection (<50 items), overwrite upload | Confirmation dialog with item count; CLI proceeds for single/piped deletes |
| **L1** large | Delete ≥50 items, prefix delete, recursive storage-class conversion | CLI requires `--force`; GUI lists exact counts + total size and requires an "I understand" check |
| **L2** bucket-wide | Empty or remove a non-empty bucket, purge all noncurrent versions | CLI requires `--force`; GUI requires **typing the bucket name** |
| **L3** unrecoverable | Permanently destroy versions and delete markers | CLI requires `--versions --force` / `versions rm --all`; GUI Delete Window requires explicitly picking the destructive type against the pre-counted summary — an amber consequence line states what is lost, and a Settings toggle can additionally demand typing `delete` before the button unlocks |

Additional rules:

- `--dry-run` exists on every bulk operation — preview the exact key list
  and totals with zero effect.
- On versioned buckets a plain delete always creates a *delete marker*
  (recoverable via `s3b versions undo`); permanence is never implicit.
  The GUI makes that explicit on every source: one Delete Window
  pre-counts the selection and, on versioned buckets, offers three
  types — adding a delete marker (default — everything stays
  restorable), deleting all except the current version, or deleting
  permanently. The two destructive types always require typing
  `delete` first, regardless of Settings (Shift+Del jumps straight to
  the permanent path).
- Removing a versioned bucket with `rb --force` reports and purges the full
  version history, so nothing silently survives in a bucket you deleted.
- Enabling object lock is possible only at bucket creation and is permanent;
  COMPLIANCE retention cannot be shortened or removed by anyone, ever.

## Supply chain

- **Small, audited dependency set.** Runtime dependencies are permissive
  Go modules only: MIT/Apache-2.0 (Wails v3, aws-sdk-go-v2, cobra/pflag,
  go-keyring, fatih/color) plus, for the remote engines, ISC
  (`jlaffaye/ftp`), BSD-2-Clause (`pkg/sftp`) and BSD-3-Clause
  (`golang.org/x/{crypto,net,sys,term}`). **Zero npm
  runtime dependencies.** The frontend embeds inline SVG path data from
  one MIT-licensed icon set — Bootstrap Icons 1.13.1 (pc-display,
  hdd-network, terminal-fill, lock-fill, globe, bucket-fill,
  hdd-rack-fill) — attributed in every release `NOTICE`. The full
  graph is reviewed at each release and
  attached to releases (`go mod graph` report + SBOM).
- **Reproducible artifacts.** Release binaries are built by CI from the
  tagged commit with stripped, version-stamped flags; every artifact's
  SHA-256 is published in `SHA256SUMS`, and each release carries a
  dependency report (`go mod graph`) plus an SBOM generated by `syft`.
  Every release also links its action-verification report
  (`docs/verification/<tag>/` — the release gate run on the tagged commit;
  the workflow refuses to publish a tag without one).
- **Code signing (Windows).** Every release signs all Windows artifacts —
  both architecture binaries, the portable zips and the NSIS installer —
  with a SHA-256 Authenticode signature plus an RFC 3161 timestamp, driven
  by the repository secrets `WINDOWS_CERT_B64` / `WINDOWS_CERT_PASS`
  (a base64-encoded PFX; see `scripts/sign-windows.sh` and
  `.github/workflows/release.yml`). Releases are currently signed with a
  **self-signed "s3b Project" certificate**; its public key ships at
  `scripts/certs/s3b-signing.cer` so a fleet can pin/whitelist it, while
  the private key stays outside the repository. Because the certificate is
  not CA-issued, Microsoft Defender SmartScreen still shows "Unknown
  publisher" on first run — choose *More info → Run anyway* and verify the
  download against `SHA256SUMS` (or install the `.cer` into *Local
  Machine → Trusted People* to silence the prompt fleet-wide). SmartScreen
  reputation accrues **per certificate**: an OV certificate needs a volume
  of clean downloads before the warning disappears, while an EV
  certificate or [Azure Trusted
  Signing](https://learn.microsoft.com/azure/trusted-signing/) starts with
  reputation. A self-signed certificate never clears the warning — it only
  proves tamper-proofing inside your own fleet. Swapping in a CA
  certificate is a matter of replacing the two secrets.
- **Code signing (macOS).** Every release signs the .app bundle before
  packaging the DMG. With the repository secrets configured —
  `MACOS_CERT_B64` / `MACOS_CERT_PASS` (base64-encoded Developer ID
  Application P12) plus `APPLE_ID` / `APPLE_PASSWORD` (an app-specific
  password) / `APPLE_TEAM_ID` for `notarytool` — the bundle is signed
  with a hardened runtime and timestamp, and the DMG is notarized and
  stapled, so Gatekeeper opens the downloaded app cleanly (see
  `scripts/sign-macos.sh`). Without the certificate the bundle ships
  with an **ad-hoc signature**: bytes Gatekeeper can verify, but no
  notarization — macOS Sequoia then reports the downloaded copy as
  “damaged”, and the documented remedy is the one-time
  `xattr -dr com.apple.quarantine` step (README “First launch on
  macOS”). A configured certificate that fails to import, sign or
  notarize fails the release.
- **Provenance markers.** The source tree and every built binary carry
  the creator/license identity in two hidden layers (see
  `internal/provenance`): zero-width watermarks on comment lines of
  nine core files — decodable by `node scripts/provenance.mjs check`
  and guarded by a Go test, so refactors cannot strip them silently —
  and the hidden `s3b provenance` readout (absent from `--help`) that
  answers from any built binary. One honest limit, per Mikko Pesonen:
  a static watermark proves derivation of the marked files, not
  per-byte integrity — that's the right tool for license-abuse
  evidence (artifact integrity stays with `dist/SHA256SUMS`).

## Reporting a vulnerability

Please open a private security advisory on GitHub
(Report a vulnerability → Security tab), or email the maintainer. Include
reproduction steps and the `s3b version` string. Please do not open public
issues for exploitable findings.
