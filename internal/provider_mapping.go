package internal

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type ProviderMappingOutcome int

const (
	ProviderMappingOK ProviderMappingOutcome = iota
	ProviderMappingBack
	ProviderMappingQuit
)

type providerMappingSearchState struct {
	query         string
	allProviders  []string
	sequential    bool
	providerIndex int
}

func (s *providerMappingSearchState) activeProviders() []string {
	if !s.sequential {
		return s.allProviders
	}
	if s.providerIndex >= len(s.allProviders) {
		return nil
	}
	return []string{s.allProviders[s.providerIndex]}
}

func (s *providerMappingSearchState) currentProviderLabel() string {
	if !s.sequential {
		return "all configured providers"
	}
	if s.providerIndex >= len(s.allProviders) {
		return ""
	}
	return s.allProviders[s.providerIndex]
}

func (s *providerMappingSearchState) nextProviderLabel() string {
	if !s.sequential {
		if len(s.allProviders) > 0 {
			return s.allProviders[0]
		}
		return ""
	}
	next := s.providerIndex + 1
	if next < len(s.allProviders) {
		return s.allProviders[next]
	}
	return ""
}

func (s *providerMappingSearchState) advanceToNextProvider() bool {
	if !s.sequential {
		s.sequential = true
		s.providerIndex = 0
		return len(s.allProviders) > 0
	}
	s.providerIndex++
	return s.providerIndex < len(s.allProviders)
}

func (s *providerMappingSearchState) resetToAllProviders() {
	s.sequential = false
	s.providerIndex = 0
}

func searchAnimeForMapping(config *CurdConfig, state *providerMappingSearchState, mode string) ([]SelectionOption, error) {
	providers := state.activeProviders()
	if len(providers) == 0 {
		return nil, nil
	}
	if !state.sequential && len(providers) == len(state.allProviders) {
		return SearchAnime(state.query, mode)
	}
	return searchAnimeWithProviders(providers, state.query, mode)
}

func confirmProviderMatch(option SelectionOption, reason string) bool {
	label := option.Label
	if label == "" {
		label = option.Title
	}
	if label == "" {
		label = option.Key
	}

	CurdOut(fmt.Sprintf("Provider match found by %s: %s", reason, label))
	selected, err := DynamicSelect([]SelectionOption{
		{Key: "use", Label: "Use this match"},
		{Key: "manual", Label: "Select manually"},
	})
	if err != nil {
		Log(fmt.Sprintf("Error confirming provider match: %v", err))
		return false
	}
	return selected.Key == "use"
}

func autoMatchProviderListing(config *CurdConfig, anime *Anime, animeList []SelectionOption, userQuery string, anilistEntry *Entry) bool {
	anilistIDStr := strconv.Itoa(anime.AnilistId)
	var jikanUrls []string
	fetchedJikan := false

	anilistRegex := regexp.MustCompile(`anilistcdn/media/anime/cover/(?:large|medium)/(?:bx)?(\d+)`)
	malRegex := regexp.MustCompile(`myanimelist\.net/images/anime/[^/]+/([^/]+\.jpg)`)

	for i, option := range animeList {
		Log(fmt.Sprintf("Checking option %d: Key='%s', Label='%s', Thumbnail='%s'", i, option.Key, option.Label, option.Thumbnail))

		if strings.Contains(option.Thumbnail, "anilist.co") {
			matches := anilistRegex.FindStringSubmatch(option.Thumbnail)
			if len(matches) > 1 && matches[1] == anilistIDStr {
				anime.ProviderId = option.Key
				Log(fmt.Sprintf("Found Anilist Thumbnail match! Setting ProviderId to: %s", anime.ProviderId))
				return true
			}
		} else if strings.Contains(option.Thumbnail, "myanimelist.net") {
			matches := malRegex.FindStringSubmatch(option.Thumbnail)
			if len(matches) > 1 {
				fileName := matches[1]

				if !fetchedJikan {
					if anime.MalId == 0 {
						anime.MalId, _ = GetAnimeMalID(anime.AnilistId)
					}
					if anime.MalId != 0 {
						urls, err := FetchJikanPictures(anime.MalId)
						if err != nil {
							Log(fmt.Sprintf("Failed to fetch Jikan pictures: %v", err))
						} else {
							jikanUrls = urls
						}
					}
					fetchedJikan = true
				}

				for _, url := range jikanUrls {
					if strings.HasSuffix(url, "/"+fileName) || strings.Contains(url, fileName) {
						anime.ProviderId = option.Key
						Log(fmt.Sprintf("Found MyAnimeList Thumbnail match (%s)! Setting ProviderId to: %s", fileName, anime.ProviderId))
						return true
					}
				}
			}
		}
	}

	if bestMatch, ok := confidentProviderSearchMatch(animeList, anime, userQuery); ok {
		anime.ProviderId = bestMatch.Key
		Log(fmt.Sprintf("Found confident provider title match! Setting ProviderId to: %s (%s)", anime.ProviderId, bestMatch.Label))
		return true
	}


	if anilistEntry != nil {
		targetLabel := fmt.Sprintf("%v (%d episodes)", userQuery, anilistEntry.Media.Episodes)
		for _, option := range animeList {
			if fmt.Sprintf("%s (%d episodes)", option.Title, anilistEntry.Media.Episodes) == targetLabel {
				if confirmProviderMatch(option, "title and episode count") {
					anime.ProviderId = option.Key
					Log(fmt.Sprintf("User confirmed exact text match. Setting ProviderId to: %s", anime.ProviderId))
					return true
				}
				break
			}
		}
		Log(fmt.Sprintf("No exact match found for label '%s'. Will require manual selection.", targetLabel))
	}

	return anime.ProviderId != ""
}

