# Music Downloader

A small Go/Fyne desktop app that turns a Chrome or Brave bookmark folder into a local MP3 collection. MusicBrainz and Cover Art Archive add track metadata and artwork without an account or API key.

## Install a release

Download the package matching your operating system and CPU from [GitHub Releases](https://github.com/vincedelmas/music-downloader-go/releases).

- **Windows x64:** run `music-downloader-vX.Y.Z-windows-amd64-setup.exe`. This installs for your user without administrator access and adds a Start menu shortcut. A portable ZIP is also available; keep its entire extracted folder together.
- **Linux x64 (including CachyOS):** extract the `.tar.gz`, open `music-downloader`, and select **Updates → Add to application menu**. Alternatively run `sh install.sh` from the extracted package. Find Music Downloader in your application launcher and pin it to your taskbar. The installed copy lives in `~/.local/opt/music-downloader`.
- **macOS:** download the `arm64` DMG for Apple Silicon or `amd64` for Intel, open it, and drag **Music Downloader.app** into Applications. Keep it in your Dock if desired.

After installing, the source clone or extracted installer can be deleted. The installed app includes **yt-dlp, FFmpeg, FFprobe, and Deno**; no Go, Python, terminal configuration, or separate media-tool installation is required. Normal desktop system libraries are still required on Linux; release builds target glibc-based distributions (Ubuntu 22.04 or newer and comparable distributions), not Alpine/musl. A bare executable copied without its tools folder is not a complete installation.

Releases have no trusted publisher signing or notarization. Windows may warn or block execution under SmartScreen/Smart App Control policies. macOS may require **System Settings → Privacy & Security → Open Anyway**. macOS bundles use a local ad-hoc signature for executable integrity, without a Developer ID certificate. Follow your OS's prompts only for downloads you trust.

## First launch

1. Create a folder on your browser's bookmarks bar and add YouTube music links. Nested folders are supported; other websites are ignored.
2. Open **Preferences**. Browse to your browser profile’s `Bookmarks` file, refresh the folder list, and select your music folder. The file browser is a separate, resizable window; hidden files are visible when choosing bookmarks.
3. Choose where MP3s should be saved. Enter the bookmark name pattern, such as `{artist} - {title}`, `{artist} {-} {title}`, or `{title} | {artist}`. Use the live preview to check the artist and title. The separator is treated as literal text. Each pattern must contain `{artist}` and `{title}` once, with a separator between them.
4. Leave **Add song details and album artwork** enabled for automatic metadata, or turn it off to skip lookups. No credentials are needed.
5. Save settings, open **Downloads**, click **Load bookmarks**, then **Download**.

The app shows each track's state and error details, an overall completion bar, and a cancel button. Files already present are skipped, duplicate links are skipped within a batch, and invalid filename characters are replaced. Selecting a row shows details. Names that don’t match the pattern are reported before downloading. Failed metadata lookup/tagging leaves the downloaded MP3 available and reports a warning. Missing cover art does not prevent other tags from being written. Existing files are not retagged automatically. To correct old tags or artwork, load the same bookmarks and click **Refresh song details**. This updates files already in your chosen music folder without downloading or re-encoding their audio; missing files are skipped.

## Updates

**Updates → Update download tools** downloads the latest supported upstream tool releases into your user data directory. Tools are staged and tested before switching to the new set. Failed updates preserve the previous tools. No administrator access is needed. Older tool sets remain on disk so another running instance is not broken; these can be removed manually when the app is closed.

**Check for app updates** checks this repository's latest GitHub release. **Download app update** downloads the matching installer/package, verifies it against that release's SHA-256 checksums, and opens its folder. Quit the app, then run the Windows installer, replace the macOS app, or extract/install the Linux package. This is a download-and-install flow, not silent binary replacement. Settings and music survive reinstalls.

Tool downloads use HTTPS and validate GitHub asset SHA-256 digests when upstream publishes them. Release checksums detect corruption; they are not a substitute for publisher signing. GitHub API limits or network failures are displayed in the app.

## Where data lives

| Data | Linux | Windows | macOS |
| --- | --- | --- | --- |
| Settings | `$XDG_CONFIG_HOME/music-downloader` or `~/.config/music-downloader` | `%APPDATA%\music-downloader` | `~/Library/Application Support/music-downloader` |
| Updated tools / app update downloads | `$XDG_DATA_HOME/music-downloader` or `~/.local/share/music-downloader` | `%LOCALAPPDATA%\MusicDownloader` | `~/Library/Application Support/MusicDownloader` |
| Music | Chosen in Preferences (default `~/Music/Downloaded Music`) | Chosen in Preferences | Chosen in Preferences |

Settings from the earlier desktop version are migrated to a name pattern when possible. Old Spotify credentials are ignored and removed the next time you save Preferences. The former command-line app's clone-local `config.json` is not imported automatically. Configuration is read from your user settings directory.

Metadata comes from [MusicBrainz](https://musicbrainz.org/doc/MusicBrainz_API), with cover images from [Cover Art Archive](https://musicbrainz.org/doc/Cover_Art_Archive/API). Public searches do not require credentials. Lookups identify the app with a User-Agent, are paced to at most one MusicBrainz request per second across workers, and cache successful matches in memory. Only close title/artist matches are accepted. The lookup compares recording and release candidates, excluding video, live, remix, demo, and bootleg releases for the default studio match. It prefers the artist’s own albums, including artist compilations such as cover albums, over singles and various-artist compilations. Artwork prefers official audio editions at up to 1200 pixels, tries other editions of the same album if needed, and uses the album-level cover as a fallback. Portrait cassette/DVD sleeves and wide booklet scans are rejected. Album artwork coverage varies. Audio still comes from the bookmarked URL.


## Build from source

Requires Go 1.24+, Python 3.11+, a C compiler, and Fyne's platform build dependencies. Build on each target OS; this is not a CGO-free cross-compilation project.

Linux build dependencies:

```sh
# CachyOS / Arch
sudo pacman -S --needed go python base-devel libglvnd libxcursor libxrandr libxinerama libxi libxxf86vm wayland libxkbcommon

# Ubuntu / Debian
sudo apt install golang python3 build-essential libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev
```

On Windows install Go, Python, an x64 MinGW-w64 compiler (`gcc` and `windres` on PATH), and Inno Setup 6. On macOS install Go, Python, and Xcode Command Line Tools.

Build a complete native package:

```sh
python3 scripts/bundle.py --version dev
```

Find the result in `dist/`. Install it using the instructions above before deleting the clone. On Windows, use `python` if that is the Python executable name. The bundler downloads standalone tools, includes their license/build notices, runs each tool to check it, and builds a package for the host OS/architecture. App source and the third-party programs remain separate within the installed package.

For quick development:

```sh
python3 scripts/bundle.py --tools-only
go build -o music-downloader .
./music-downloader
```

Keep the executable beside the populated `tools/` directory. On Windows, build/run `music-downloader.exe`. `go run .` also works, but builds into a temporary directory, so use **Install / update tools** once inside the app in that mode.

Checks:

```sh
go vet ./...
go test -race -tags ci ./...
go build ./...
python3 -m unittest discover -s scripts -p 'test_*.py'
```

The Fyne UI test uses its software test driver. Tests do not download music or call metadata services. Tests that exercise real FFmpeg skip if it is unavailable.

## Automated releases

`.github/workflows/release.yml` uses **Release Please** to open release PRs from conventional commits (`feat:`, `fix:`, and so on), update `version.txt` / the manifest / changelog, and create a GitHub release when the PR is merged. Build jobs run in the same workflow because releases created with `GITHUB_TOKEN` do not trigger a separate release-event workflow.

Squash and merge the Release Please PR into `main` to publish a release. Ordinary pushes only update the release PR and run CI checks; the packaging matrix is gated by Release Please’s `release_created` output. CI may compile the app as a check, but does not upload release packages.

Four native build jobs produce Linux x64, Windows x64, Intel Mac, and Apple Silicon Mac packages, then upload all packages and `checksums.txt` together after all builds pass. Windows ARM64 and Linux ARM64 packages are not currently built. Release packages include bundled tools and third-party license notices. For local release redistribution, collect Go license texts before running the bundler:

```sh
go install github.com/google/go-licenses/v2@latest
go-licenses save . --save_path build/go-licenses --ignore music-downloader-go
python3 scripts/bundle.py --version v1.1.0
```

Repository setup: enable GitHub Actions and allow Actions to create pull requests under **Settings → Actions → General → Workflow permissions**. The workflow requests `contents: write` and `pull-requests: write` for release management, and uses the default token without extra secrets. A release PR made with the default token may need to be closed/reopened by a human to trigger its PR checks, or use a GitHub App token if fully automatic PR checks are required. There is no manual packaging trigger.

FFmpeg builds are GPL-licensed; the package includes the upstream platform license, build information, and source references. Review upstream redistribution/source requirements before publishing modified third-party builds. See `THIRD_PARTY_NOTICES.txt`.
