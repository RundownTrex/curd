package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/wraient/curd/internal"
	_ "github.com/wraient/curd/internal/loadproviders"
)

var version string // Will be set by ldflags during build

func main() {
	var anime internal.Anime
	var user internal.User

	internal.SetGlobalAnime(&anime)

	var homeDir string
	if runtime.GOOS == "windows" {
		homeDir = os.Getenv("USERPROFILE")
	} else {
		homeDir = os.Getenv("HOME")
	}

	configFilePath := filepath.Join(homeDir, ".config", "curd", "curd.conf")

	// load curd userCurdConfig
	userCurdConfig, err := internal.LoadConfig(configFilePath)
	if err != nil {
		fmt.Println("Error loading config:", err)
		return
	}
	internal.SetGlobalConfig(&userCurdConfig)
	internal.GlobalConfigPath = configFilePath

	logFile := filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "debug.log")
	internal.SetGlobalLogFile(logFile)
	internal.ClearLogFile(logFile)

	// Flags configured here cause userconfig needs to be changed.
	flag.StringVar(&userCurdConfig.Player, "player", userCurdConfig.Player, "Player to use for playback (Only mpv supported currently)")
	flag.StringVar(&userCurdConfig.StoragePath, "storage-path", userCurdConfig.StoragePath, "Path to the storage directory")
	flag.StringVar(&userCurdConfig.SubsLanguage, "subs-lang", userCurdConfig.SubsLanguage, "Subtitles language")
	flag.IntVar(&userCurdConfig.PercentageToMarkComplete, "percentage-to-mark-complete", userCurdConfig.PercentageToMarkComplete, "Percentage to mark episode as complete")

	// Boolean flags that accept true/false
	flag.BoolVar(&userCurdConfig.NextEpisodePrompt, "next-episode-prompt", userCurdConfig.NextEpisodePrompt, "Prompt for the next episode (true/false)")
	flag.BoolVar(&userCurdConfig.SkipOp, "skip-op", userCurdConfig.SkipOp, "Skip opening (true/false)")
	flag.BoolVar(&userCurdConfig.SkipEd, "skip-ed", userCurdConfig.SkipEd, "Skip ending (true/false)")
	flag.BoolVar(&userCurdConfig.SkipFiller, "skip-filler", userCurdConfig.SkipFiller, "Skip filler episodes (true/false)")
	flag.BoolVar(&userCurdConfig.SkipRecap, "skip-recap", userCurdConfig.SkipRecap, "Skip recap (true/false)")
	flag.BoolVar(&userCurdConfig.ScoreOnCompletion, "score-on-completion", userCurdConfig.ScoreOnCompletion, "Score on episode completion (true/false)")
	flag.BoolVar(&userCurdConfig.SaveMpvSpeed, "save-mpv-speed", userCurdConfig.SaveMpvSpeed, "Save MPV speed setting (true/false)")
	flag.BoolVar(&userCurdConfig.DiscordPresence, "discord-presence", userCurdConfig.DiscordPresence, "Enable Discord presence (true/false)")
	flag.StringVar(&userCurdConfig.DiscordClientId, "discord-client-id", userCurdConfig.DiscordClientId, "Discord client ID for Rich Presence")
	continueLast := flag.Bool("c", false, "Continue last episode")
	addNewAnime := flag.Bool("new", false, "Add new anime")
	rofiSelection := flag.Bool("rofi", false, "Open selection in rofi")
	noRofi := flag.Bool("no-rofi", false, "No rofi")
	imagePreview := flag.Bool("image-preview", false, "Show image preview")
	noImagePreview := flag.Bool("no-image-preview", false, "No image preview")
	changeToken := flag.Bool("change-token", false, "Change token")
	changeMALToken := flag.Bool("change-mal-token", false, "Change MyAnimeList token")
	currentCategory := flag.Bool("current", false, "Current category")
	updateScript := flag.Bool("u", false, "Update the script")
	editConfig := flag.Bool("e", false, "Edit config")
	subFlag := flag.Bool("sub", false, "Watch sub version")
	dubFlag := flag.Bool("dub", false, "Watch dub version")
	versionFlag := flag.Bool("v", false, "Print version information")

	// Custom help/usage function
	flag.Usage = func() {
		internal.RestoreScreen()
		fmt.Fprintf(os.Stderr, "Curd is a CLI tool to manage anime playback with advanced features like skipping intro, outro, filler, recap, tracking progress, and integrating with Discord.\n")
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults() // This prints the default flag information
	}

	flag.Parse()

	// Check version before screen clearing
	if *versionFlag {
		internal.RestoreScreen()
		if version == "" {
			version = "2.0.0"
		}
		fmt.Printf("Curd version: %s\n", version)
		os.Exit(0)
	}

	anime.Ep.ContinueLast = *continueLast

	if *updateScript {
		repo := "wraient/curd"
		fileName := "curd"

		if err := internal.UpdateCurd(repo, fileName); err != nil {
			internal.CurdOut(fmt.Sprintf("Error updating executable: %v\n", err))
			internal.ExitCurd(err)
		} else {
			internal.CurdOut("Program Updated!")
			internal.ExitCurd(nil)
		}
	}

	if *changeToken {
		internal.ChangeToken(&userCurdConfig, &user)
		return
	}

	if *changeMALToken {
		internal.ChangeMALToken(&userCurdConfig, &user)
		return
	}

	// Setup screen for interactive mode (only if not changing token)
	internal.ClearScreen()
	defer internal.RestoreScreen()

	if *currentCategory {
		userCurdConfig.CurrentCategory = true
	}

	if *rofiSelection {
		userCurdConfig.RofiSelection = true
	}

	if *noRofi || runtime.GOOS == "windows" {
		userCurdConfig.RofiSelection = false
	}

	if *imagePreview {
		userCurdConfig.ImagePreview = true
	}

	if *noImagePreview || runtime.GOOS == "windows" {
		userCurdConfig.ImagePreview = false
	}

	if *editConfig {
		internal.EditConfig(configFilePath)
		return
	}

	// Set SubOrDub based on the flags
	if *subFlag {
		userCurdConfig.SubOrDub = "sub"
	} else if *dubFlag {
		userCurdConfig.SubOrDub = "dub"
	}

	// Determine which tracking service to use
	trackingService := internal.GetTrackingService(&userCurdConfig)

	// Get the appropriate token based on tracking service (for displaying anime list)
	if trackingService == "mal" || trackingService == "myanimelist" {
		user.Token, err = internal.GetMALTokenFromFile(filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "mal_token.json"))
		if err != nil {
			internal.Log("Error reading MAL token")
		}
		if user.Token == "" {
			internal.ChangeMALToken(&userCurdConfig, &user)
		}
		user.MalToken = user.Token
	} else {
		// Default to AniList
		user.Token, err = internal.GetTokenFromFile(filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "anilist_token.json"))
		if err != nil {
			internal.Log("Error reading AniList token")
		}
		if user.Token == "" {
			internal.ChangeToken(&userCurdConfig, &user)
		}
		user.AnilistToken = user.Token
	}

	// If dual tracking is enabled, load the secondary token as well
	if userCurdConfig.DualTracking {
		internal.Log("Dual tracking is enabled in config")
		if trackingService == "mal" || trackingService == "myanimelist" {
			// Primary is MAL, load AniList as secondary
			internal.Log("Primary service is MAL, loading AniList token as secondary...")
			anilistToken, err := internal.GetTokenFromFile(filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "anilist_token.json"))
			if err != nil {
				internal.Log(fmt.Sprintf("Dual tracking enabled but AniList token not found: %v", err))
				internal.CurdOut("Warning: Dual tracking enabled but AniList token not found. Run './curd -change-token' to set up AniList.")
			} else {
				user.AnilistToken = anilistToken
				internal.Log(fmt.Sprintf("AniList token loaded successfully (length: %d)", len(anilistToken)))
				internal.Log("Dual tracking enabled: will update both MAL and AniList")
			}
		} else {
			// Primary is AniList, load MAL as secondary
			internal.Log("Primary service is AniList, loading MAL token as secondary...")
			malToken, err := internal.GetMALTokenFromFile(filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "mal_token.json"))
			if err != nil {
				internal.Log(fmt.Sprintf("Dual tracking enabled but MAL token not found: %v", err))
				internal.CurdOut("Warning: Dual tracking enabled but MAL token not found. Run './curd -change-mal-token' to set up MAL.")
			} else {
				user.MalToken = malToken
				internal.Log(fmt.Sprintf("MAL token loaded successfully (length: %d)", len(malToken)))
				internal.Log("Dual tracking enabled: will update both AniList and MAL")
			}
		}
	} else {
		internal.Log("Dual tracking is disabled in config")
	}

	if userCurdConfig.RofiSelection {
		// Define a slice of file names to check and download
		filesToCheck := []string{
			"selectanimepreview.rasi",
			"selectanime.rasi",
			"userinput.rasi",
		}

		// Call the function to check and download files
		err := internal.CheckAndDownloadFiles(os.ExpandEnv(userCurdConfig.StoragePath), filesToCheck)
		if err != nil {
			internal.Log(fmt.Sprintf("Error checking and downloading files: %v\n", err))
			internal.CurdOut(fmt.Sprintf("Error checking and downloading files: %v\n", err))
			internal.ExitCurd(err)
		}
	}

	// Load animes in database
	databaseFile := filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "curd_history.txt")
	databaseAnimes := internal.LocalGetAllAnime(databaseFile)

	if *addNewAnime {
		internal.AddNewAnime(&userCurdConfig, &anime, &user, &databaseAnimes)
		// internal.ExitCurd(fmt.Errorf("Added new anime!"))
	}

	internal.SetupCurd(&userCurdConfig, &anime, &user, &databaseAnimes, databaseFile)
	trackingService = internal.GetTrackingService(&userCurdConfig)

	// Find anime in user's list using the correct ID based on tracking service
	var idToFind string
	if trackingService == "mal" || trackingService == "myanimelist" {
		idToFind = strconv.Itoa(anime.MalId)
	} else {
		idToFind = strconv.Itoa(anime.AnilistId)
	}

	temp_anime, err := internal.FindAnimeByAnilistID(user.AnimeList, idToFind)
	if err != nil {
		internal.Log("Error finding anime in user list: " + err.Error())
	}

	if temp_anime != nil && anime.TotalEpisodes == temp_anime.Progress {
		internal.Log(temp_anime.Progress)
		internal.Log(anime.TotalEpisodes)
		internal.Log(user.AnimeList)
		internal.Log("Rewatching anime: " + internal.GetAnimeName(anime))
		anime.Rewatching = true
	}

	anime.Ep.Player.Speed = 1.0

	// Get filler list concurrently
	go func() {
		// Get MAL ID first if not already set
		if anime.MalId == 0 {
			malID, err := internal.GetAnimeMalID(anime.AnilistId)
			if err != nil {
				internal.Log("Error getting MAL ID: " + err.Error())
				return
			}
			anime.MalId = malID
		}

		fillerList, err := internal.FetchFillerEpisodes(anime.MalId)
		if err != nil {
			internal.Log("Error getting filler list: " + err.Error())
		} else {
			anime.FillerEpisodes = fillerList
			internal.Log("Filler list fetched successfully")
			// fmt.Println("Filler episodes: ", anime.FillerEpisodes)
		}
	}()

	// Main loop (loop to keep starting new episodes)
	retryProvider := false
	for {
		anime.Ep.Started = false
		anime.Ep.IsCompleted = false
		internal.Log(anime)

		// Create a channel to signal when to exit the skip loop
		var wg sync.WaitGroup
		skipLoopDone := make(chan struct{})
		skipLoopClosed := make(chan bool, 1)
		skipLoopClosed <- false
		// retryProviderCh: true = retry provider for same episode, false = normal completion
		retryProviderCh := make(chan bool, 1)

		if retryProvider {
			// Re-prompt provider selection and clear old links
			selectedProvider := internal.PromptProviderSelection()
			if selectedProvider == "" {
				internal.ExitCurd(nil)
				return
			}
			internal.Log(fmt.Sprintf("Retry: user selected provider: %s (current: %s)", selectedProvider, anime.ProviderName))
			internal.CurdOut(fmt.Sprintf("\033[1;36mUser explicitly selected provider: %s\033[0m", selectedProvider))
			if selectedProvider != anime.ProviderName {
				anime.ProviderName = selectedProvider
				anime.ProviderId = ""                         // Force re-search on the chosen provider
				anime.Ep.NextEpisode = internal.NextEpisode{} // Clear any prefetched episode from the old provider
			}
			userCurdConfig.Provider = selectedProvider
			anime.Ep.Links = nil // Clear old links so they are re-fetched

			// Set to false so that the link-fetching block below executes
			retryProvider = false
		}

		if !retryProvider {
			// Get MalId and CoverImage (only if discord presence is enabled)
			if userCurdConfig.DiscordPresence {
				if trackingService == "mal" || trackingService == "myanimelist" {
					if anime.MalId == 0 {
						anime.MalId = anime.AnilistId
					}
				} else {
					anime.MalId, anime.CoverImage, err = internal.GetAnimeIDAndImage(anime.AnilistId)
					if err != nil {
						internal.Log("Error getting anime ID and image: " + err.Error())
					}
				}
				// Skip initial Discord presence - wait for MPV to provide real duration
				// This avoids showing the default 25-minute duration before the video starts
				internal.Log("Waiting for MPV to start to get actual video duration before showing Discord presence")
			} else if anime.MalId == 0 {
				if trackingService == "mal" || trackingService == "myanimelist" {
					anime.MalId = anime.AnilistId
				} else {
					anime.MalId, err = internal.GetAnimeMalID(anime.AnilistId)
					if err != nil {
						internal.Log("Error getting anime MAL ID: " + err.Error())
					}
				}
			}

			// Skip filler/recap loop
			for {
				err = internal.GetEpisodeData(anime.MalId, anime.Ep.Number, &anime)
				if err != nil {
					internal.Log("Error getting episode data, assuming non-filler: " + err.Error())
					break
				}

				anime.Ep.IsFiller = internal.IsEpisodeFiller(anime.FillerEpisodes, anime.Ep.Number)

				if !((anime.Ep.IsFiller && userCurdConfig.SkipFiller) || (anime.Ep.IsRecap && userCurdConfig.SkipRecap)) {
					if anime.Ep.LastWasSkipped && !anime.Rewatching {
						go internal.UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, anime.Ep.Number-1, &userCurdConfig)
					}
					break
				}

				if anime.Ep.IsFiller && userCurdConfig.SkipFiller {
					internal.CurdOut(fmt.Sprint("Filler episode, skipping: ", anime.Ep.Number))
					anime.Ep.Number = internal.GetNextCanonEpisode(anime.FillerEpisodes, anime.Ep.Number)
				} else {
					internal.CurdOut(fmt.Sprint("Recap episode, skipping: ", anime.Ep.Number))
					anime.Ep.Number++
				}

				anime.Ep.LastWasSkipped = true
				anime.Ep.Started = false
				internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, 0, 0, internal.GetAnimeName(anime), anime.ProviderName)

				if !anime.Rewatching {
					go internal.UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, anime.Ep.Number-1, &userCurdConfig)
				}

				if anime.Ep.Number > anime.TotalEpisodes {
					internal.CurdOut("Reached end of series")
					internal.ExitCurd(nil)
				}
			}

			// Fetch links if not already set
			if len(anime.Ep.Links) == 0 {
				// Wait up to 5s for prefetched links
				for i := 0; i < 5; i++ {
					if anime.Ep.NextEpisode.Number == anime.Ep.Number && len(anime.Ep.NextEpisode.Links) > 0 && anime.Ep.NextEpisode.ProviderName == anime.ProviderName {
						internal.Log("Using prefetched next episode link")
						anime.Ep.Links = anime.Ep.NextEpisode.Links
						break
					}
					time.Sleep(1 * time.Second)
				}

				if len(anime.Ep.Links) == 0 {
					result, err := internal.ResolveEpisodeURLForPlayback(userCurdConfig, &anime, anime.Ep.Number)
					if err != nil {
						internal.Log("Failed to get episode links: " + err.Error())
						internal.CurdOut("Failed to get episode links. Try again later.")
						internal.ExitCurd(fmt.Errorf("failed to get episode links: %v", err))
						return
					}
					anime.Ep.Links = result.Links
				}

				if len(anime.Ep.Links) == 0 {
					internal.CurdOut("No episode links found. Try again later.")
					internal.ExitCurd(fmt.Errorf("no episode links found"))
					return
				}
			}
		}
		retryProvider = false

		// Show provider menu and start playback
		anime.Ep.Player.SocketPath = internal.StartCurd(&userCurdConfig, &anime)
		internal.Log(fmt.Sprint("Playback starting time: ", anime.Ep.Player.PlaybackTime))
		internal.Log(anime.Ep.Player.SocketPath)

		// Helper to close skipLoopDone safely
		closeSkipLoop := func() {
			select {
			case isClosed := <-skipLoopClosed:
				if !isClosed {
					close(skipLoopDone)
					skipLoopClosed <- true
				}
			default:
			}
		}

		wg.Add(1)
		// Get episode data goroutine
		go func() {
			defer wg.Done()
			err = internal.GetEpisodeData(anime.MalId, anime.Ep.Number, &anime)
			if err != nil {
				internal.Log("Error getting episode data: " + err.Error())
			} else {
				internal.Log(anime)
				if (anime.Ep.IsFiller && userCurdConfig.SkipFiller) || (anime.Ep.IsRecap && userCurdConfig.SkipRecap) {
					if anime.Ep.IsFiller && userCurdConfig.SkipFiller {
						internal.CurdOut(fmt.Sprint("Filler Episode, starting next episode: ", anime.Ep.Number+1))
					} else {
						internal.CurdOut(fmt.Sprint("Recap Episode, starting next episode: ", anime.Ep.Number+1))
					}
					anime.Ep.IsCompleted = true
					if !userCurdConfig.NextEpisodePrompt {
						internal.StartNextEpisode(&anime, &userCurdConfig, databaseFile, &user)
					} else {
						internal.ExitMPV(anime.Ep.Player.SocketPath)
						internal.StartNextEpisode(&anime, &userCurdConfig, databaseFile, &user)
						retryProviderCh <- false
						closeSkipLoop()
						return
					}
					_, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"quit"})
					if err != nil {
						internal.Log("Error closing MPV: " + err.Error())
					}
					retryProviderCh <- false
					closeSkipLoop()
				}
			}
		}()

		wg.Add(1)
		// Thread to update Discord presence with simple position-gap seek detection
		go func() {
			defer wg.Done()
			if userCurdConfig.DiscordPresence {
				var lastKnownPauseState bool = false
				var lastKnownPosition int = 0
				var lastStateCheck time.Time
				var discordPresenceInitialized bool = false // Track if Discord presence has been set with real duration

				for {
					select {
					case <-skipLoopDone:
						return
					default:
						// Get current state from MPV
						isPaused, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "pause"})
						if err != nil {
							internal.Log("Error getting pause status: " + err.Error())
							time.Sleep(5 * time.Second)
							continue
						}

						if isPaused == nil {
							isPaused = true
						}

						// Get current time position
						currentPos := 0
						timePos, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "time-pos"})
						if err == nil && timePos != nil {
							if pos, ok := timePos.(float64); ok {
								currentPos = int(pos + 0.5) // Round to nearest integer
							}
						}

						currentPauseState, ok := isPaused.(bool)
						if !ok {
							internal.Log(fmt.Sprintf("Error: pause state is not a bool (%T)", isPaused))
							currentPauseState = true
						}

						// Simple seek detection: position gap > 5 seconds
						hasSeekEvent := false
						if lastKnownPosition > 0 {
							positionDiff := currentPos - lastKnownPosition
							if positionDiff < -5 || positionDiff > 7 { // 5 sec backward or 7 sec forward (allowing normal playback + buffer)
								hasSeekEvent = true
							}
						}

						hasPlayPauseEvent := currentPauseState != lastKnownPauseState

						// Determine if we should update Discord presence
						shouldUpdate := false

						// Force update every 30 seconds for Discord keep-alive
						if lastStateCheck.IsZero() || time.Since(lastStateCheck) >= 30*time.Second {
							shouldUpdate = true
						}

						// Update on pause state change
						if hasPlayPauseEvent {
							shouldUpdate = true
						}

						// Update on seek events
						if hasSeekEvent {
							shouldUpdate = true
						}

						if shouldUpdate {
							// Only update Discord if we have real duration OR if presence was already initialized
							totalDuration := anime.Ep.Duration
							if totalDuration == 0 {
								// Skip Discord updates until we have real duration from MPV
								if !discordPresenceInitialized {
									lastKnownPauseState = currentPauseState
									lastKnownPosition = currentPos
									lastStateCheck = time.Now()
									time.Sleep(2 * time.Second)
									continue
								}
								totalDuration = currentPos + 1 // Small duration to avoid divide by zero
							} else {
								discordPresenceInitialized = true // Mark as initialized once we have real duration
							}

							// Force update on seek events to bypass Discord's internal filtering
							var presenceErr error
							if hasSeekEvent {
								presenceErr = internal.DiscordPresenceWithForce(anime, currentPauseState, currentPos, totalDuration, userCurdConfig.DiscordClientId, true)
							} else {
								presenceErr = internal.DiscordPresence(anime, currentPauseState, currentPos, totalDuration, userCurdConfig.DiscordClientId)
							}

							if presenceErr != nil {
								internal.Log("Error setting Discord presence: " + presenceErr.Error())
							}

							lastKnownPauseState = currentPauseState
							lastStateCheck = time.Now()
						}

						// Always update position for next comparison
						lastKnownPosition = currentPos

						time.Sleep(2 * time.Second) // Check every 2 seconds
					}
				}
			}
		}()

		// AniSkip data goroutine
		go func() {
			err = internal.GetAndParseAniSkipData(anime.MalId, anime.Ep.Number, 1, &anime)
			if err != nil {
				internal.Log("Error getting and parsing AniSkip data: " + err.Error())
			}
			internal.Log(anime.Ep.SkipTimes)
		}()

		// Get video duration
		go func() {
			for {
				if anime.Ep.Started {
					if anime.Ep.Duration == 0 {
						// Get video duration
						durationPos, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "duration"})
						if err != nil {
							internal.Log("Error getting video duration: " + err.Error())
						} else if durationPos != nil {
							if duration, ok := durationPos.(float64); ok {
								anime.Ep.Duration = int(duration + 0.5) // Round to nearest integer
								internal.Log(fmt.Sprintf("Video duration: %d seconds", anime.Ep.Duration))

								// Initialize Discord presence with correct duration (first time with real duration)
								if userCurdConfig.DiscordPresence {
									isPaused, _ := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "pause"})
									currentPos := 0
									if timePos, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "time-pos"}); err == nil && timePos != nil {
										if pos, ok := timePos.(float64); ok {
											currentPos = int(pos + 0.5)
										}
									}
									pauseState := false
									if isPaused != nil {
										if value, ok := isPaused.(bool); ok {
											pauseState = value
										} else {
											internal.Log(fmt.Sprintf("Error: pause state is not a bool (%T)", isPaused))
										}
									}
									internal.Log("Initializing Discord presence with real video duration")
									if presenceErr := internal.DiscordPresence(anime, pauseState, currentPos, anime.Ep.Duration, userCurdConfig.DiscordClientId); presenceErr != nil {
										internal.Log("Discord presence error: " + presenceErr.Error())
									}
								}
							} else {
								internal.Log("Error: duration is not a float64")
							}
						}
						break
					}
				}
				time.Sleep(1 * time.Second)
			}
		}()

		wg.Add(1)
		// Playback monitoring goroutine
		go func() {
			defer wg.Done()

			for {
				select {
				case <-skipLoopDone:
					return
				default:
					time.Sleep(1000 * time.Millisecond) // Checked every second

					timePos, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "time-pos"})

					// ── Check if mpv is still reachable ──────────────────────────────
					if err != nil {
						internal.Log("Error getting playback time: " + err.Error())

						mpvRunning := internal.IsMPVRunning(anime.Ep.Player.SocketPath)
						if !mpvRunning {
							// mpv process is gone entirely
							percentageWatched := internal.PercentageWatched(anime.Ep.Player.PlaybackTime, anime.Ep.Duration)
							internal.Log(fmt.Sprintf("MPV closed. Watched: %.1f%%, Required: %d%%", percentageWatched, userCurdConfig.PercentageToMarkComplete))

							isCompleted := int(percentageWatched) >= userCurdConfig.PercentageToMarkComplete
							if isCompleted {
								anime.Ep.IsCompleted = true
								internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, internal.ConvertSecondsToMinutes(anime.Ep.Duration), internal.GetAnimeName(anime), anime.ProviderName)

								if !anime.Rewatching {
									err2 := internal.UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, anime.Ep.Number, &userCurdConfig)
									if err2 != nil {
										internal.Log("Error updating progress on completion: " + err2.Error())
									} else {
										internal.CurdOut(fmt.Sprintf("Episode %d marked complete! Progress updated.", anime.Ep.Number))
									}
								}

								if !userCurdConfig.NextEpisodePrompt {
									internal.StartNextEpisode(&anime, &userCurdConfig, databaseFile, &user)
									retryProviderCh <- false
								} else if userCurdConfig.RofiSelection {
									if internal.NextEpisodePromptRofi(&userCurdConfig) {
										internal.StartNextEpisode(&anime, &userCurdConfig, databaseFile, &user)
									} else {
										if anime.TotalEpisodes > 0 && anime.Ep.Number == anime.TotalEpisodes && !anime.IsAiring {
											internal.HandleLastEpisodeCompletion(&userCurdConfig, &anime, &user)
										}
										internal.ExitCurd(nil)
									}
									retryProviderCh <- false
								} else {
									options := []internal.SelectionOption{
										{Key: "yes", Label: fmt.Sprintf("Continue to next episode (%d)", anime.Ep.Number+1)},
									}
									internal.CurdOut(fmt.Sprintf("Episode %d finished!", anime.Ep.Number))
									sel, _ := internal.DynamicSelect(options)
									if sel.Key == "yes" {
										internal.StartNextEpisode(&anime, &userCurdConfig, databaseFile, &user)
										retryProviderCh <- false
									} else {
										if anime.TotalEpisodes > 0 && anime.Ep.Number == anime.TotalEpisodes && !anime.IsAiring {
											internal.HandleLastEpisodeCompletion(&userCurdConfig, &anime, &user)
										}
										internal.ExitCurd(nil)
									}
								}
							} else {
								// ── Premature close (mpv gone, not enough watched) ────────
								internal.QuitActiveSelectionMenu()

								if internal.PromptTryAnotherProvider(&userCurdConfig) {
									retryProviderCh <- true
								} else {
									internal.ExitCurd(nil)
								}
							}

							closeSkipLoop()
							return
						}
						// mpv is running but time-pos errored — could be transitional, keep polling
						continue
					}

					// ── time-pos succeeded — update playback state ───────────────────
					if timePos != nil {
						currentPos, ok := timePos.(float64)
						if ok {

							if !anime.Ep.Started {
								anime.Ep.Started = true
								if userCurdConfig.SaveMpvSpeed {
									_, err2 := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"set_property", "speed", anime.Ep.Player.Speed})
									if err2 != nil {
										internal.Log("Error setting playback speed: " + err2.Error())
									}
								}
								if err2 := internal.SendSkipTimesToMPV(&anime); err2 != nil {
									internal.Log("Error sending skip times to MPV: " + err2.Error())
								}
							}

							if anime.Ep.Resume {
								internal.SeekMPV(anime.Ep.Player.SocketPath, anime.Ep.Player.PlaybackTime)
								anime.Ep.Resume = false
							}

							anime.Ep.Player.PlaybackTime = int(currentPos + 0.5)
							internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, internal.ConvertSecondsToMinutes(anime.Ep.Duration), internal.GetAnimeName(anime), anime.ProviderName)
						}
					}

					// ── Check eof-reached every tick (works with --keep-open=yes) ────
					// With --keep-open=yes, mpv pauses at the last frame on EOF.
					// time-pos still returns successfully, so we must check eof-reached
					// independently on every cycle, not just inside the error block.
					if anime.Ep.Started {
						eofReached, eofErr := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "eof-reached"})
						if eofErr == nil {
							if reached, ok := eofReached.(bool); ok && reached {
								internal.Log("EOF detected via eof-reached property")

								percentageWatched := internal.PercentageWatched(anime.Ep.Player.PlaybackTime, anime.Ep.Duration)
								internal.Log(fmt.Sprintf("Playback ended at EOF. Watched: %.1f%%, Required: %d%%", percentageWatched, userCurdConfig.PercentageToMarkComplete))

								anime.Ep.IsCompleted = true

								// Update local DB
								internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, internal.ConvertSecondsToMinutes(anime.Ep.Duration), internal.GetAnimeName(anime), anime.ProviderName)

								// Update remote progress BEFORE prompting
								if !anime.Rewatching {
									err2 := internal.UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, anime.Ep.Number, &userCurdConfig)
									if err2 != nil {
										internal.Log("Error updating progress on completion: " + err2.Error())
									} else {
										internal.CurdOut(fmt.Sprintf("Episode %d marked complete! Progress updated.", anime.Ep.Number))
									}
								}

								// Close mpv window
								if err2 := internal.ExitMPV(anime.Ep.Player.SocketPath); err2 != nil {
									internal.Log("Error closing MPV after EOF: " + err2.Error())
								}

								if !userCurdConfig.NextEpisodePrompt {
									internal.StartNextEpisode(&anime, &userCurdConfig, databaseFile, &user)
									retryProviderCh <- false
								} else if userCurdConfig.RofiSelection {
									if internal.NextEpisodePromptRofi(&userCurdConfig) {
										internal.StartNextEpisode(&anime, &userCurdConfig, databaseFile, &user)
									} else {
										if anime.TotalEpisodes > 0 && anime.Ep.Number == anime.TotalEpisodes && !anime.IsAiring {
											internal.HandleLastEpisodeCompletion(&userCurdConfig, &anime, &user)
										}
										internal.ExitCurd(nil)
									}
									retryProviderCh <- false
								} else {
									options := []internal.SelectionOption{
										{Key: "yes", Label: fmt.Sprintf("Continue to next episode (%d)", anime.Ep.Number+1)},
									}
									internal.CurdOut(fmt.Sprintf("Episode %d finished!", anime.Ep.Number))
									sel, _ := internal.DynamicSelect(options)
									if sel.Key == "yes" {
										internal.StartNextEpisode(&anime, &userCurdConfig, databaseFile, &user)
										retryProviderCh <- false
									} else {
										if anime.TotalEpisodes > 0 && anime.Ep.Number == anime.TotalEpisodes && !anime.IsAiring {
											internal.HandleLastEpisodeCompletion(&userCurdConfig, &anime, &user)
										}
										internal.ExitCurd(nil)
									}
								}

								closeSkipLoop()
								return
							}
						}
					}
				}
			}
		}()

		// Skip OP/ED loop and speed saving
	skipLoop:
		for {
			select {
			case <-skipLoopDone:
				break skipLoop
			default:
				if userCurdConfig.SkipOp {
					if anime.Ep.Player.PlaybackTime > anime.Ep.SkipTimes.Op.Start && anime.Ep.Player.PlaybackTime < anime.Ep.SkipTimes.Op.Start+2 && anime.Ep.SkipTimes.Op.Start != anime.Ep.SkipTimes.Op.End {
						internal.SeekMPV(anime.Ep.Player.SocketPath, anime.Ep.SkipTimes.Op.End)
					}
				}
				if userCurdConfig.SkipEd {
					if anime.Ep.Player.PlaybackTime > anime.Ep.SkipTimes.Ed.Start && anime.Ep.Player.PlaybackTime < anime.Ep.SkipTimes.Ed.Start+2 && anime.Ep.SkipTimes.Ed.Start != anime.Ep.SkipTimes.Ed.End {
						internal.SeekMPV(anime.Ep.Player.SocketPath, anime.Ep.SkipTimes.Ed.End)
					}
				}
				_, err2 := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "time-pos"})
				if err2 == nil && anime.Ep.Started {
					anime.Ep.Player.Speed, err2 = internal.GetMPVPlaybackSpeed(anime.Ep.Player.SocketPath)
					if err2 != nil {
						internal.Log("Failed to get mpv speed " + err2.Error())
					}
				}
			}
			time.Sleep(500 * time.Millisecond)
		}

		wg.Wait()
		wg = sync.WaitGroup{}

		// Read the retry signal
		retryProvider = false
		select {
		case r := <-retryProviderCh:
			retryProvider = r
		default:
		}

		if retryProvider {
			// Re-show the provider menu for the same episode — loop back
			continue
		}

		// ── Post-episode logic ─────────────────────────────────────────────────────
		if anime.Ep.Number > anime.TotalEpisodes && anime.TotalEpisodes > 0 {
			internal.CurdOut("Reached end of series")
			internal.ExitCurd(nil)
		}

		if anime.Ep.IsCompleted && !anime.Rewatching {
			// Redundant background updates have been removed, as they are correctly handled
			// before the prompt and inside StartNextEpisode.
			anime.Ep.IsCompleted = false

			if anime.Ep.Number-1 == anime.TotalEpisodes && userCurdConfig.ScoreOnCompletion && anime.TotalEpisodes > 0 {
				updatedAnime, err := internal.GetAnimeDataByID(anime.AnilistId, user.Token)
				if err != nil {
					internal.Log("Error getting updated anime data: " + err.Error())
				} else if !updatedAnime.IsAiring {
					anime.Ep.Number = anime.Ep.Number - 1
					internal.CurdOut("Completed anime.")
					err = internal.RateAnime(user.Token, anime.AnilistId)
					if err != nil {
						internal.Log("Error rating anime: " + err.Error())
						internal.CurdOut("Error rating anime: " + err.Error())
					}
					internal.LocalDeleteAnime(databaseFile, anime.AnilistId, anime.ProviderId)
					internal.ExitCurd(nil)
				}
			}
		}

		if anime.Rewatching && anime.Ep.IsCompleted && anime.Ep.Number-1 == anime.TotalEpisodes {
			anime.Ep.Number = anime.Ep.Number - 1
			internal.CurdOut("Completed anime. (Rewatching so no scoring)")
			internal.LocalDeleteAnime(databaseFile, anime.AnilistId, anime.ProviderId)
			internal.ExitCurd(nil)
		}

		// Clear links so next iteration fetches fresh ones for the new episode
		anime.Ep.Links = []string{}
	}
}
