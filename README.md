# Music Downloader

Turn your YouTube bookmarks into MP3s. 
A desktop app built with Go and Fyne, with song details and album covers from [MusicBrainz](https://musicbrainz.org/) and [Cover Art Archive](https://coverartarchive.org/).

## Install

Get the latest version from [Releases](https://github.com/vincedelmas/music-downloader-go/releases). Everything you need is included inside.

- **Windows:** run the installer, or extract the portable ZIP and keep its files together.
- **Linux:** extract the archive and run `sh install.sh`, then open Music Downloader from your application menu. Requires a `glibc-based` distrib.
- **macOS:** open the DMG and drag the app into Applications. Pick `arm64` for Apple Silicon or `amd64` for Intel.

The app is unsigned, so Windows or macOS may ask you to allow it to run.

## Use it

1. Put your YouTube links in a folder on your Chrome/Brave bookmarks bar. Nested folders work too.
2. In **Preferences**, choose your browser profile's `Bookmarks` file, the bookmark folder, and where to save your music.
3. Set the bookmark name pattern. For `Daft Punk - One More Time`, use `{artist} - {title}`. Check the preview, then save.
4. Open **Downloads**, click **Load bookmarks**, then **Download**.

Existing MP3s in the selected folder are skipped (uses filenames in your music folder, so renamed or moved files won't be recognized).

- Right-click a song to check its file, download it, or refresh its details and cover. Toolbar actions apply to the whole collection.
- Use **Show** to filter by status, and select a song to see what happened.
- **Saved with warning** means the audio is saved, but some details or artwork couldn't be added. Use **Refresh song details** to try again without downloading the audio again.

## Updates

Use the **Updates** tab to update download tools or check for a new app version. 
Close the app before installing an update. Your settings and music are kept.

## Development

You'll need Go 1.24+, a C compiler, and the [Fyne build dependencies](https://docs.fyne.io/started/).

To run locally:

```sh
go run ./cmd/bundle --tools-only
go build -o music-downloader .
./music-downloader
```

On Windows, use `music-downloader.exe` as the output name. Keep the executable beside `tools/`.

To create a release package in `dist/`:

```sh
go run ./cmd/bundle --version dev
```

Packaging also needs Inno Setup 6 on Windows or Xcode Command Line Tools on macOS.

To run checks:

```sh
go vet ./...
go test -race -tags ci ./...
```

Bundled tools have their own licenses. See [Third-party notices](THIRD_PARTY_NOTICES.txt).
