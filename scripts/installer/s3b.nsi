; s3b.nsi — NSIS installer for S3 Bucket Browser (M5, dual-scope).
;
;   makensis -DVERSION=1.2.3 scripts/installer/s3b.nsi
;
; makensis resolves File/OutFile paths against the script's own directory,
; not the invocation directory — so anchor everything at the repo root
; with ../.. (from scripts/installer/) and the script works from any
; working directory. CI/release pass -DPAYLOAD/-DOUT with absolute paths
; (base-directory-independent) for the same reason.
;
; Expects the freshly built binary at dist/s3b.exe and writes the installer
; to dist/s3b-setup-<VERSION>.exe.
;
; SCOPE: the wizard asks who the install is for — the current user only
; (no administrator needed: %LOCALAPPDATA% + HKCU) or all users (Program
; Files + HKLM, Windows asks for administrator approval once via UAC).
; The uninstaller reads the scope the install recorded and follows it,
; including re-elevating for an all-users uninstall. The binary never
; lands on the system PATH: the App Paths registry key lets Win+R "s3b"
; and ShellExecute find it without touching PATH.
;
; Silent: /S honors the recorded/flagged scope — /ALLUSERS or /CURRENTUSER
; pick one explicitly; a silent run with no flag and no prior install
; defaults to all users (the behavior of every version before the scope
; choice existed, so existing automation rides unchanged).

Unicode true

!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "nsDialogs.nsh"

!define ROOT "../.."

!ifndef VERSION
  !define VERSION "dev"
!endif
!ifndef PAYLOAD
  !define PAYLOAD "${ROOT}/dist/s3b.exe"
!endif
!ifndef OUT
  !define OUT "${ROOT}/dist/s3b-setup-${VERSION}.exe"
!endif

!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\S3BucketBrowser"
!define APPKEY "Software\Microsoft\Windows\CurrentVersion\App Paths\s3b.exe"

Name "S3 Bucket Browser"
; Brand mark on the installer and uninstaller executables (build/icon.ico
; is assembled from the SVG master by tools/appicon).
Icon "${ROOT}/build/icon.ico"
UninstallIcon "${ROOT}/build/icon.ico"
OutFile "${OUT}"
; The default dir depends on the chosen scope and is resolved in .onInit.
InstallDir ""
RequestExecutionLevel user
SetCompressor /solid lzma

Var Scope       ; "all" or "user" — resolved in .onInit, re-read on the scope page
Var PriorDir    ; a previous install's dir, kept so upgrades land in place
Var EntryDir    ; $INSTDIR as the command line left it ("/D=" detection)
Var RadioAll
Var RadioUser

Page custom ScopeCreate ScopeLeave "Installation scope"
Page components
Page directory
Page instfiles

UninstPage uninstConfirm
UninstPage instfiles

;--------------------------------
; Installer entry: flags, prior installs, defaults

Function .onInit
  ; The native registry view: the CLI's setup face (a 64-bit binary) reads
  ; and writes the same keys, so both faces must speak the 64-bit view.
  SetRegView 64

  StrCpy $Scope ""
  StrCpy $PriorDir ""
  StrCpy $EntryDir $INSTDIR ; non-empty when /D= named a dir on the command line

  ${GetParameters} $R0
  ClearErrors
  ${GetOptions} $R0 "/ALLUSERS" $R1
  ${IfNot} ${Errors}
    StrCpy $Scope "all"
  ${EndIf}
  ClearErrors
  ${GetOptions} $R0 "/CURRENTUSER" $R1
  ${IfNot} ${Errors}
    StrCpy $Scope "user"
  ${EndIf}
  ; /RELAUNCHED marks the elevated second copy of an all-users install.

  ; Upgrade in place: with no explicit scope flag, a previous install
  ; names both its dir and its scope — an explicit flag always wins.
  ${If} $Scope == ""
    ReadRegStr $R2 HKCU "${UNINSTKEY}" "InstallLocation"
    ${If} $R2 != ""
      StrCpy $PriorDir $R2
      StrCpy $Scope "user"
    ${Else}
      ReadRegStr $R2 HKLM "${UNINSTKEY}" "InstallLocation"
      ${If} $R2 != ""
        StrCpy $PriorDir $R2
        StrCpy $Scope "all"
      ${EndIf}
    ${EndIf}
  ${EndIf}

  ${If} $Scope == ""
    ${If} ${Silent}
      StrCpy $Scope "all"
    ${Else}
      StrCpy $Scope "user"
    ${EndIf}
  ${EndIf}

  ; A dir from /D= or a previous install is already seated in $INSTDIR
  ; (NSIS parses /D= before .onInit runs); only default an empty one.
  ${If} $INSTDIR == ""
    ${If} $PriorDir != ""
      StrCpy $INSTDIR $PriorDir
    ${Else}
      Call SetDefaultInstDir
    ${EndIf}
  ${EndIf}