func promptProviderSearchRecovery(config *CurdConfig, state *providerMappingSearchState, reason string) (action string, err error) {
	options := []SelectionOption{
		{Key: "custom", Label: "Search with a different name"},
	}
	if next := state.nextProviderLabel(); next != "" {
		options = append(options, SelectionOption{
			Key:   "next_provider",
			Label: fmt.Sprintf("Try next provider (%s)", next),
		})
	}
	options = append(options, SelectionOption{Key: "back", Label: "Back to menu"})

	message := reason
	if message == "" {
		message = fmt.Sprintf("No results found for '%s' on %s.", state.query, state.currentProviderLabel())
	}
	CurdOut(message)

	selected, err := DynamicSelect(options)
	if err != nil {
		return "", err
	}
	if selected.Key == "-1" {
		return "quit", nil
	}
	if selected.Key == "-2" {
		return "back", nil
	}
	return selected.Key, nil
}

func promptProviderMatchRecovery(config *CurdConfig, state *providerMappingSearchState) (action string, err error) {
	options := []SelectionOption{
		{Key: "pick", Label: "Pick from search results"},
		{Key: "custom", Label: "Search with a different name"},
	}
	if next := state.nextProviderLabel(); next != "" {
		options = append(options, SelectionOption{
			Key:   "next_provider",
			Label: fmt.Sprintf("Try next provider (%s)", next),
		})
	}
	options = append(options, SelectionOption{Key: "back", Label: "Back to menu"})

	CurdOut("We didn't find an automatic provider match.")
	selected, err := DynamicSelect(options)
	if err != nil {
		return "", err
	}
	if selected.Key == "-1" {
		return "quit", nil
	}
	if selected.Key == "-2" {
		return "back", nil
	}
	return selected.Key, nil
}

func applySelectedProviderMapping(config *CurdConfig, anime *Anime, selected SelectionOption) {
	anime.ProviderId = selected.Key
	if providerName, rawProviderID, ok := ParseProviderQualifiedID(anime.ProviderId); ok {
		anime.ProviderName = providerName
		anime.ProviderId = rawProviderID
	} else {
		anime.ProviderName = configuredProviderNames(config)[0]
	}
}

func promptCustomProviderSearchQuery(config *CurdConfig, currentQuery, hint string) (string, bool, bool, error) {
	manualQuery, err := promptText(config, hint, true)
	if err != nil {
		return "", false, false, err
	}
	if manualQuery == "" {
		manualQuery = currentQuery
	}
	return manualQuery, false, false, nil
}

