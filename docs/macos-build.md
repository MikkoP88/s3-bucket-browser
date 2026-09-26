# Building on macOS locally

Build the application in Terminal with the Go compiler. Xcode provides
Apple's clang compiler and the macOS SDK used by Wails' Cocoa/WebKit GUI.
There is no `.xcodeproj` file to open in Xcode. Node, npm installation and
the Wails CLI are not required.

## 1. Toolchain

You need Go 1.26 or newer and either Xcode or Xcode Command Line Tools.
Local builds target macOS 13.0, which also accommodates Go 1.27's minimum
requirement. Check the operating system requirements again when upgrading
to a newer Go release.

If Homebrew is already installed:

```bash
brew install go
go version
command -v go
```

If your Homebrew Go installation is too old, run `brew upgrade go`.
Without Homebrew, use the official macOS installer from the
[Go downloads page](https://go.dev/dl/). Choose arm64 for Apple Silicon
or amd64 for Intel.

After installing full Xcode, open it once and let it finish setup.
Check the tools in Terminal:

```bash
xcode-select -p
xcrun --find clang
xcrun --show-sdk-path
```

If neither Apple toolchain is installed:

```bash
xcode-select --install
```

If the active developer directory is incorrect and Xcode is installed at
this path:

```bash
sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer
```

Resolve license errors by accepting the license in Xcode or running
`sudo xcodebuild -license`. Do not run Go builds with sudo.

## 2. Build and launch

Open the project root, the directory containing `go.mod` and `Makefile`.
Close any previous S3 Bucket Browser instance before opening the new build.

```bash
make mac
open "dist/S3 Bucket Browser.app"
```

The first build downloads the dependencies specified by `go.mod` and
`go.sum`, so it takes longer than subsequent builds. `make mac` builds for
this Mac's architecture. Without make, run `bash scripts/build-macos.sh`.

The script compiles the GUI and CLI into one binary, adds the icon and
Info.plist, checks the deployment target, and applies and verifies a local
ad-hoc signature. It replaces the generated `dist/S3 Bucket Browser.app`
bundle after a successful build. A paid Apple developer account is not
required for this local signature.

Run the app from this directory or copy it to Applications using Finder.
The same bundle also provides the CLI:

```bash
"dist/S3 Bucket Browser.app/Contents/MacOS/s3b" version
"dist/S3 Bucket Browser.app/Contents/MacOS/s3b" --help
```

To build only the binary, without an application bundle:

```bash
make build
./bin/s3b version
./bin/s3b
```

`VERSION=1.2.3 make mac` sets the reported version. By default it comes
from git; `-dirty` indicates local changes. Apple's numeric bundle version
fields contain the numeric prefix, such as `1.2.3`. The CLI reports the
full version, including any beta or git suffix.

## 3. Both Mac architectures

```bash
make mac-universal
file "dist/S3 Bucket Browser.app/Contents/MacOS/s3b"
```

This creates a universal bundle at the same path, containing arm64 and
x86_64 slices. For Intel only, run `bash scripts/build-macos.sh amd64`.
Compiling the Intel slice does not require Rosetta; running it on Apple
Silicon is a separate concern. Verify Intel runtime compatibility separately.

## 4. Verification

```bash
make test
plutil -lint "dist/S3 Bucket Browser.app/Contents/Info.plist"
codesign --verify --strict --verbose=2 "dist/S3 Bucket Browser.app"
xcrun vtool -show-build "dist/S3 Bucket Browser.app/Contents/MacOS/s3b"
```

`make test` runs Go tests with the race detector and the same macOS
compiler settings as the build. In the `vtool` output, `minos` should be
13.0 for both architectures. The SDK version may be newer.

Local verification on September 26, 2026:

| Item | Result |
|---|---|
| Machine | macOS 15.2, Apple Silicon / arm64 |
| Go | Homebrew installation, go1.27.1 darwin/arm64 |
| Apple tools | Xcode 16.3; SDK 15.2 used for the build |
| `make build` | Passed with the corrected Makefile |
| `make mac` | arm64 bundle and signature verification passed |
| `make mac-universal` | arm64 + x86_64, both with `minos` 13.0 |
| CLI inside the bundle | `version` works |
| `make test` | All Go test packages passed with the race detector |
| Desktop launch | `open` succeeded; the macOS window list contained the main window; the process remained running beyond the 60-second startup watchdog limit |

The window list confirms that a native window exists; it does not verify
all rendered content. This was not an acceptance test of every S3 service,
file transfer operation or macOS desktop integration. macOS 13/14 and Intel
hardware were not included in the runtime checks. Local build commands and
Mac tests have been added to CI; their GitHub execution remains to be
verified after the change is pushed.

## 5. Fixes included

- `make build` treated an empty `GOOS` environment variable as Windows and
  added `-H windowsgui` to the Mac link step. The default target now comes
  from `go env GOOS`. The original build failed with Go linker errors.
- Mac bundle creation is now automated with a single command.
- Go 1.27 object code required macOS 13, but the previous instructions
  linked for 12.0. This produced a warning about an object built for a
  newer macOS version. The compiler, linker, Info.plist and CI check now
  use a minimum of 13.0.
- A local file URL test assumed a Windows drive-letter path. Unix paths
  added an extra slash to the expected value. The test was corrected;
  the URL produced by the application was already correct.
- An unverified README claim attributing historical DMG failures to one
  definite cause was removed. Local builds and Gatekeeper checks on
  downloaded applications require separate verification.

## 6. Troubleshooting launch failures

Run the binary in Terminal to see its error output:

```bash
"dist/S3 Bucket Browser.app/Contents/MacOS/s3b"
```

- `go: command not found`: check the installation and open a new Terminal.
- `invalid active developer path` or SDK/clang errors: finish Xcode setup
  and check `xcode-select` and `xcrun` with the commands above.
- `undefined: macosApp` or similar: use `make mac`; the GUI requires cgo.
- Invalid signature: rebuild the bundle. Do not modify the contents of a
  signed `.app` afterward.
- The old application is still visible: close it and open the bundle in
  this project's `dist` directory.

Without Apple's GUI toolchain, you can build the CLI only:

```bash
CGO_ENABLED=0 go build -tags s3b_headless -o bin/s3b-cli ./cmd/s3b
./bin/s3b-cli --help
```

## Local signing and distribution

An ad-hoc signature supports signature verification for this local build.
It is neither an Apple Developer ID signature nor notarization. Downloaded
applications may trigger Gatekeeper warnings. Follow
[Apple's instructions for opening apps](https://support.apple.com/en-au/102445).
The previous README claim that Sequoia had no Open Anyway option was incorrect.

These changes do not enable macOS releases. Developer ID signing and
notarization for distribution are a separate workflow; the certificate
path in `scripts/sign-macos.sh` was not tested as part of the local build
verification. The build script always applies a local ad-hoc signature.

## Notarization with protected Apple credentials

This local workflow prepares a Developer ID signed universal application
for distribution. You need Apple Developer Program membership, your team's
Developer ID Application certificate and private key in your Mac's
Keychain, and Xcode's `notarytool` and `stapler` tools. Signing and
notarization are separate steps.

### Handling credentials

- Use your own trusted Mac and Terminal. Enter your email and password
  only in the tool's local prompts, never in chat, pull requests or issues.
- In your [Apple account](https://account.apple.com), go to **Sign-In and
  Security → App-Specific Passwords** and create a dedicated app-specific
  password for notarization. Do not use your primary Apple account
  password with `notarytool`. Two-factor authentication must be enabled.
- Do not put the password in a `--password` command argument, environment
  variable, `.env` file or script. This avoids storing it in shell history
  or passing it through process arguments. Do not use `set -x` or session
  recording while entering credentials.
- The `store-credentials` command below saves credentials in the Keychain.
  These instructions do not use `--sync`. The profile name `s3b-notary`
  is not a secret.
- Never include a Keychain, `.p12`/`.p8` private keys or passwords in the
  repository or release archive. Base64 does not encrypt a certificate's
  private key. This local workflow requires no certificate export.

The Keychain reduces credential copying but does not protect against a
compromised user session. The public signature includes the publisher's
name and Team ID; these are not the Apple account password or private key.

### 1. Store the notarization profile once

```bash
xcrun notarytool store-credentials "s3b-notary"
```

At the interactive prompts, enter your Apple developer account email,
app-specific password and your team's Team ID. The tool validates the
credentials before storing them. Use only the profile name afterward.

### 2. Build and sign

Run these commands from the project root. If the app already has a
Developer ID signature, skip to verification and packaging. Running
`make mac-universal` again replaces the app with an ad-hoc signed build.

```bash
make mac-universal
security find-identity -v -p codesigning
```

Select **Developer ID Application** from the list. Replace the placeholder
below with the full name of your certificate. The example contains no
real personal information or Team ID.

```bash
codesign --force --options runtime --timestamp \
  --sign "Developer ID Application: YOUR NAME (TEAM_ID)" \
  "dist/S3 Bucket Browser.app"
```

Verify and package the app. Continue only after each command succeeds.

```bash
codesign --verify --strict --all-architectures --verbose=2 \
  "dist/S3 Bucket Browser.app"
codesign -dv --verbose=2 "dist/S3 Bucket Browser.app"

ditto -c -k --sequesterRsrc --keepParent \
  "dist/S3 Bucket Browser.app" \
  "dist/s3b-macos-universal-signed.zip"
```

The signature details must show `Authority=Developer ID Application:` and
a timestamp, rather than `Signature=adhoc`. Do not rebuild, modify the
bundle contents or sign again between the following steps.

### 3. Submit to Apple and wait for acceptance

This command uploads the ZIP to Apple's notarization service. Submit only
the application bundle, not the project directory or user profile files.

```bash
xcrun notarytool submit "dist/s3b-macos-universal-signed.zip" \
  --keychain-profile "s3b-notary" --wait
```

**Continue only when the result is `status: Accepted`.** Neither `Invalid`
nor `In Progress` means notarization succeeded. Keep the returned submission
ID. If the wait is interrupted, check the existing submission instead
(replace `SUBMISSION_ID`):

```bash
xcrun notarytool info "SUBMISSION_ID" --keychain-profile "s3b-notary"
xcrun notarytool log "SUBMISSION_ID" --keychain-profile "s3b-notary" \
  "dist/notarization-log.json"
```

Review the log locally before sharing it: it may contain paths and
publisher information. Fix the reason for rejection and submit the
corrected build again; do not bypass verification by disabling Gatekeeper.

### 4. Staple the ticket and create the final ZIP

```bash
xcrun stapler staple "dist/S3 Bucket Browser.app"
xcrun stapler validate "dist/S3 Bucket Browser.app"
spctl --assess --type execute --verbose=2 "dist/S3 Bucket Browser.app"
```

After the checks succeed, package the app again. The ticket is attached
to the `.app` bundle, not directly to the ZIP.

```bash
ditto -c -k --sequesterRsrc --keepParent \
  "dist/S3 Bucket Browser.app" \
  "dist/s3b-macos-universal-notarized.zip"
```

Attach **`dist/s3b-macos-universal-notarized.zip`** to the release. Before
publishing, also verify that the application extracted from the ZIP works.
Notarization does not replace functional testing.

### Revoking credentials and using CI

If an app-specific password is exposed or no longer needed, revoke it
under App-Specific Passwords in your Apple account. Deleting only the
local Keychain entry does not revoke the password at Apple. If needed,
create a new password and store the profile again with the same command.
If a private signing key is exposed, changing passwords is insufficient:
revoke and replace the affected certificate through the developer account.

These instructions do not add credentials to GitHub or enable automatic
Mac releases. If notarization moves to CI later, store credentials as
secrets in a protected release environment. Never expose them to PR
builds or code from forks. Do not copy your local Keychain to CI.

References: [Apple's notarization workflow](https://developer.apple.com/documentation/security/customizing-the-notarization-workflow)
and [creating and revoking app-specific passwords](https://support.apple.com/en-gb/102654).
