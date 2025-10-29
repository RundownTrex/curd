package internal

import (
	"fmt"
	"strconv"
	"strings"
)

// TrackingService interface defines methods that both AniList and MAL must implement
type TrackingServiceInterface interface {
	GetUserID(token string) (int, string, error)
	GetUserAnimeList(token string, userID int) (map[string]interface{}, error)
	SearchAnime(query, token string) ([]SelectionOption, error)
	SearchAnimePreview(query, token string) (map[string]RofiSelectPreview, error)
	UpdateProgress(token string, mediaID, progress int) error
	UpdateStatus(token string, mediaID int, status string) error
	RateAnime(token string, mediaID int) error
	AddToWatchingList(animeID int, token string) error
	GetAnimeDetails(id int, token string) (Anime, error)
}

// GetTrackingService returns the appropriate tracking service based on config
func GetTrackingService(config *CurdConfig) string {
	service := strings.ToLower(config.TrackingService)
	if service == "mal" || service == "myanimelist" {
		return "mal"
	}
	return "anilist" // Default
}

// GetUserIDUnified gets user ID from the configured tracking service
func GetUserIDUnified(token string, config *CurdConfig) (int, string, error) {
	service := GetTrackingService(config)
	if service == "mal" {
		return GetMALUserInfo(token)
	}
	return GetAnilistUserID(token)
}

// GetUserDataUnified gets user anime list from the configured tracking service
func GetUserDataUnified(token string, userID int, config *CurdConfig, withPreview bool) (map[string]interface{}, error) {
	service := GetTrackingService(config)
	if service == "mal" {
		return GetMALUserAnimeList(token)
	}

	if withPreview {
		return GetUserDataPreview(token, userID)
	}
	return GetUserData(token, userID)
}

// SearchAnimeUnified searches for anime using the configured tracking service
func SearchAnimeUnified(query, token string, config *CurdConfig, withPreview bool) (interface{}, error) {
	service := GetTrackingService(config)
	if service == "mal" {
		if withPreview {
			return SearchAnimeMALPreview(query, token)
		}
		return SearchAnimeMAL(query, token)
	}

	if withPreview {
		return SearchAnimeAnilistPreview(query, token)
	}
	return SearchAnimeAnilist(query, token)
}

// UpdateAnimeProgressUnified updates anime progress on the configured tracking service
// If DualTracking is enabled, updates both MAL and AniList
func UpdateAnimeProgressUnified(token string, mediaID, progress int, config *CurdConfig) error {
	service := GetTrackingService(config)
	var primaryErr error

	// Update primary service
	if service == "mal" {
		primaryErr = UpdateMALAnimeProgress(token, mediaID, progress)
	} else {
		primaryErr = UpdateAnimeProgress(token, mediaID, progress)
	}

	// If dual tracking is disabled, return primary result
	if !config.DualTracking {
		return primaryErr
	}

	// Dual tracking is enabled - update the secondary service too
	// We need the tokens from the global user context
	// This will be passed via the anime/user context in the actual calls
	return primaryErr
}

// UpdateAnimeProgressDual updates progress on both services when dual tracking is enabled
func UpdateAnimeProgressDual(anilistToken, malToken string, anilistID, malID, progress int, config *CurdConfig) error {
	Log(fmt.Sprintf("UpdateAnimeProgressDual called: anilistID=%d, malID=%d, progress=%d", anilistID, malID, progress))
	Log(fmt.Sprintf("Token status: anilistToken length=%d, malToken length=%d", len(anilistToken), len(malToken)))

	if !config.DualTracking {
		Log("Dual tracking is disabled, using single service")
		// Not dual tracking, use the unified function
		service := GetTrackingService(config)
		if service == "mal" {
			return UpdateMALAnimeProgress(malToken, malID, progress)
		}
		return UpdateAnimeProgress(anilistToken, anilistID, progress)
	}

	Log("Dual tracking is enabled, updating both services")
	// Dual tracking enabled - update both services
	var errors []string

	// Update MAL
	if malToken != "" && malID > 0 {
		Log(fmt.Sprintf("Updating MAL: ID=%d, progress=%d", malID, progress))
		if err := UpdateMALAnimeProgress(malToken, malID, progress); err != nil {
			Log(fmt.Sprintf("Failed to update progress on MAL: %v", err))
			errors = append(errors, fmt.Sprintf("MAL: %v", err))
		} else {
			Log("Successfully updated progress on MAL")
			CurdOut(fmt.Sprintf("✓ MAL updated: Episode %d", progress))
		}
	} else {
		Log(fmt.Sprintf("Skipping MAL update: token empty=%v, ID=%d", malToken == "", malID))
	}

	// Update AniList
	if anilistToken != "" && anilistID > 0 {
		Log(fmt.Sprintf("Updating AniList: ID=%d, progress=%d", anilistID, progress))
		if err := UpdateAnimeProgress(anilistToken, anilistID, progress); err != nil {
			Log(fmt.Sprintf("Failed to update progress on AniList: %v", err))
			errors = append(errors, fmt.Sprintf("AniList: %v", err))
		} else {
			Log("Successfully updated progress on AniList")
			CurdOut(fmt.Sprintf("✓ AniList updated: Episode %d", progress))
		}
	} else {
		Log(fmt.Sprintf("Skipping AniList update: token empty=%v, ID=%d", anilistToken == "", anilistID))
	}

	if len(errors) > 0 {
		return fmt.Errorf("dual tracking errors: %s", strings.Join(errors, "; "))
	}
	return nil
}

