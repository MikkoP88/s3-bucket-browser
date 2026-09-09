; s3b.nsi — NSIS installer for S3 Bucket Browser (M5, PLAN.md §12).
;
;   makensis -DVERSION=1.2.3 scripts/installer/s3b.nsi
;
; makensis resolves relative paths against the SCRIPT's directory (it
; chdirs there while compiling), not the invocation directory — so every
; path is anchored at the repo root via ${__FILEDIR__} (NSIS >= 3.02)
; and the script works from any working directory.
;
; Expects the freshly built binary at dist/s3b.exe and writes the installer
; to dist/s3b-setup-<VERSION>.exe.
;
; What it does NOT do: modify the system PATH. The App Paths registry key
; lets Win+R "s3b" and ShellExecute find the binary without touching PATH,
; and the CLI works from any terminal via the Start Menu shortcut's folder.

Unicode true

!define ROOT "${__FILEDIR__}\..\.."

!ifndef VERSION
  !define VERSION "dev"
!endif

Name "S3 Bucket Browser"
OutFile "${ROOT}\dist\s3b-setup-${VERSION}.exe"
InstallDir "$PROGRAMFILES64\S3 Bucket Browser"
; Upgrade in place: remember the previous install dir.
InstallDirRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\S3BucketBrowser" "InstallLocation"
RequestExecutionLevel admin
SetCompressor /solid lzma

Page components
Page directory
Page instfiles

UninstPage uninstConfirm
UninstPage instfiles

;--------------------------------
; Sections

Section "S3 Bucket Browser (required)"
  SectionIn RO
  SetOutPath "$INSTDIR"
  File "${ROOT}\dist\s3b.exe"

  ; App Paths: find s3b.exe without modifying PATH.
  WriteRegStr HKLM "Software\Microsoft\Windows\App Paths\s3b.exe" "" "$INSTDIR\s3b.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\App Paths\s3b.exe" "Path" "$INSTDIR"

  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateDirectory "$SMPROGRAMS\S3 Bucket Browser"
  CreateShortcut "$SMPROGRAMS\S3 Bucket Browser\S3 Bucket Browser.lnk" "$INSTDIR\s3b.exe"
  CreateShortcut "$SMPROGRAMS\S3 Bucket Browser\Uninstall S3 Bucket Browser.lnk" "$INSTDIR\uninstall.exe"

  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\S3BucketBrowser" "DisplayName" "S3 Bucket Browser"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\S3BucketBrowser" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\S3BucketBrowser" "Publisher" "MikkoP88"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\S3BucketBrowser" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\S3BucketBrowser" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\S3BucketBrowser" "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\S3BucketBrowser" "NoRepair" 1
SectionEnd

Section "Desktop shortcut"
  CreateShortcut "$DESKTOP\S3 Bucket Browser.lnk" "$INSTDIR\s3b.exe"
SectionEnd

;--------------------------------
; Uninstaller
;
; Deliberately leaves user data behind: profiles live in %APPDATA% (or in a
; config\ folder next to the exe in portable mode). RMDir only removes the
; install dir when empty, so a portable config survives uninstall.

Section "Uninstall"
  Delete "$DESKTOP\S3 Bucket Browser.lnk"
  Delete "$SMPROGRAMS\S3 Bucket Browser\S3 Bucket Browser.lnk"
  Delete "$SMPROGRAMS\S3 Bucket Browser\Uninstall S3 Bucket Browser.lnk"
  RMDir "$SMPROGRAMS\S3 Bucket Browser"

  Delete "$INSTDIR\s3b.exe"
  Delete "$INSTDIR\uninstall.exe"
  DeleteRegKey HKLM "Software\Microsoft\Windows\App Paths\s3b.exe"
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\S3BucketBrowser"
  RMDir "$INSTDIR"
SectionEnd
