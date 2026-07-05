# Curd Fork

This repository is a fork of [Wraient/curd](https://github.com/Wraient/curd), a CLI app for anime playback with AniList/MyAnimeList tracking and Discord Rich Presence.

## What's New in This Fork

1. **Discord Rich Presence fix**
   - Presence now shows `Watching <Anime Name>` instead of a static app name.
2. **Dual tracking support**
   - AniList and MyAnimeList can both be updated automatically.

## Build From Source

```bash
git clone https://github.com/RundownTrex/curd.git
cd curd
go build -o curd ./cmd/curd
```

## AI-Assisted Development Notice

These changes were developed in this forked repository (`RundownTrex/curd`) with AI-assisted coding support.

## Original Project

For full upstream docs and usage details, see:
- [Wraient/curd](https://github.com/Wraient/curd)

## Credits

- [Wraient](https://github.com/Wraient) for the original project
- Contributors to the upstream Curd project
