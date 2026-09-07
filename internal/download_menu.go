package internal

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wraient/curd/internal/downloader"
)

// DownloadMenu guides the user through provider selection, anime search,
// episode multi-selection, and downloading to the current working directory.
func DownloadMenu(userCurdConfig *CurdConfig) {
	// 1. Prompt user to select provider
	selectedProvider := PromptProviderSelection()
	if selectedProvider == "" {
		return
	}

	Log(fmt.Sprintf("DownloadMenu: user selected provider %s", selectedProvider))
	CurdOut(fmt.Sprintf("\033[1;36mDownload provider:\033[0m %s", ProviderDisplayName(selectedProvider)))

	// Temporarily override provider for this download session
	origProvider := userCurdConfig.Provider
	userCurdConfig.Provider = canonicalProviderConfigValue(selectedProvider)
	CurrentProvider = nil
	defer func() {
		userCurdConfig.Provider = origProvider
		CurrentProvider = nil
	}()

	// 2. Prompt user for anime name to search
	var query string
	if userCurdConfig.RofiSelection {
		userInput, err := GetUserInputFromRofi("Enter anime name to download")
		if err != nil {
			Log("DownloadMenu: error getting user input: " + err.Error())
			return
		}
		query = userInput
	} else {
		CurdOut("Enter anime name to download:")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		query = strings.TrimSpace(input)
	}

	if query == "" {
		return
	}

	// 3. Search anime in the chosen provider
	providerID, providerName, selectedTitle, back, searchErr := ResolveUntrackedProviderSearch(userCurdConfig, query)
	if searchErr != nil {
		Log(fmt.Sprintf("DownloadMenu: search error: %v", searchErr))
		CurdOut(fmt.Sprintf("Failed to search anime: %v", searchErr))
		return
	}
	if back || providerID == "" {
		return
	}

	var anime Anime
	anime.ProviderId = providerID
	anime.ProviderName = providerName
	if selectedTitle != "" {
		anime.Title.English = selectedTitle
		anime.Title.Romaji = selectedTitle
	} else {
		anime.Title.English = query
		anime.Title.Romaji = query
	}

	// 4. Fetch episode catalog from the provider
	CurdOut(fmt.Sprintf("Fetching episode list for %q...", GetAnimeName(anime)))
	episodes, err := EpisodesList(QualifyProviderID(providerName, providerID), userCurdConfig.SubOrDub)
	if err != nil || len(episodes) == 0 {
		CurdOut(fmt.Sprintf("\033[1;31mNo episodes found for %s (%v)\033[0m", GetAnimeName(anime), err))
		fmt.Println("Press Enter to return...")
		_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
		return
	}

	// 5. Present interactive episode selection
	episodeOptions := make([]SelectionOption, 0, len(episodes))
	for _, epStr := range episodes {
		episodeOptions = append(episodeOptions, SelectionOption{
			Key:   epStr,
			Label: fmt.Sprintf("Episode %s", epStr),
		})
	}

	CurdOut(fmt.Sprintf("\033[1;36mSelect episode(s) to download:\033[0m [Space to toggle, Enter to confirm]"))
	selectedEps, err := DynamicMultiSelect(episodeOptions)
	if err != nil || len(selectedEps) == 0 {
		CurdOut("No episodes selected.")
		return
	}

	// 6. Download loop into current directory (.)
	ClearScreen()
	CurdOut(fmt.Sprintf("\033[1;32mStarting download of %d episode(s) into current directory (.)\033[0m", len(selectedEps)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		CurdOut("\n\033[1;33mDownload interrupted by user. Cleaning up...\033[0m")
		cancel()
	}()

	totalSelected := len(selectedEps)
	for i, epOpt := range selectedEps {
		select {
		case <-ctx.Done():
			return
		default:
		}

		epNum, _ := strconv.Atoi(epOpt.Key)
		if epNum <= 0 {
			continue
		}

		anime.Ep.Number = epNum
		anime.Ep.Links = nil

		safeTitle := downloader.SanitizeFileName(GetAnimeName(anime))
		fileName := fmt.Sprintf("%s - Episode %02d.mp4", safeTitle, epNum)

		// Check if file already exists
		if _, statErr := os.Stat(fileName); statErr == nil {
			fmt.Printf("\n\033[1;33m[Notice]\033[0m %s already exists. Overwrite? [y/N]: ", fileName)
			reader := bufio.NewReader(os.Stdin)
			resp, _ := reader.ReadString('\n')
			resp = strings.ToLower(strings.TrimSpace(resp))
			if resp != "y" && resp != "yes" {
				fmt.Printf("Skipping %s\n", fileName)
				continue
			}
		}

		fmt.Printf("\n\033[1;34m[%d/%d] Resolving stream for Episode %d...\033[0m\n", i+1, totalSelected, epNum)
		result, resErr := ResolveEpisodeURLForPlayback(*userCurdConfig, &anime, epNum)
		if resErr != nil || len(result.Links) == 0 {
			fmt.Printf("\033[1;31mError resolving stream for Episode %d: %v\033[0m\n", epNum, resErr)
			continue
		}

		selectedLink := PrioritizeLink(result.Links)
		anime.Ep.Links = result.Links
		applyStreamPlaybackHints(&anime, anime.Ep.Links, result.LinkHints)

		ref := strings.TrimSpace(anime.Ep.StreamReferrer)
		if ref == "" {
			ref = streamReferrerForLink(selectedLink, CurrentAnimeProviderName(&anime))
		}

		origin := ""
		if strings.EqualFold(anime.ProviderName, "kickassanime") || strings.Contains(selectedLink, "krussdomi.com") {
			origin = "https://krussdomi.com"
		}

		concurrency := userCurdConfig.DownloadConcurrency
		if concurrency <= 0 {
			concurrency = 3
		}

		quality := strings.TrimSpace(userCurdConfig.DownloadQuality)
		if quality == "" {
			quality = "best"
		}

		opts := downloader.DownloadOptions{
			URL:            selectedLink,
			Referrer:       ref,
			Origin:         origin,
			SubtitleURL:    anime.Ep.SubtitleURL,
			DestinationDir: ".",
			FileName:       fileName,
			Concurrency:    concurrency,
			Quality:        quality,
		}

		epStart := time.Now()
		dlErr := downloader.DownloadEpisode(ctx, opts)
		if dlErr != nil {
			fmt.Printf("\033[1;31mError downloading Episode %d: %v\033[0m\n", epNum, dlErr)
		}

		// Smart pacing: only pause if the episode downloaded so fast that provider APIs might be spammed
		if i+1 < totalSelected && time.Since(epStart) < 3*time.Second {
			time.Sleep(2 * time.Second)
		}
	}

	fmt.Println("\n\033[1;32mDownload session complete!\033[0m Press Enter to return to main menu...")
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
}