FunctionEnd

; Default dir for the current scope. ScopeLeave re-runs this when the page
; flips the scope and the dir is still a standard default — a dir the user
; typed, a previous install's, or /D= on the command line stays untouched.
Function SetDefaultInstDir
  ${If} $Scope == "all"
    StrCpy $INSTDIR "$PROGRAMFILES64\S3 Bucket Browser"
  ${Else}
    StrCpy $INSTDIR "$LOCALAPPDATA\S3 Bucket Browser"
  ${EndIf}
FunctionEnd

; IsCurrentDefaultDir: is $INSTDIR exactly one of the two standard
; defaults (i.e. nobody chose a dir by hand)?
Function IsCurrentDefaultDir
  StrCpy $R3 ""
  ${If} $INSTDIR == "$PROGRAMFILES64\S3 Bucket Browser"
    StrCpy $R3 "yes"
  ${EndIf}
  ${If} $INSTDIR == "$LOCALAPPDATA\S3 Bucket Browser"
    StrCpy $R3 "yes"
  ${EndIf}
FunctionEnd

;--------------------------------
; The scope page

Function ScopeCreate
  nsDialogs::Create 1018
  Pop $R0
  ${NSD_CreateLabel} 0 0 100% 12u "Install S3 Bucket Browser for:"
  Pop $R0
  ${NSD_CreateRadioButton} 0 22u 100% 12u "All users of this computer"
  Pop $RadioAll
  ${NSD_CreateLabel} 10u 36u -10u 16u "Program Files, Start Menu for everyone. Windows asks for administrator approval."
  Pop $R0
  ${NSD_CreateRadioButton} 0 60u 100% 12u "Current user only (recommended)"
  Pop $RadioUser
  ${NSD_CreateLabel} 10u 74u -10u 16u "Your profile only — installs and uninstalls without any administrator rights."
  Pop $R0
  ${If} $Scope == "all"
    ${NSD_Check} $RadioAll
  ${Else}
    ${NSD_Check} $RadioUser
  ${EndIf}
  nsDialogs::Show
FunctionEnd

Function ScopeLeave
  ${NSD_GetState} $RadioAll $R0
  ${If} $R0 == 1
    StrCpy $Scope "all"
  ${Else}
    StrCpy $Scope "user"
  ${EndIf}
  ${If} $EntryDir == ""
  ${AndIf} $PriorDir == ""
    Call IsCurrentDefaultDir
    ${If} $R3 == "yes"
      Call SetDefaultInstDir
    ${EndIf}
  ${EndIf}
FunctionEnd

;--------------------------------
; Sections

