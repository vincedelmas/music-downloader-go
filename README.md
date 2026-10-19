# Music Downloader

Download music from your Chrome or Brave bookmarks as MP3s. Choose a bookmark folder, set your preferences, and manage downloads from a desktop app built with Go and Fyne.

Song details and album covers come from [MusicBrainz](https://musicbrainz.org/) and [Cover Art Archive](https://coverartarchive.org/). No account or API key is needed.

## Installation

Download the package for your system from [Releases](https://github.com/vincedelmas/music-downloader-go/releases).

- **Windows:** run the x64 installer. A portable ZIP is also available; keep its extracted folder together.
- **Linux:** extract the x64 archive and open `music-downloader`. In **Updates**, click **Add to application menu** to install it and create a launcher. You can also run `sh install.sh`.
- **macOS:** open the DMG and drag **Music Downloader.app** into Applications. Choose `arm64` for Apple Silicon or `amd64` for Intel.

The packages include yt-dlp, FFmpeg, FFprobe, and Deno. You do not need to install these separately. Linux packages require a glibc-based desktop distribution, such as CachyOS or Ubuntu 22.04 and newer.

The app is unsigned, so Windows or macOS may show a security warning. On macOS, you may need to allow it under **System Settings → Privacy & Security**.

Once installed, you can delete the source clone or extracted installer. Your settings and music are stored separately.

## Getting started

1. Create a folder on your browser's bookmarks bar and add YouTube links.
2. Open **Preferences** and browse to your browser profile's `Bookmarks` file.
3. Select your bookmark folder and choose where to save your music.
4. Enter the name pattern used by your bookmarks and check the preview.
5. Save your settings, then open **Downloads → Load bookmarks → Download**.

For a bookmark named `Daft Punk - One More Time`, use:

```text
{artist} - {title}
```

You can also write `{artist} {-} {title}`, or reverse the order with `{title} - {artist}`. Include `{artist}` and `{title}` once each, with your separator between them.

Nested bookmark folders are supported. Loading bookmarks checks the selected music directory and marks matching MP3 filenames as **Already downloaded**. Downloads check again before fetching audio, so existing files are skipped even after restarting the app. Renamed or moved files will not be recognized.

Use **Show** to filter queued, active, completed, warning, failed, skipped, or cancelled tracks. Status counts stay visible for the whole collection. Select a track to see its details or any errors, and use **Cancel** to stop a batch.

## Song details and covers

Enable **Add song details and album artwork** in Preferences to add the artist, title, album, year, and cover automatically. Matching prefers studio albums over live recordings and video releases. Some songs or covers may be missing from the database.

Requests for details and covers are paced across download workers. Temporary HTTP failures are retried up to three times with increasing delays, respecting the server's retry time. **Saved with warning** means the audio was downloaded but some details or artwork could not be added.

To fix the details or cover of an existing download, load your bookmarks and click **Refresh song details**. This updates the MP3 without downloading or re-encoding its audio.

## Updates

Open the **Updates** tab to:

- **Update download tools** when downloads stop working or newer tool versions are available.
- **Check for app updates** and download a newer version. Close the app and install the downloaded package to finish updating.

Updating or reinstalling keeps your settings and music.

After a successful tool update, the app keeps the active tool set and the two newest previous sets. Older downloaded sets are removed automatically. If files are in use, cleanup is retried after the next successful update.

## Building from source

You need Go 1.24+, a C compiler, and the [Fyne build dependencies](https://docs.fyne.io/started/). Python is not required.

On CachyOS or Arch:

```sh
sudo pacman -S --needed go base-devel libglvnd libxcursor libxrandr libxinerama libxi libxxf86vm wayland libxkbcommon
```

On Ubuntu or Debian, install Go 1.24+, then the native build dependencies:

```sh
sudo apt install build-essential libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev
```

On Windows, install MinGW-w64 and Inno Setup 6 alongside Go. On macOS, install Xcode Command Line Tools alongside Go.

Build a package for your current system:

```sh
go run ./cmd/bundle --version dev
```

The package is written to `dist/`. Install the package before deleting the clone.

To build and run during development:

```sh
go run ./cmd/bundle --tools-only
go build -o music-downloader .
./music-downloader
```

On Windows, use `music-downloader.exe` as the output filename. Keep the executable beside the `tools/` folder.

Run the checks with:

```sh
go vet ./...
go test -race -tags ci ./...
```

## Repository layout

- `main.go`: application entry point and release version injected during packaging.
- `internal/app/`: desktop UI, bookmarks, downloading, metadata, settings, installation, and updates. Tests sit beside the code they exercise; metadata fixtures live in `internal/app/testdata/`.
- `assets/`: application icons and their embedded Go resource.
- `cmd/bundle/`: packaging command and its tests.
- `scripts/`: Linux installation and Windows installer scripts.

Build output and downloaded tools belong in the ignored `build/`, `dist/`, and `tools/` directories. Release executables are distributed through GitHub Releases.

## Releases

[Release Please](https://github.com/googleapis/release-please) creates release PRs from conventional commits such as `feat:` and `fix:`. Squash and merge the release PR into `main` to publish a version and build the packages.

Release packages are built for Windows x64, Linux x64, Intel Mac, and Apple Silicon Mac. Ordinary pushes run checks without publishing packages.

In the repository settings, enable **Actions → General → Workflow permissions → Allow GitHub Actions to create and approve pull requests**. Release PRs created with the default GitHub token may need to be closed and reopened to trigger their checks.

Bundled tools have their own licenses, including GPL-licensed FFmpeg builds. See [Third-party notices](THIRD_PARTY_NOTICES.txt) for licenses and source references.
