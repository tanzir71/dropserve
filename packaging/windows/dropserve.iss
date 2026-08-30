#ifndef SourceDir
  #define SourceDir "..\..\bin"
#endif
#ifndef OutputDir
  #define OutputDir "..\..\dist\installer"
#endif
#ifndef AppVersion
  #define AppVersion "0.0.0-dev"
#endif
#ifndef NumericVersion
  #define NumericVersion "0.0.0.0"
#endif

[Setup]
AppId={{BC7032D9-0DC2-4CA8-B936-B0BF5E271450}
AppName=Dropserve
AppVersion={#AppVersion}
AppPublisher=Dropserve contributors
AppPublisherURL=https://github.com/tanzir71/dropserve
AppSupportURL=https://github.com/tanzir71/dropserve/issues
AppUpdatesURL=https://github.com/tanzir71/dropserve/releases/latest
DefaultDirName={autopf}\Dropserve
DefaultGroupName=Dropserve
DisableProgramGroupPage=yes
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=commandline
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={#OutputDir}
OutputBaseFilename=dropserve-{#AppVersion}-windows-amd64-setup
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
RestartApplications=no
UninstallDisplayIcon={app}\dropserve.exe
LicenseFile=..\..\LICENSE
VersionInfoVersion={#NumericVersion}
VersionInfoCompany=Dropserve contributors
VersionInfoDescription=Dropserve local app server installer
VersionInfoProductName=Dropserve
VersionInfoProductVersion={#NumericVersion}

[Files]
Source: "{#SourceDir}\dropserve.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\dropserve-cli.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\Dropserve"; Filename: "{app}\dropserve.exe"
Name: "{group}\Dropserve diagnostics"; Filename: "{app}\dropserve-cli.exe"; Parameters: "doctor"

[Run]
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall delete rule name=""Dropserve"""; Flags: runhidden waituntilterminated; Check: IsAdminInstallMode
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall add rule name=""Dropserve"" dir=in action=allow program=""{app}\dropserve.exe"" enable=yes profile=private"; Flags: runhidden waituntilterminated; Check: IsAdminInstallMode
Filename: "{app}\dropserve.exe"; Description: "Start Dropserve"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{sys}\taskkill.exe"; Parameters: "/IM dropserve.exe /T /F"; Flags: runhidden waituntilterminated; RunOnceId: "StopDropserve"
Filename: "{sys}\taskkill.exe"; Parameters: "/IM dropserve-cli.exe /T /F"; Flags: runhidden waituntilterminated; RunOnceId: "StopDropserveCLI"
Filename: "{app}\dropserve-cli.exe"; Parameters: "autostart disable"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveAutostart"
Filename: "{app}\dropserve-cli.exe"; Parameters: "trust --uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "RemoveTrust"
Filename: "{sys}\netsh.exe"; Parameters: "advfirewall firewall delete rule name=""Dropserve"""; Flags: runhidden waituntilterminated; RunOnceId: "RemoveFirewall"

[UninstallDelete]
; Dropserve owns these two per-user directories. The user's
; {userprofile}\Dropserve\Apps directory is intentionally outside this list.
Type: filesandordirs; Name: "{localappdata}\Dropserve"
Type: filesandordirs; Name: "{userappdata}\Dropserve"