// UpdateAnimeStatusUnified updates anime status on the configured tracking service
func UpdateAnimeStatusUnified(token string, mediaID int, status string, config *CurdConfig) error {
	service := GetTrackingService(config)
	if service == "mal" {
		return UpdateMALAnimeStatus(token, mediaID, status)
	}
	return UpdateAnimeStatus(token, mediaID, status)
}

// UpdateAnimeStatusDual updates status on both services when dual tracking is enabled
func UpdateAnimeStatusDual(anilistToken, malToken string, anilistID, malID int, status string, config *CurdConfig) error {
	if !config.DualTracking {
		// Not dual tracking, use the unified function
		service := GetTrackingService(config)
		if service == "mal" {
			return UpdateMALAnimeStatus(malToken, malID, status)
		}
		return UpdateAnimeStatus(anilistToken, anilistID, status)
	}

	// Dual tracking enabled - update both services
	var errors []string

	// Update MAL
	if malToken != "" && malID > 0 {
		if err := UpdateMALAnimeStatus(malToken, malID, status); err != nil {
			Log(fmt.Sprintf("Failed to update status on MAL: %v", err))
			errors = append(errors, fmt.Sprintf("MAL: %v", err))
		} else {
			Log("Successfully updated status on MAL")
		}
	}

	// Update AniList
	if anilistToken != "" && anilistID > 0 {
		if err := UpdateAnimeStatus(anilistToken, anilistID, status); err != nil {
			Log(fmt.Sprintf("Failed to update status on AniList: %v", err))
			errors = append(errors, fmt.Sprintf("AniList: %v", err))
		} else {
			Log("Successfully updated status on AniList")
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("dual tracking errors: %s", strings.Join(errors, "; "))
	}
	return nil
}

// RateAnimeUnified rates anime on the configured tracking service
func RateAnimeUnified(token string, mediaID int, config *CurdConfig) error {
	service := GetTrackingService(config)
	if service == "mal" {
		return RateAnimeMAL(token, mediaID)
	}
	return RateAnime(token, mediaID)
}

// RateAnimeDual rates anime on both services when dual tracking is enabled
func RateAnimeDual(anilistToken, malToken string, anilistID, malID int, config *CurdConfig) error {
	if !config.DualTracking {
		// Not dual tracking, use the unified function
		service := GetTrackingService(config)
		if service == "mal" {
			return RateAnimeMAL(malToken, malID)
		}
		return RateAnime(anilistToken, anilistID)
	}

	// Dual tracking enabled - update both services
	var errors []string

	// Update MAL
	if malToken != "" && malID > 0 {
		if err := RateAnimeMAL(malToken, malID); err != nil {
			Log(fmt.Sprintf("Failed to rate anime on MAL: %v", err))
			errors = append(errors, fmt.Sprintf("MAL: %v", err))
		} else {
			Log("Successfully rated anime on MAL")
		}
	}

	// Update AniList
	if anilistToken != "" && anilistID > 0 {
		if err := RateAnime(anilistToken, anilistID); err != nil {
			Log(fmt.Sprintf("Failed to rate anime on AniList: %v", err))
			errors = append(errors, fmt.Sprintf("AniList: %v", err))
		} else {
			Log("Successfully rated anime on AniList")
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("dual tracking errors: %s", strings.Join(errors, "; "))
	}
	return nil
}

// AddAnimeToWatchingListUnified adds anime to watching list on the configured tracking service
func AddAnimeToWatchingListUnified(animeID int, token string, config *CurdConfig) error {
	service := GetTrackingService(config)
	if service == "mal" {
		return AddAnimeToMALWatchingList(animeID, token)
	}
	return AddAnimeToWatchingList(animeID, token)
}

// GetAnimeDataByIDUnified gets anime details from the configured tracking service
func GetAnimeDataByIDUnified(id int, token string, config *CurdConfig) (Anime, error) {
	service := GetTrackingService(config)
	if service == "mal" {
		return GetMALAnimeDetails(id, token)
	}
	return GetAnimeDataByID(id, token)
}

// ConvertIDIfNeeded converts between MAL and AniList IDs if necessary
func ConvertIDIfNeeded(id int, fromService, toService string) (int, error) {
	if fromService == toService {
		return id, nil
	}

	if fromService == "anilist" && (toService == "mal" || toService == "myanimelist") {
		return GetAnimeMalID(id)
	}

	if (fromService == "mal" || fromService == "myanimelist") && toService == "anilist" {
		return ConvertMALIDToAnilist(id, "")
	}

	return id, fmt.Errorf("unsupported service conversion: %s to %s", fromService, toService)
}

// FindAnimeByIDUnified finds anime by ID in the unified anime list
func FindAnimeByIDUnified(list AnimeList, idStr string) (*Entry, error) {
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return nil, fmt.Errorf("invalid ID format: %s", idStr)
	}

	// Define a slice of pointers to hold categories
	categories := [][]Entry{
		list.Watching,
		list.Completed,
		list.Paused,
		list.Dropped,
		list.Planning,
		list.Rewatching,
	}

	// Iterate through each category
	for _, category := range categories {
		for _, entry := range category {
			if entry.Media.ID == id {
				return &entry, nil
			}
		}
	}

	return nil, fmt.Errorf("anime with ID %d not found", id)
}

// GetServiceName returns a user-friendly name for the tracking service
func GetServiceName(config *CurdConfig) string {
	service := GetTrackingService(config)
	if service == "mal" {
		return "MyAnimeList"
	}
	return "AniList"
}
