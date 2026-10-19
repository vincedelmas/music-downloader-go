[Setup]
AppId={{2E65359E-DA2C-4669-9E14-A58DBBA96E9B}
AppName=Music Downloader
AppVersion={#AppVersion}
DefaultDirName={localappdata}\Programs\Music Downloader
DefaultGroupName=Music Downloader
PrivilegesRequired=lowest
OutputDir=..\dist
OutputBaseFilename={#ArtifactName}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
SetupIconFile=..\assets\icon.ico
UninstallDisplayIcon={app}\music-downloader.exe
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
CloseApplications=yes

[Files]
Source: "..\build\package\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{userprograms}\Music Downloader"; Filename: "{app}\music-downloader.exe"
Name: "{userdesktop}\Music Downloader"; Filename: "{app}\music-downloader.exe"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; Flags: unchecked

[Run]
Filename: "{app}\music-downloader.exe"; Description: "Launch Music Downloader"; Flags: nowait postinstall skipifsilent
