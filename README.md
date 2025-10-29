# Curd Fork

This is a fork of [Curd by Wraient](https://github.com/Wraient/curd) - A CLI application to stream anime with Anilist/MyAnimeList integration and Discord RPC.

## Changes Made in This Fork

### Discord Rich Presence Fix
- **Fixed Discord RPC to display "Watching [Anime Name]"** instead of "Watching Curd"
  - Modified `internal/discord.go` to use the `Name` field in the Activity struct
  - Activity Type remains as 3 (Watching) for proper Discord status
  - Now correctly shows the actual anime being watched (e.g., "Watching One Piece")

### Dual Tracking Integration
- **Added MAL and AniList dual tracking support**
  - Both MyAnimeList and AniList get updated simultaneously after watching episodes
  - No need to choose between services - track on both platforms at once
  - Automatic progress synchronization across both tracking services

## Building from Source

```bash
git clone https://github.com/RundownTrex/curd.git
cd curd
go build -o curd ./cmd/curd
```

## Original Repository

For full documentation, installation instructions, and usage details, please visit the original repository:
- [Wraient/curd](https://github.com/Wraient/curd)

## Credits

- [Wraient](https://github.com/Wraient) - Original author of Curd
- All contributors to the original Curd project

