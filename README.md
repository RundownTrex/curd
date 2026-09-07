# Curd (Linux & Android Fork)

A fast, keyboard-driven CLI application to stream anime with dual [AniList](https://anilist.co/) & [MyAnimeList](https://myanimelist.net/) tracking and Discord Rich Presence, written in Go.

This repository is a focused fork of [Wraient/curd](https://github.com/Wraient/curd), actively maintained and streamlined specifically for **Linux** and **Android** (via Termux + mpv-android).

---

## What's New in This Fork (vs Upstream)

Compared to upstream `wraient/curd`, this fork includes major architectural improvements, mobile support, new providers, and fixes:

### 1. Native Android Support (via Termux & mpv-android)
- **Zero-X11/No-VNC Needed**: Plays directly in the native [mpv-android](https://github.com/mpv-android/mpv-android) app using Android Activity Intents (`am start -n is.xyz.mpv/.MPVActivity`).
- **Interactive Terminal Controller**: While mpv-android streams in the foreground, Termux runs an interactive watch loop:
  - `[Enter]`: Mark the episode as completed and automatically sync progress to AniList / MyAnimeList.
  - `[Esc]`: Immediately switch to the next available provider if the stream buffers or fails.
  - `[q]`: Quit session.
- **Mobile-Tailored Configuration**: Auto-detects Android and isolates settings in `android.conf`, cleanly disabling desktop-only features (Rofi, Ueberzug, Discord RPC).
- **Automated Termux Installer**: One-line script to install or update prebuilt arm64 binaries.

### 2. Modernized & Hardened Provider Stack
- **New Providers**: Added **KickAssAnime**, **AniDB**, and **MegaPlay** with AniList search matching.
- **Rebranded & Updated AllAnime (Mkissa)**: Migrated endpoints (`mkissa.to`, `isekai2nd.com`), implemented dynamic API authentication key generation, and added AES-256-CTR decryption for stream data.
- **AniNeko Integration**: Scrapes both soft-sub (`.vtt`) and hard-sub servers with automatic subtitle track loading.
- **Removed Broken Dependencies**: Completely removed deprecated **Animepahe** scrapers, eliminating heavy headless Chromium requirements.
- **HLS Hardening & Ad-Validation**: Validates M3U8 playlists against ad-segment injection and injects required HTTP headers (Referer/Origin) for protected CDNs (e.g. `fast4speed.rsvp`).
- **Subtitle URL Sanitization**: Built-in `cleanSubtitleURL` handles malformed or concatenated subtitle URLs.
- **Automatic Fallback & Stuck Detection**: Automatically switches providers if a stream fails to load or drops.

### 3. Dual Tracking (AniList + MyAnimeList)
- Simultaneous synchronization with both **AniList** and **MyAnimeList**.
- Automatically reconciles watch history and sets start dates on MyAnimeList.

### 4. Desktop Discord Rich Presence Fix
- Corrected Discord RPC to show dynamic `Watching <Anime Name>` status with episode number, timestamps, and cover art instead of a static application label.

### 5. UI & Navigation Enhancements
- Added "Back" options in menus to return to previous screens without exiting the application.
- Refined Rofi themes and CLI feedback prompts.

### 6. Streamlined CI/CD & Binary Releases
- Removed obsolete and broken Windows (Wine/Inno-Setup) and macOS pipelines.
- Lightweight GitHub Actions CI/CD automatically building and publishing **Linux** (`x86_64`, `arm64`) and **Android** (`arm64`) binaries.

### 7. Episode Downloading
- **Interactive Menu**: Access "Download Episodes" directly from the main menu.
- **Episode Selection**: Search any provider and select single or multiple episodes (`Space` to toggle, `Enter` to confirm).
- **Current Directory Output**: Saves episodes directly to your current working directory (`.`).
- **Terminal Progress**: Real-time download speed, progress bar, and ETA.
- **Rate-Limit Safe**: Controlled concurrency and retry backoff to avoid CDN blocks.


---

## Installation

### 1. Android (Termux)

#### Prerequisites
1. Install **Termux** from [F-Droid](https://f-droid.org/en/packages/com.termux/) (do not use Google Play Store).
2. Install **mpv-android** from [F-Droid](https://f-droid.org/en/packages/is.xyz.mpv/) or Google Play Store.
3. Grant Termux permission to launch background apps:
   - On Android: **Settings > Apps > Termux > Advanced (or Special app access) > Display over other apps > Allow**.
4. Install **termux-am** (handled automatically by the one-line installer, or run `pkg install termux-am`).
5. *(Optional, for downloads)*:
   - Run `termux-setup-storage` to save files to shared phone storage (e.g. `~/storage/shared/Download`).
   - Run `pkg install ffmpeg` to merge separate audio/video streams (e.g. KickAssAnime).

#### One-Line Install (Recommended)
Open Termux and run:
```bash
curl -fsSL https://raw.githubusercontent.com/RundownTrex/curd/main/Build/install-termux.sh | bash
```

#### Build From Source in Termux
```bash
pkg update && pkg upgrade -y
pkg install -y git golang
git clone https://github.com/RundownTrex/curd.git
cd curd
go build -o curd ./cmd/curd
install -m 755 curd $PREFIX/bin/
```

---

### 2. Linux

#### Prerequisites
- **Required**: `mpv` (media player)
- **Optional**: `rofi` and `ueberzugpp` (graphical menu & image preview), `ffmpeg` (remuxing downloads with separate audio)

Install dependencies on your distribution:
```bash
# Debian / Ubuntu
sudo apt update && sudo apt install mpv curl rofi ffmpeg

# Arch Linux
sudo pacman -S mpv curl rofi ueberzugpp ffmpeg

# Fedora
sudo dnf install mpv curl rofi ffmpeg
```

#### Prebuilt Binary
```bash
# For x86_64:
curl -Lo curd https://github.com/RundownTrex/curd/releases/latest/download/curd-linux-x86_64

# For ARM64:
curl -Lo curd https://github.com/RundownTrex/curd/releases/latest/download/curd-linux-arm64

chmod +x curd
sudo mv curd /usr/local/bin/
```

#### Build From Source
```bash
git clone https://github.com/RundownTrex/curd.git
cd curd
go build -o curd ./cmd/curd
sudo install -m 755 curd /usr/local/bin/
```

---

## Usage

Simply run:
```bash
curd
```

### Android Playback Controls
When you select an anime and episode in Termux:
1. `mpv-android` (or `VLC`) opens in the foreground to stream your episode.
2. Switch back to or view Termux in split-screen/PiP:
   - Press **`[Enter]`**: Confirms episode completion and syncs progress to AniList / MyAnimeList.
   - Press **`[Esc]`**: Switches provider if the video is stuck, buffering, or offline.
   - Press **`[q]`**: Exits playback cleanly.

### Changing Player on Android (MPV or VLC)

You can choose between **mpv** (default) or **vlc**:

- **While running (command-line)**:
  ```bash
  curd -player vlc
  # or
  curd -player mpv
  ```

- **In configuration (`curd -e`)**:
  ```ini
  Player=vlc
  # or
  Player=mpv
  ```

### Downloading Episodes

1. Run `curd` and select **Download Episodes**.
2. Pick a provider and search for an anime.
3. Select episode(s) using `[Space]` to toggle and `[Enter]` to confirm.
4. Episodes download into your current directory.

> **Tip (Android/Termux)**: To save downloads directly to your phone's Downloads folder, run `cd ~/storage/shared/Download` before starting `curd`.

### Command Line Options

| Flag | Description | Default |
|------|-------------|---------|
| `-c` | Continue watching the last episode | - |
| `-new` | Add and search for a new anime | - |
| `-sub` | Prefer subbed anime audio | - |
| `-dub` | Prefer dubbed anime audio | - |
| `-softsub` | Prefer soft subtitles when available | - |
| `-hardsub` | Prefer hard burned-in subtitles | - |
| `-rofi` | Use Rofi selection menu (Linux desktop) | `false` |
| `-no-rofi` | Force terminal selection menu | `true` (Android) |
| `-image-preview` | Show anime cover previews (Rofi only) | `false` |
| `-skip-op` | Automatically skip anime openings via AniSkip | `true` |
| `-skip-ed` | Automatically skip anime endings via AniSkip | `true` |
| `-skip-filler` | Automatically skip filler episodes | `true` |
| `-percentage-to-mark-complete` | % watched to automatically mark completed | `85` |
| `-player` | Media player to use (`mpv` or `vlc`) | `"mpv"` |
| `-e` | Open configuration file in editor | - |
| `-change-token` | Reconfigure tracking authentication token | - |
| `-v` | Display Curd version | - |

---

## Configuration

Configurations are stored in:
- **Linux**: `~/.config/curd/curd.conf`
- **Android**: `~/.config/curd/android.conf` (falls back to `curd.conf`)

Edit the config anytime via:
```bash
curd -e
```

### Key Configuration Options

| Option | Type | Values | Description |
|--------|------|--------|-------------|
| `TrackingRemote` | Enum | `none`, `anilist`, `myanimelist`, `anilist+myanimelist` | Remote tracking service to sync progress with. |
| `Provider` | List | `stacked`, `["kickassanime"]`, `["anidb"]`, `["senshi"]`, `["megaplay"]`, `["anineko"]`, `["allanime"]` | Provider order. `stacked` tries all providers sequentially. |
| `SubOrDub` | Enum | `sub`, `dub` | Audio preference. |
| `SubStyle` | Enum | `ask`, `soft`, `hard` | Subtitle preference when both soft and hard subs are available. |
| `PercentageToMarkComplete` | Integer | `0` - `100` | Minimum percentage watched to count as complete. |
| `DiscordPresence` | Boolean | `true`, `false` | Enable/disable Discord Rich Presence (Desktop). |
| `RofiSelection` | Boolean | `true`, `false` | Enable/disable Rofi interface (Desktop). |
| `SkipOp` / `SkipEd` | Boolean | `true`, `false` | Auto-skip intro and outro via AniSkip. |
| `AndroidPlayerPackage` | String | `is.xyz.mpv` | Package name of Android player. |
| `AndroidPlayerActivity` | String | `is.xyz.mpv.MPVActivity` | Activity name of Android player. |
| `DownloadQuality` | Enum | `best`, `1080p`, `720p`, `480p` | Preferred download resolution (default: `best`). |
| `DownloadConcurrency` | Integer | `1` - `5` | Number of concurrent segment downloads (default: `3`). |

---

## Uninstallation

### Linux
```bash
sudo rm /usr/local/bin/curd
rm -rf ~/.config/curd ~/.local/share/curd
```

### Android (Termux)
```bash
rm -f $PREFIX/bin/curd
rm -rf ~/.config/curd ~/.local/share/curd
```

---

## Credits & Attribution

- **Original Project**: [Wraient/curd](https://github.com/Wraient/curd) by [Wraient](https://github.com/Wraient)
- [ani-cli](https://github.com/pystardust/ani-cli) & [jerry](https://github.com/justchokingaround/jerry) for inspiration
- [AniSkip API](https://api.aniskip.com/) for opening & ending timestamps
- [mpv](https://mpv.io/) and [mpv-android](https://github.com/mpv-android/mpv-android)
