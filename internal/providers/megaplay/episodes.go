package megaplay

import (
	"fmt"
	"strconv"
	"strings"
)

func episodesList(showID, mode string) ([]string, error) {
	malID, err := parseMalID(showID)
	if err != nil {
		return nil, err
	}
	_ = mode

	// Query AniList by MAL ID to get the episode count.
	media, err := fetchAniListByMalID(malID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch episode count for mal id %d: %w", malID, err)
	}

	count := 0
	if media.Episodes != nil {
		count = *media.Episodes
	}

	// For ongoing anime where episode count is unknown, use a generous fallback.
	// Stream resolution will naturally fail for episodes that don't exist yet.
	if count <= 0 {
		count = 2000
	}

	episodes := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		episodes = append(episodes, strconv.Itoa(i))
	}
	return episodes, nil
}

func parseMalID(showID string) (int, error) {
	showID = strings.TrimSpace(showID)
	if showID == "" {
		return 0, fmt.Errorf("empty show id")
	}
	malID, err := strconv.Atoi(showID)
	if err != nil || malID <= 0 {
		return 0, fmt.Errorf("invalid megaplay mal id %q", showID)
	}
	return malID, nil
}
