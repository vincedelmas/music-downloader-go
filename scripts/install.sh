#!/bin/sh
# Install the extracted release for this user, including a launcher entry.
set -eu
src=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
"$src/music-downloader" --install
