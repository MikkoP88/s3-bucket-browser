# Signing the builds

What you need to sign every artifact the project ships — which identity
to acquire for each platform, what it costs, where it plugs into the
pipeline, and how to prove a signature is real. The signing machinery
itself already exists and is fail-closed; this guide is about the
credentials you feed it.

- What signing buys, per platform: [the gate table](#what-signing-buys)
- Windows: [current state](#where-the-pipeline-signs-today) ·
  [choosing an identity](#windows--choosing-an-identity) ·
  [Trusted Signing recipe](#recipe-azure-trusted-signing) ·
  [CA certificate recipe](#recipe-a-ca-certificate-ov-or-ev) ·
  [uninstaller note](#the-nsis-uninstaller) ·
  [verifying](#verifying-a-windows-signature)
- macOS: [the manual stack](#macos--developer-id--notarization)
- Linux: [checksums and the optional GPG layer](#linux--checksums-and-optional-gpg)
- [The one-page checklist](#the-one-page-checklist)

## What signing buys

| Platform | Gate | Unsigned consequence | Signed outcome |
|---|---|---|---|
| Windows | Defender SmartScreen, Authenticode | "Windows protected your PC" on every download until reputation accrues | Publisher name shown; with a trusted cert, the warning fades as clean downloads accumulate (EV and Trusted Signing start with reputation) |
| macOS | Gatekeeper + notarization | "app can't be opened because it is from an unidentified developer" / "damaged" on Apple Silicon | Opens cleanly on any Mac; hardened-runtime signature + stapled notarization ticket |
| Linux | none (no distributor gate) | nothing blocks execution | integrity is proven by checksums (`SHA256SUMS`), optionally GPG-signed |

A signature also pins *who* built the bytes: tampering with a signed
file breaks the signature, so a valid signature is proof the artifact
left this pipeline unchanged. That holds even for the self-signed
certificate described next.

## Where the pipeline signs today

Every release already signs everything Windows, with an internal
certificate; the hooks fail closed (a configured certificate that is
rejected fails the release rather than shipping mixed artifacts):

| Artifact | Signed | How |
|---|---|---|
| `s3b-<v>-windows-amd64.zip` / `-arm64.zip` | yes | [`scripts/sign-windows.sh`](../scripts/sign-windows.sh) — Authenticode SHA-256 + RFC 3161 timestamp, applied to `s3b.exe` **before** zipping, both architectures |
| `s3b-<v>-windows-*-portable.zip` | yes | the portable zips copy the same already-signed binary |
| `s3b-setup-<v>.exe` | yes | built by NSIS from the signed binary, then signed itself — SmartScreen weighs the outermost signature |
| `uninstall.exe` (written at install time) | no | [known gap, closable](#the-nsis-uninstaller) |
| Linux tarballs ×5 | checksummed | `SHA256SUMS` in every release, computed from the final (signed) bytes |
| macOS | local ad-hoc | release artifacts for macOS are disabled; local builds self-sign ad-hoc ([Mac guide](macos-build.md)) |

Current identity: a **self-signed "s3b Project" certificate** whose
public key ships at [`scripts/certs/s3b-signing.cer`](../scripts/certs/s3b-signing.cer)
— a fleet can pin or whitelist it (install the `.cer` into *Local
Machine → Trusted People* to silence prompts fleet-wide). It proves
tamper-proofing but no public trust: SmartScreen still says "Unknown
publisher". The rest of this guide is what to acquire to change that.

The order of operations matters and is already correct — keep it if you
touch the workflow: binaries are signed **before** zipping, the
installer is signed **after** `makensis`, and `SHA256SUMS` is computed
in the release job from the downloaded (already signed) artifacts, so
the checksums always cover the shipped bytes.

## Windows — choosing an identity

One rule shapes every option: **since June 2023 the CA/Browser Forum
requires the private key of every newly issued code-signing
certificate to live in certified hardware** — a USB token or a
provider's cloud signing service. No certificate authority will hand
you an exportable `.pfx` anymore. The existing `WINDOWS_CERT_B64`
secret consumes exactly a PFX, so it fits keys you hold as a file
(today's self-signed one, an internal-CA certificate, or any key you
manage yourself); anything bought new plugs in through its provider:

| Identity | Cost (approx.) | Who can get it | SmartScreen | Fits where |
|---|---|---|---|---|
| Self-signed (today) | $0 | anyone | never clears the warning | `WINDOWS_CERT_B64`, as-is |
| [Azure Trusted Signing](https://learn.microsoft.com/azure/trusted-signing/) | ≈US$10/month (Basic, 5k signatures) | individuals (photo ID) and organizations | starts with reputation | a new sign step in the workflow — [recipe below](#recipe-azure-trusted-signing) |
| CA certificate, OV | ≈US$100–400/year | organizations; Certum's open-source tier serves individual OSS publishers | accrues with clean download volume | provider CLI / Windows-runner leg — [recipe below](#recipe-a-ca-certificate-ov-or-ev) |
| CA certificate, EV | ≈US$300–700/year | registered organizations only | immediate | same integration as OV |

Recommendation for a solo publisher shipping open-weight betas:
**Azure Trusted Signing** — lowest cost, no hardware to babysit,
individual-friendly validation, and the reputation head start. If you
already represent an organization and want the CA route, buy OV or EV
with the provider's **cloud** key (token-bound keys cannot reach
GitHub-hosted runners — a USB token would force a self-hosted runner).

### Recipe: Azure Trusted Signing

1. **Azure subscription** — create one at <https://portal.azure.com>
   (pay-as-you-go is enough).
2. **Trusted Signing account** — search the portal for *Trusted
   Signing*, create an account (pick any available region), then a
   **certificate profile** inside it (public trust, default settings).
   Note the **endpoint URL** shown on the account overview — it looks
   like `https://<account>.<region>.codesigning.azure.net`.
3. **Identity validation** — under the account, submit a validation
   (individual: government photo ID and liveness check; organization:
   business registry). Allow several days. The validated name becomes
   the certificate's publisher subject — you cannot name it "S3 Bucket
   Browser" freely; it will read as the validated publisher.
4. **Service principal** — Microsoft Entra ID → App registrations →
   New registration; then grant it the **Trusted Signing Certificate
   Signer** role on your Trusted Signing account (Access control /
   IAM). Create a client secret. You now hold tenant ID, client ID,
   client secret.
5. **Repo secrets** (Settings → Secrets and variables → Actions):

   | Secret | Value |
   |---|---|
   | `AZURE_TENANT_ID` | the Entra tenant ID |
   | `AZURE_CLIENT_ID` | the service principal's client ID |
   | `AZURE_CLIENT_SECRET` | the client secret |
   | `TRUSTED_SIGNING_ENDPOINT` | the account endpoint URL |
   | `TRUSTED_SIGNING_ACCOUNT` | the Trusted Signing account name |
   | `TRUSTED_SIGNING_PROFILE` | the certificate profile name |

6. **Workflow** — replace the two `Code-sign …` steps in the `windows`
   job of [`.github/workflows/release.yml`](../.github/workflows/release.yml)
   with the official action (pin the current major from its releases
   page), keeping the same position in the sequence — binaries before
   `Package windows zips`, installer after `Build NSIS installer`:

   ```yaml
   - name: Code-sign binaries (Trusted Signing)
     uses: microsoft/trusted-signing-action@v0
     with:
       azure-tenant-id:            ${{ secrets.AZURE_TENANT_ID }}
       azure-client-id:            ${{ secrets.AZURE_CLIENT_ID }}
       azure-client-secret:        ${{ secrets.AZURE_CLIENT_SECRET }}
       endpoint:                   ${{ secrets.TRUSTED_SIGNING_ENDPOINT }}
       trusted-signing-account-name:        ${{ secrets.TRUSTED_SIGNING_ACCOUNT }}
       trusted-signing-certificate-profile-name: ${{ secrets.TRUSTED_SIGNING_PROFILE }}
       files: dist/pkg/s3b.exe,dist/pkg-arm/s3b.exe
       file-digest: SHA256
       timestamp-rfc3161: http://timestamp.acs.microsoft.com
   ```

   The installer step repeats with `files: dist/s3b-setup-*.exe`.
   `scripts/sign-windows.sh` stays in the tree — it serves the PFX
   shape and local signing — but these steps no longer call it.
7. **Prove it** — cut the next tag and verify as in
   [Verifying](#verifying-a-windows-signature); the signer subject
   should read as your validated identity, with a countersignature
   from Microsoft's timestamp service.

### Recipe: a CA certificate (OV or EV)

1. **Buy** from any public CA — DigiCert, Sectigo, GlobalSign, SSL.com,
   Certum (their open-source tier is the individual-friendly one and
   runs on the SimplySign cloud key). Choose the **cloud signing
   service** flavor of the key (SSL.com *eSigner*, DigiCert
   *KeyLocker*, Certum *SimplySign*): a USB-token key cannot be reached
   by GitHub-hosted runners.
2. **Pick the integration shape**:
   - **Vendor CLI inside the existing Linux job** — the cloud services
     ship Linux clients (`smctl sign` for DigiCert KeyLocker,
     `CodeSignTool sign` for SSL.com eSigner). Swap the body of the two
     `Code-sign …` steps for the vendor command, feeding it the
     vendor's own secrets (OAuth client credentials). Same ordering as
     the Trusted Signing recipe.
   - **A Windows signing leg** — add a `windows-latest` job that
     downloads the unsigned artifacts, signs with `signtool` through
     the vendor's key-storage provider (every cloud service installs a
     KSP), and re-uploads:

     ```powershell
     signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 `
       /n "<your certificate subject>" dist\s3b.exe
     ```

   - **EV token, offline** — if you end up with a token anyway, sign
     on your own machine (the `signtool` line above) or move the whole
     sign stage to a self-hosted runner with the token attached.
3. **Rotate the secrets** — when the PFX shape still fits your key
   (self-managed), nothing in the workflow changes: update
   `WINDOWS_CERT_B64` / `WINDOWS_CERT_PASS` and cut the next tag:

   ```bash
   base64 -w0 my-cert.pfx > cert.b64      # Git Bash on Windows: openssl base64
   gh secret set WINDOWS_CERT_B64 < cert.b64
   gh secret set WINDOWS_CERT_PASS        # prompts, never a shell arg
   ```

   A certificate change updates the public half too: refresh
   `scripts/certs/s3b-signing.cer` (fleets pin it) and the two pages
   that describe the current identity ([security.md](security.md),
   [comparison.md](comparison.md)).

### The NSIS uninstaller

`WriteUninstaller` in
[`scripts/installer/s3b.nsi`](../scripts/installer/s3b.nsi) materializes
`uninstall.exe` on the user's machine at install time, so that binary
carries no signature (the installer embedding it does). Low risk, but
if you want every executable signed, NSIS ≥ 3.08 can sign the
uninstaller stub at build time — check `makensis /VERSION` on the
runner, then add to the `.nsi`:

```nsis
; signs the embedded uninstaller with the same PFX hook (keep cert.pfx
; on disk until after makensis and export S3B_CERT_PFX/S3B_CERT_PASS)
!uninstfinalize 'bash ../../scripts/sign-windows.sh "%1"'
```

### Verifying a Windows signature

End user (PowerShell, on a downloaded artifact):

```powershell
Get-AuthenticodeSignature .\s3b.exe | Format-List SignerCertificate, TimeStamperCertificate, Status
```

`Status: Valid`, a signer subject (today `CN=s3b Project`, tomorrow
your CA/Trusted Signing identity) and a **TimeStamperCertificate** —
the RFC 3161 countersignature that keeps the signature valid after the
certificate itself expires.

Maintainer, the same three ways the pipeline signs:

```bash
osslsigncode verify -in dist/pkg/s3b.exe            # Linux runner
signtool verify /pa /all dist\s3b.exe               # Windows SDK
```

SmartScreen reality check: an OV certificate needs a volume of clean
downloads before the warning disappears; EV and Trusted Signing start
with reputation; a self-signed certificate never clears it (see
[security.md](security.md) for the fleet-pinning pattern that makes
that acceptable inside an organization).

## macOS — Developer ID + notarization

macOS release artifacts are disabled; local builds apply an ad-hoc
signature ([`scripts/build-macos.sh`](../scripts/build-macos.sh)) which
Apple Silicon requires to run at all. To distribute a Mac build that
opens cleanly on any machine you need:

| What | Where | Cost |
|---|---|---|
| Apple Developer Program membership | <https://developer.apple.com/programs/> | US$99/year |
| **Developer ID Application** certificate | Certificates → + → Developer ID Application (created on the Mac, CSR from Keychain Access) | included |
| App-specific password for `notarytool` | <https://appleid.apple.com> → Sign-In and Security | included |
| Team ID | membership details page | included |

The full manual flow — storing the notarization profile in the
Keychain, `codesign --force --options runtime --timestamp`, submitting
with `notarytool submit --wait`, stapling, and reading the notarization
log — is the step-by-step
[Notarization with protected Apple credentials](macos-build.md#notarization-with-protected-apple-credentials)
section of the Mac guide. Never use your primary Apple ID password;
never commit any of the four values.

If macOS release jobs ever return,
[`scripts/sign-macos.sh`](../scripts/sign-macos.sh) already carries the
CI contract for that day — base64 P12 in `MACOS_CERT_B64` +
`MACOS_CERT_PASS`, with `APPLE_ID` / `APPLE_PASSWORD` /
`APPLE_TEAM_ID` for notarization — mirroring the Windows secrets'
shape. Verify a distributed Mac build with:

```bash
codesign -dv --verbose=2 "dist/S3 Bucket Browser.app"
spctl -a -t exec --verbose "dist/S3 Bucket Browser.app"
xcrun stapler validate "dist/S3 Bucket Browser.app"   # notarized mode
```

## Linux — checksums and optional GPG

Nothing on Linux demands a signature: no gate blocks an unsigned ELF.
Integrity is covered by `SHA256SUMS`, which the release job computes
from the final artifacts (Windows signing included) — `sha256sum -c
SHA256SUMS` after download is the whole story.

If you also want *who* vouched for the checksums, add a GPG detached
signature over `SHA256SUMS` and publish the `.asc` beside it:

```bash
gpg --armor --detach-sign SHA256SUMS        # → SHA256SUMS.asc
gpg --verify SHA256SUMS.asc SHA256SUMS      # the user-side check
```

This is **not wired into the workflow** — it needs your personal
OpenPGP key published (keyserver + fingerprint recorded in
[security.md](security.md)), a `GPG_PRIVATE_KEY` / `GPG_PASSPHRASE`
secret pair, and one `import + sign` step before the release job's
`gh release create`. Worth it only once strangers download the
artifacts and want a channel to pin your identity.

## The one-page checklist

| Face | You need | Cost | Plugs into | Proof it worked |
|---|---|---|---|---|
| Windows ×5 artifacts | nothing — self-signed today | $0 | already live | `Get-AuthenticodeSignature` shows `CN=s3b Project` + timestamp |
| Windows, publicly trusted | Azure Trusted Signing (individual) *or* CA OV/EV (cloud key) | ≈$10/mo · ≈$100–700/yr | [Trusted Signing recipe](#recipe-azure-trusted-signing) / [CA recipe](#recipe-a-ca-certificate-ov-or-ev) | signer subject reads your validated identity |
| macOS, distribute cleanly | Apple Developer Program + Developer ID Application cert + app-specific password | US$99/yr | [Mac guide's notarization section](macos-build.md#notarization-with-protected-apple-credentials) | `spctl -a -t exec` says *accepted* |
| Linux | nothing mandatory | $0 | `SHA256SUMS` ships per release | `sha256sum -c SHA256SUMS` |
| Linux, vouched checksums (optional) | a published OpenPGP key | $0 | one GPG step in the release job | `gpg --verify SHA256SUMS.asc` |

## Where to go next

- [security.md](security.md) — the signing and supply-chain model this
  guide extends (artifact integrity, SBOM, provenance)
- [build.md](build.md) — building every flavor locally, including the
  release-style Windows build a local signature would stamp
- [macos-build.md](macos-build.md) — the Mac deep-dive this guide's
  macOS section leans on
- [CONTRIBUTING.md](../CONTRIBUTING.md) — *Cutting a release*: the
  tag-driven flow these signing steps ride inside