func ResolveAnimeProviderMapping(config *CurdConfig, anime *Anime, query string, anilistEntry *Entry) (ProviderMappingOutcome, error) {
	return resolveAnimeProviderMapping(config, anime, query, anilistEntry, config.ManualProviderSearch)
}

func collectCandidateQueries(primaryQuery string, anime *Anime, anilistEntry *Entry) []string {
	seen := make(map[string]struct{})
	var candidates []string

	add := func(q string) {
		q = strings.TrimSpace(q)
		if q == "" {
			return
		}
		key := strings.ToLower(q)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			candidates = append(candidates, q)
		}
	}

	add(primaryQuery)
	if anime != nil {
		add(GetAnimeName(*anime))
		add(anime.Title.English)
		add(anime.Title.Romaji)
		add(anime.Title.Japanese)
	}
	if anilistEntry != nil {
		add(anilistEntry.Media.Title.English)
		add(anilistEntry.Media.Title.Romaji)
		add(anilistEntry.Media.Title.Japanese)
	}

	for _, c := range append([]string(nil), candidates...) {
		lower := strings.ToLower(c)
		for _, p := range []string{"kabushiki gaisha ", "kabushikigaisha ", "gekijouban ", "shin ", "eiga "} {
			if strings.HasPrefix(lower, p) {
				add(c[len(p):])
			}
		}
		if strings.Contains(c, "-") {
			add(strings.ReplaceAll(c, "-", " "))
		}
		if strings.Contains(c, " ") {
			add(strings.ReplaceAll(c, " ", "-"))
		}
	}

	if len(candidates) == 0 && primaryQuery != "" {
		candidates = []string{primaryQuery}
	}
	return candidates
}

func resolveAnimeProviderMapping(config *CurdConfig, anime *Anime, query string, anilistEntry *Entry, manualOnly bool) (ProviderMappingOutcome, error) {
	candidates := collectCandidateQueries(query, anime, anilistEntry)
	candidateIdx := 0

	state := &providerMappingSearchState{
		query:        candidates[0],
		allProviders: configuredProviderNames(config),
	}

	for {
		Log(fmt.Sprintf("Searching for anime with query: %s, SubOrDub: %s, scope: %s", state.query, config.SubOrDub, state.currentProviderLabel()))

		animeList, err := searchAnimeForMapping(config, state, config.SubOrDub)
		if err != nil || len(animeList) == 0 {
			if candidateIdx+1 < len(candidates) {
				candidateIdx++
				state.query = candidates[candidateIdx]
				Log(fmt.Sprintf("No results for previous query, trying alternate candidate %d/%d: %q", candidateIdx+1, len(candidates), state.query))
				continue
			}

			errMsg := ""
			if err != nil {
				Log(fmt.Sprintf("Provider search failed: %v", err))
				errMsg = fmt.Sprintf("Provider search failed for '%s': %v", query, err)
			}
			action, actionErr := promptProviderSearchRecovery(config, state, errMsg)
			if actionErr != nil {
				return ProviderMappingQuit, actionErr
			}
			outcome, cont, actionErr := handleProviderMappingAction(config, state, action, query)
			if actionErr != nil {
				return ProviderMappingQuit, actionErr
			}
			if !cont {
				return outcome, nil
			}
			candidates = collectCandidateQueries(state.query, anime, anilistEntry)
			candidateIdx = 0
			continue
		}

		anime.ProviderId = ""
		if !manualOnly && autoMatchProviderListing(config, anime, animeList, state.query, anilistEntry) {
			if providerName, rawProviderID, ok := ParseProviderQualifiedID(anime.ProviderId); ok {
				anime.ProviderName = providerName
				anime.ProviderId = rawProviderID
			} else {
				anime.ProviderName = configuredProviderNames(config)[0]
			}
			return ProviderMappingOK, nil
		}

		if !manualOnly && candidateIdx+1 < len(candidates) {
			candidateIdx++
			state.query = candidates[candidateIdx]
			Log(fmt.Sprintf("No confident match for previous query, trying alternate candidate %d/%d: %q", candidateIdx+1, len(candidates), state.query))
			continue
		}

		if manualOnly {
			for {
				selected, selectErr := DynamicSelect(animeList)
				if selectErr != nil {
					Log(fmt.Sprintf("Failed to select anime: %v", selectErr))
				} else {
					switch selected.Key {
					case "-1":
						// fall through to recovery menu
					case "-2":
						return ProviderMappingBack, nil
					default:
						applySelectedProviderMapping(config, anime, selected)
						return ProviderMappingOK, nil
					}
				}

				action, actionErr := promptProviderMatchRecovery(config, state)
				if actionErr != nil {
					return ProviderMappingQuit, actionErr
				}
				switch action {
				case "pick":
					continue
				case "custom", "next_provider":
					outcome, cont, handleErr := handleProviderMappingAction(config, state, action, query)
					if handleErr != nil {
						return ProviderMappingQuit, handleErr
					}
					if !cont {
						return outcome, nil
					}
					candidates = collectCandidateQueries(state.query, anime, anilistEntry)
					candidateIdx = 0
					break
				case "back":
					return ProviderMappingBack, nil
				case "quit":
					return ProviderMappingQuit, nil
				default:
					continue
				}
				break
			}
			continue
		}

		for {
			action, actionErr := promptProviderMatchRecovery(config, state)
			if actionErr != nil {
				return ProviderMappingQuit, actionErr
			}

			switch action {
			case "pick":
				CurdOut("Select the correct anime from the search results.")
				selected, selectErr := DynamicSelect(animeList)
				if selectErr != nil {
					Log(fmt.Sprintf("Failed to select anime: %v", selectErr))
					continue
				}
				switch selected.Key {
				case "-1":
					continue
				case "-2":
					return ProviderMappingBack, nil
				default:
					applySelectedProviderMapping(config, anime, selected)
					return ProviderMappingOK, nil
				}
			case "custom", "next_provider":
				outcome, cont, handleErr := handleProviderMappingAction(config, state, action, query)
				if handleErr != nil {
					return ProviderMappingQuit, handleErr
				}
				if !cont {
					return outcome, nil
				}
				candidates = collectCandidateQueries(state.query, anime, anilistEntry)
				candidateIdx = 0
				break
			case "back":
				return ProviderMappingBack, nil
			case "quit":
				return ProviderMappingQuit, nil
			default:
				continue
			}
			break
		}
	}
}



