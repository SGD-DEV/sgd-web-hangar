Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####
## Include the wails tools
####
!include "wails_tools.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

# Finish page two checkboxes:
#   1. Launch Hangar now (default checked, runs the GUI)
#   2. Start Hangar at Windows login (default checked, writes the HKCU Run key
#      so the daemon launches headless next time Windows boots - keeps the
#      MCP server reachable for AI agents)
#
# We retired the "show me where to pin" checkbox because autostart is the
# more valuable hidden feature for our target audience (developers + AI
# integrators). Users who want to pin can still right-click the Start Menu
# entry manually.
#
# IMPORTANT - launching unelevated:
# This installer runs as Administrator (we install to Program Files). If we
# launch hangar.exe via the default MUI_FINISHPAGE_RUN it inherits the
# installer's admin token, and stays elevated for its whole lifetime. That
# breaks PostgreSQL init - postgres.exe refuses to run with an Admin token,
# and the workaround (CreateRestrictedToken) hits a Windows DLL-init failure
# (0xc0000142) because of window-station ACL evaluation under restricted
# tokens. The cleanest fix is "don't elevate Hangar in the first place".
#
# Solution: launch via explorer.exe. The shell process always runs as the
# logged-in user (medium integrity, no admin), so it forks hangar.exe at
# user level even when the installer that invoked it is elevated.
# Reference: https://stackoverflow.com/q/9001638/ (UAC stripping via shell).
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchHangarUnelevated
!define MUI_FINISHPAGE_RUN_TEXT "Launch Hangar now"
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_SHOWREADME_TEXT "Start Hangar at login (recommended for AI integration)"
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION EnableHangarAutostart

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

Function LaunchHangarUnelevated
  # Exec'ing explorer.exe with the path to our exe drops the elevated token -
  # explorer is the shell, which runs at the user's medium integrity level.
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\${PRODUCT_EXECUTABLE}"'
FunctionEnd

Function EnableHangarAutostart
  # Per-user autostart. Same registry key the CLI subcommand
  # `hangar daemon autostart enable` writes - we duplicate it here so
  # the user can opt in from the installer without ever opening a
  # terminal. The value is the daemon entrypoint, NOT the GUI exe,
  # so the system tray comes up at login without flashing a window.
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "Hangar" '"$INSTDIR\${PRODUCT_EXECUTABLE}" daemon'
FunctionEnd

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}" # Default installing folder ($PROGRAMFILES is Program Files folder).
ShowInstDetails show # This will always show the installation details.

Function .onInit
   !insertmacro wails.checkArchitecture
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    # Remove the autostart Run entry if the user set it. Idempotent -
    # DeleteRegValue is a no-op when the value does not exist.
    DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "Hangar"

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
