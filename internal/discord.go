package internal

import (
	"fmt"
	"strings"
	"time"

	"github.com/tr1xem/go-discordrpc/client"
)

var discordClient *client.Client
var isLoggedIn bool
var lastPausedState bool
var lastEpisodeNumber int
var lastAnimeTitle string
var lastUpdateTime time.Time
var lastForceUpdateTime time.Time

func LoginClient(clientId string) error {
	if discordClient != nil && isLoggedIn {
		return nil // Already logged in
	}

	discordClient = client.NewClient(clientId)

	if err := discordClient.Login(); err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	isLoggedIn = true
	return nil
}

func DiscordPresence(anime Anime, IsPaused bool, currentPosition int, totalDuration int, clientId string) error {
	return DiscordPresenceWithForce(anime, IsPaused, currentPosition, totalDuration, clientId, false)
}

func DiscordPresenceWithForce(anime Anime, IsPaused bool, currentPosition int, totalDuration int, clientId string, forceUpdate bool) error {
	// Ensure client is logged in
	if discordClient == nil || !isLoggedIn {
		if err := LoginClient(clientId); err != nil {
			return err
		}
	}

	currentAnimeTitle := GetAnimeName(anime)
	now := time.Now()

	shouldUpdate := false

	if lastForceUpdateTime.IsZero() || time.Since(lastForceUpdateTime) >= 2*time.Minute {
		shouldUpdate = true
		lastForceUpdateTime = now
	}

	if lastUpdateTime.IsZero() ||
		lastPausedState != IsPaused ||
		lastEpisodeNumber != anime.Ep.Number ||
		lastAnimeTitle != currentAnimeTitle ||
		forceUpdate {
		shouldUpdate = true
	}

	if !shouldUpdate {
		return nil // Skip update
	}

	var timestamps *client.Timestamps
	var SmallImage = "pause-button"
	var SmallText = "pause-button"

	startTime := now.Add(-time.Duration(currentPosition) * time.Second)

	if IsPaused {
		timestamps = &client.Timestamps{
			Start: &startTime,
			End:   nil, // No end time when paused
		}
		SmallImage = "pause-button"
		SmallText = "Paused"
	} else {
		if totalDuration > 60 && totalDuration > currentPosition {
			remainingSeconds := totalDuration - currentPosition
			endTime := now.Add(time.Duration(remainingSeconds) * time.Second)
			timestamps = &client.Timestamps{
				Start: &startTime,
				End:   &endTime,
			}
		} else {
			// Duration unknown, show elapsed time only
			timestamps = &client.Timestamps{
				Start: &startTime,
				End:   nil,
			}
		}
		SmallImage = ""
		SmallText = ""
	}

	largeImage := ResolveDiscordLargeImage(anime.CoverImage)
	buttons := BuildDiscordButtons(anime.AnilistId, anime.MalId)

	err := discordClient.SetActivity(client.Activity{
		Type:       3, // Watching
		Name:       currentAnimeTitle,
		Details:    currentAnimeTitle, // Large text
		LargeImage: largeImage,
		LargeText:  currentAnimeTitle, // Would display while hovering over the large image
		State:      fmt.Sprintf("Episode %d", anime.Ep.Number),
		SmallImage: SmallImage,
		SmallText:  SmallText,
		Timestamps: timestamps,
		Buttons:    buttons,
	})

	if err != nil {
		return fmt.Errorf("failed to set Discord activity: %w", err)
	}

	lastPausedState = IsPaused
	lastEpisodeNumber = anime.Ep.Number
	lastAnimeTitle = currentAnimeTitle
	lastUpdateTime = now
	// fmt.Println("Discord presence updated!", time.Now())
	return nil
}

func LogoutClient() error {
	if discordClient != nil && isLoggedIn {
		if err := discordClient.Logout(); err != nil {
			return fmt.Errorf("logout failed: %w", err)
		}
		isLoggedIn = false
		discordClient = nil
		// fmt.Println("Discord RPC logged out!")
	}
	return nil
}

func FormatTime(seconds int) string {
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	remainingSeconds := seconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, remainingSeconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, remainingSeconds)
}

func ConvertSecondsToMinutes(seconds int) int {
	return seconds / 60
}

// ResolveDiscordLargeImage ensures the image URL passed to Discord RPC is a valid raster format (PNG/JPG/WebP/GIF).
// Discord does not render SVG images or local file paths in Rich Presence.
func ResolveDiscordLargeImage(coverImage string) string {
	largeImage := strings.TrimSpace(coverImage)
	if largeImage == "" || strings.HasSuffix(strings.ToLower(largeImage), ".svg") || (!strings.HasPrefix(largeImage, "http://") && !strings.HasPrefix(largeImage, "https://")) {
		return "https://anilist.co/img/icons/icon.png" // fallback image (Discord-compatible PNG)
	}
	return largeImage
}

// BuildDiscordButtons constructs activity buttons, only including links with valid IDs.
func BuildDiscordButtons(anilistID, malID int) []*client.Button {
	var buttons []*client.Button
	if anilistID > 0 {
		buttons = append(buttons, &client.Button{
			Label: "View on AniList",
			Url:   fmt.Sprintf("https://anilist.co/anime/%d", anilistID),
		})
	}
	if malID > 0 {
		buttons = append(buttons, &client.Button{
			Label: "View on MAL",
			Url:   fmt.Sprintf("https://myanimelist.net/anime/%d", malID),
		})
	}
	return buttons
}