func handleProviderMappingAction(config *CurdConfig, state *providerMappingSearchState, action, defaultQuery string) (ProviderMappingOutcome, bool, error) {
	switch action {
	case "custom":
		hint := fmt.Sprintf("Enter a search name for configured providers (current: '%s').", state.query)
		newQuery, _, _, err := promptCustomProviderSearchQuery(config, state.query, hint)
		if err != nil {
			return ProviderMappingQuit, false, err
		}
		state.query = newQuery
		state.resetToAllProviders()
		return ProviderMappingOK, true, nil
	case "next_provider":
		if !state.advanceToNextProvider() {
			CurdOut("No more providers left to try.")
			action, err := promptProviderSearchRecovery(config, state, "No more providers left to try.")
			if err != nil {
				return ProviderMappingQuit, false, err
			}
			return handleProviderMappingAction(config, state, action, defaultQuery)
		}
		return ProviderMappingOK, true, nil
	case "back":
		return ProviderMappingBack, false, nil
	case "quit":
		return ProviderMappingQuit, false, nil
	default:
		return ProviderMappingOK, true, nil
	}
}

func untrackedProviderSearchSelection(config *CurdConfig, query string, animeList []SelectionOption) (SelectionOption, error) {
	return DynamicSelect(animeList)
}

func RemapAnimeProviderOnEpisodeFailure(config *CurdConfig, anime *Anime, anilistEntry *Entry) bool {
	query := GetAnimeName(*anime)
	if query == "" {
		query = anime.Title.Romaji
	}
	if query == "" {
		query = anime.Title.English
	}
	if query == "" {
		return false
	}

	CurdOut("Could not get an episode link with the current provider mapping.")
	anime.ProviderId = ""
	anime.ProviderName = ""
	anime.Ep.NextEpisode = NextEpisode{}

	outcome, err := ResolveAnimeProviderMapping(config, anime, query, anilistEntry)
	if err != nil {
		Log(fmt.Sprintf("Provider remap failed: %v", err))
		return false
	}
	return outcome == ProviderMappingOK
}

