# Curd - Provider Development Rules

This file documents critical knowledge, quirks, and patterns for the `curd` project regarding anime providers. Please adhere to these rules when modifying or adding new providers.

## 1. Cloudflare Bypass (User-Agent)
Many providers (e.g., `senshi`, `anineko`, `anipub`) sit behind strict Cloudflare WAFs. Using outdated or default Go HTTP Client `User-Agent` strings will result in instant blocks or `403/404` errors.
- **Rule**: ALWAYS use a modern `User-Agent` string for provider requests.
- **Current Standard**: `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36` (matching `megaplay`).

## 2. API Search Punctuation Sensitivity
The search APIs for `anineko` and `anipub` are heavily bugged when it comes to punctuation. If you send a search query containing an apostrophe (`'`), their APIs will crash or return empty results (`0 results`).
- **Rule**: For `anineko` and `anipub` (and any future providers with similar behavior), you MUST explicitly strip apostrophes (`strings.ReplaceAll(query, "'", "")`) from the query *before* URL encoding and sending the search request.

## 3. Fallback Mechanism & Pure Titles
When `curd` fails to find a stream on one provider, it will fall back to the next provider in the chain. To do this, it searches the fallback provider using the anime's title.
- **Rule**: When selecting an anime from a provider (e.g. in `internal/provider_mapping.go`), the `SelectionOption.Title` MUST be the pure, clean name of the anime. It must NOT contain provider metadata tags like `[mkissa]` or episode counts like `(25 episodes)`. If dirty metadata is saved, the fallback search will search for `"Anime Name [mkissa]"` and fail across all providers.

## 4. Graceful Fallbacks (No Regex Panics)
If a provider resolves an external stream URL that `curd` or `yt-dlp` cannot play (e.g. `play.bunnycdn.to` on `anipub`), do NOT use hardcoded regexes to silently filter them or crash the application.
- **Rule**: If an embed URL is unplayable, return a descriptive Go error (e.g., `fmt.Errorf("unsupported external embed...")`). This allows the main playback loop in `curd` to gracefully catch the error and automatically trigger the fallback chain to seamlessly switch to another provider.

## 5. Soft Subtitles (Senshi Provider)
Most providers (`anineko`, `megaplay`) provide subtitle `.vtt` tracks directly in the main response. `senshi` does **not**.
- **Rule**: When fetching streams from `senshi` for a soft-sub mode, the stream `status` might say `HardSub` or similar for the raw video. To actually get the subtitles, you must:
  1. Inspect the `serverFM` field in the response.
  2. Parse the `sub.info` query parameter from the `serverFM` URL, which points to a Filemoon JSON file.
  3. Perform a `fetchJSON` request on that `sub.info` URL to get the array of subtitles.
  4. Iterate through the array to find the English (or default) track and explicitly assign it to `StreamPlaybackHint.Subtitle` so `mpv` can load it.

## 6. HTTP Client Usage (`curdhost`)
Do NOT use `http.DefaultClient` or `http.Get` directly in provider code.
- **Rule**: Always use `curdhost.HTTPClient()` when creating requests (e.g., `curdhost.HTTPClient().Do(req)`). This ensures that any user-configured proxies, timeouts, and connection settings are globally respected across all providers.

## 7. URL Encoding
Many anime titles contain special characters, spaces, and non-ASCII text. 
- **Rule**: When building API request URLs, always use `url.QueryEscape` for query parameters and `url.PathEscape` for path segments. Never interpolate raw strings directly into URLs.

## 8. Unique Keys in SelectionOptions
The `SelectionOption` struct returned by `SearchAnime` is critical for tracking.
- **Rule**: The `Key` field MUST be a unique identifier for the anime within that specific provider (e.g., a slug, a numeric ID, or a path). This `Key` is saved in the local database and used in subsequent `GetEpisodes` and `ResolveStream` calls. If the `Key` changes dynamically, local tracking will break.

## 9. Error Wrapping & Graceful Degradation
Do not `panic` anywhere in the provider code. Providers are just plugins to the main `curd` loop.
- **Rule**: Always wrap errors with context (e.g., `fmt.Errorf("parse anipub response: %w", err)`). If a provider fails, `curd` will log the wrapped error and automatically degrade to the next provider. Panics will crash the entire application and ruin the user experience.