Section "S3 Bucket Browser (required)"
  SectionIn RO

  ; All-users needs elevation. The manifest is asInvoker, so an unelevated
  ; process cannot write HKLM: probe, and on refusal re-launch ourselves
  ; elevated with the scope flag, then stop this copy. (A manifest with a
  ; requestedExecutionLevel disables registry virtualization, so the probe
  ; cannot silently land in a fallback hive.)
  ${If} $Scope == "all"
    ClearErrors
    WriteRegStr HKLM "Software\S3BucketBrowser\Probe" "" ""
    ${If} ${Errors}
      ${If} ${Silent}
        ExecShell "runas" '"$EXEPATH" /ALLUSERS /S'
      ${Else}
        ExecShell "runas" '"$EXEPATH" /ALLUSERS'
      ${EndIf}
      SetErrorLevel 0
      Quit
    ${EndIf}
    DeleteRegKey HKLM "Software\S3BucketBrowser\Probe"
  ${EndIf}

  ${If} $Scope == "all"
    SetShellVarContext all
  ${Else}
    SetShellVarContext current
  ${EndIf}

  SetOutPath "$INSTDIR"
  ; /oname: the payload may be staged under any name (CI artifacts) —
  ; the installed binary is always s3b.exe.
  File "/oname=s3b.exe" "${PAYLOAD}"

  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateDirectory "$SMPROGRAMS\S3 Bucket Browser"
  CreateShortcut "$SMPROGRAMS\S3 Bucket Browser\S3 Bucket Browser.lnk" "$INSTDIR\s3b.exe"
  CreateShortcut "$SMPROGRAMS\S3 Bucket Browser\Uninstall S3 Bucket Browser.lnk" "$INSTDIR\uninstall.exe"

  ; App Paths (the scope's hive): find s3b.exe without modifying PATH.
  ; The uninstall key records the scope so the uninstaller follows it.
  ${If} $Scope == "all"
    WriteRegStr HKLM "${APPKEY}" "" "$INSTDIR\s3b.exe"
    WriteRegStr HKLM "${APPKEY}" "Path" "$INSTDIR"

    WriteRegStr HKLM "${UNINSTKEY}" "DisplayName" "S3 Bucket Browser"
    WriteRegStr HKLM "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
    WriteRegStr HKLM "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\s3b.exe,0"
    WriteRegStr HKLM "${UNINSTKEY}" "Publisher" "MikkoP88"
    WriteRegStr HKLM "${UNINSTKEY}" "URLInfoAbout" "https://github.com/MikkoP88/s3-bucket-browser"
    WriteRegStr HKLM "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKLM "${UNINSTKEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    WriteRegStr HKLM "${UNINSTKEY}" "InstallScope" "allusers"
    WriteRegDWORD HKLM "${UNINSTKEY}" "NoModify" 1
    WriteRegDWORD HKLM "${UNINSTKEY}" "NoRepair" 1
  ${Else}
    WriteRegStr HKCU "${APPKEY}" "" "$INSTDIR\s3b.exe"
    WriteRegStr HKCU "${APPKEY}" "Path" "$INSTDIR"

    WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "S3 Bucket Browser"
    WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
    WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\s3b.exe,0"
    WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "MikkoP88"
    WriteRegStr HKCU "${UNINSTKEY}" "URLInfoAbout" "https://github.com/MikkoP88/s3-bucket-browser"
    WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
    WriteRegStr HKCU "${UNINSTKEY}" "InstallScope" "currentuser"
    WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
    WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
  ${EndIf}
SectionEnd

Section "Desktop shortcut"
  ${If} $Scope == "all"
    SetShellVarContext all
  ${Else}
    SetShellVarContext current
  ${EndIf}
  CreateShortcut "$DESKTOP\S3 Bucket Browser.lnk" "$INSTDIR\s3b.exe"
SectionEnd

;--------------------------------
; Uninstaller
;
; The uninstall follows the installation selection: the scope recorded at
; install time picks the hive, the shortcut context and the elevation. A
; per-user install uninstalls without any administrator rights.
;
; Deliberately leaves user data behind: profiles live in %APPDATA% (or in a
; config\ folder next to the exe in portable mode). RMDir only removes the
; install dir when empty, so a portable config survives uninstall.

Function un.onInit
  ; The native registry view, matching the installer's own writes.
  SetRegView 64

  ; Resolve this uninstall.exe's scope: the hive whose recorded install
  ; location matches $INSTDIR, else the recorded scope values, else the
  ; well-known dir prefixes.
  StrCpy $Scope "user"
  ReadRegStr $R0 HKCU "${UNINSTKEY}" "InstallLocation"
  ${If} $R0 == $INSTDIR
    StrCpy $Scope "user"
  ${Else}
    ReadRegStr $R0 HKLM "${UNINSTKEY}" "InstallLocation"
    ${If} $R0 == $INSTDIR
      StrCpy $Scope "all"
    ${Else}
      StrLen $R1 "$PROGRAMFILES64\"
      StrCpy $R2 $INSTDIR $R1
      ${If} $R2 == "$PROGRAMFILES64\"
        StrCpy $Scope "all"
      ${EndIf}
    ${EndIf}
  ${EndIf}

  ; All-users uninstall from an unelevated context: re-launch elevated.
  ${If} $Scope == "all"
    ClearErrors
    WriteRegStr HKLM "Software\S3BucketBrowser\Probe" "" ""
    ${If} ${Errors}
      ${If} ${Silent}
        ExecShell "runas" '"$INSTDIR\uninstall.exe" /S'
      ${Else}
        ExecShell "runas" '"$INSTDIR\uninstall.exe"'
      ${EndIf}
      SetErrorLevel 0
      Quit
    ${EndIf}
    DeleteRegKey HKLM "Software\S3BucketBrowser\Probe"
  ${EndIf}

  ${If} $Scope == "all"
    SetShellVarContext all
  ${Else}
    SetShellVarContext current
  ${EndIf}
FunctionEnd

Section "Uninstall"
  Delete "$DESKTOP\S3 Bucket Browser.lnk"
  Delete "$SMPROGRAMS\S3 Bucket Browser\S3 Bucket Browser.lnk"
  Delete "$SMPROGRAMS\S3 Bucket Browser\Uninstall S3 Bucket Browser.lnk"
  RMDir "$SMPROGRAMS\S3 Bucket Browser"

  Delete "$INSTDIR\s3b.exe"
  Delete "$INSTDIR\uninstall.exe"

  ${If} $Scope == "all"
    DeleteRegKey HKLM "${APPKEY}"
    DeleteRegKey HKLM "${UNINSTKEY}"
  ${Else}
    DeleteRegKey HKCU "${APPKEY}"
    DeleteRegKey HKCU "${UNINSTKEY}"
  ${EndIf}
  RMDir "$INSTDIR"
SectionEnd