func promptEpisodeLinkFailureRecovery(config *CurdConfig) string {
	selected, err := DynamicSelect([]SelectionOption{
		{Key: "remap", Label: "Search providers again"},
		{Key: "episode", Label: "Try a different episode number"},
		{Key: "quit", Label: "Cancel playback"},
	})
	if err != nil {
		return "quit"
	}
	if selected.Key == "-1" || selected.Key == "-2" {
		return "quit"
	}
	return selected.Key
}

func ResolveUntrackedProviderSearch(config *CurdConfig, initialQuery string) (providerID, providerName, selectedTitle string, back bool, err error) {
	state := &providerMappingSearchState{
		query:        initialQuery,
		allProviders: configuredProviderNames(config),
	}

	for {
		animeList, searchErr := searchAnimeForMapping(config, state, config.SubOrDub)
		if searchErr != nil {
			Log(fmt.Sprintf("Provider search failed: %v", searchErr))
			action, actionErr := promptProviderSearchRecovery(config, state, fmt.Sprintf("Provider search failed for '%s': %v", state.query, searchErr))
			if actionErr != nil {
				return "", "", "", false, actionErr
			}
			if action == "back" {
				return "", "", "", true, nil
			}
			if action == "quit" {
				return "", "", "", false, nil
			}
			_, cont, handleErr := handleProviderMappingAction(config, state, action, initialQuery)
			if handleErr != nil {
				return "", "", "", false, handleErr
			}
			if !cont {
				return "", "", "", false, nil
			}
			continue
		}

		if len(animeList) == 0 {
			action, actionErr := promptProviderSearchRecovery(config, state, "")
			if actionErr != nil {
				return "", "", "", false, actionErr
			}
			if action == "back" {
				return "", "", "", true, nil
			}
			if action == "quit" {
				return "", "", "", false, nil
			}
			_, cont, handleErr := handleProviderMappingAction(config, state, action, initialQuery)
			if handleErr != nil {
				return "", "", "", false, handleErr
			}
			if !cont {
				return "", "", "", false, nil
			}
			continue
		}

		selected, selectErr := untrackedProviderSearchSelection(config, state.query, animeList)
		if selectErr != nil {
			Log(fmt.Sprintf("Failed to select anime: %v", selectErr))
			action, actionErr := promptProviderMatchRecovery(config, state)
			if actionErr != nil {
				return "", "", "", false, actionErr
			}
			if action == "back" {
				return "", "", "", true, nil
			}
			if action == "quit" {
				return "", "", "", false, nil
			}
			_, cont, handleErr := handleProviderMappingAction(config, state, action, initialQuery)
			if handleErr != nil {
				return "", "", "", false, handleErr
			}
			if !cont {
				return "", "", "", false, nil
			}
			continue
		}

		switch selected.Key {
		case "-1":
			action, actionErr := promptProviderMatchRecovery(config, state)
			if actionErr != nil {
				return "", "", "", false, actionErr
			}
			if action == "back" {
				return "", "", "", true, nil
			}
			if action == "quit" {
				return "", "", "", false, nil
			}
			_, cont, handleErr := handleProviderMappingAction(config, state, action, initialQuery)
			if handleErr != nil {
				return "", "", "", false, handleErr
			}
			if !cont {
				return "", "", "", false, nil
			}
			continue
		case "-2":
			return "", "", "", true, nil
		default:
			
			// Clean up the label to get the pure title if it has metadata like "[provider]" or " — TV"
			cleanTitle := selected.Title
			if cleanTitle == "" {
				cleanTitle = selected.Label
				if idx := strings.Index(cleanTitle, " — "); idx != -1 {
					cleanTitle = cleanTitle[:idx]
				}
			}
			// Strip off the provider tag if it snuck in anyway
			if idx := strings.LastIndex(cleanTitle, " ["); idx != -1 && strings.HasSuffix(cleanTitle, "]") {
				cleanTitle = cleanTitle[:idx]
			}
			if name, rawID, ok := ParseProviderQualifiedID(selected.Key); ok {
				return rawID, name, cleanTitle, false, nil
			}
			return selected.Key, configuredProviderNames(config)[0], cleanTitle, false, nil
		}
	}
}
