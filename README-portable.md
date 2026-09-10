# S3 Bucket Browser — portable edition

This archive is the portable build of s3b: no installer, no admin rights,
nothing written outside the folder you extract it to. Designed for USB
sticks, shared workstations or running straight out of your Downloads
folder.

## Run it

1. Extract the whole archive to a folder you can write to.
2. Keep the files together: the `s3b` binary (`s3b.exe` on Windows), the
   `s3b-portable` marker, `LICENSE`, `NOTICE` and this README.
3. Start it:
   - **Windows** — double-click `s3b.exe` for the GUI, or run it in a
     terminal for the CLI (`s3b.exe --help`).
   - **Linux** — `./s3b` launches the GUI in a desktop session; with
     arguments it is the CLI.

The `s3b-portable` marker is what makes this build portable — leave it
next to the binary.

## Where your data lives

In portable mode every profile, saved source and the activity log
(`events.jsonl`) is written to a `config` folder created next to the
binary. Copy the folder to another machine and your settings travel with
it; delete the folder and the app is completely gone. That is the whole
uninstall.

## What does not travel

Profile and source secrets (S3 secret keys, SSH and FTP passwords) are
stored in the operating system's credential store — Windows Credential
Manager, macOS Keychain or the Linux secret service — never in files, by
design. They stay on the machine that saved them, so on a new computer
you re-enter each credential once. Everything else is portable.

## Build notes

- `linux-amd64` is the full GUI build and needs the usual webkit2gtk
  runtime libraries installed (the ones Ubuntu/Mint/Fedora desktops
  already ship).
- `linux-arm64` is the headless CLI build — it works on servers and
  containers without any GUI libraries.
- Windows builds (amd64, arm64) are the full GUI + CLI in one binary.
