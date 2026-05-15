package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wraient/curd/internal"
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
			version = "1.1.7"
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
	trackingService := strings.ToLower(userCurdConfig.TrackingService)
	if trackingService == "" {
		trackingService = "mal" // Default to MAL
	}

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

		if !retryProvider {
			// Get MalId and CoverImage (only if discord presence is enabled)
			if userCurdConfig.DiscordPresence {
				discordAvailable := true
				err := internal.LoginClient(userCurdConfig.DiscordClientId)
				if err != nil {
					internal.Log("Discord connection failed, disabling presence: " + err.Error())
					discordAvailable = false
					userCurdConfig.DiscordPresence = false
				}

				if discordAvailable {
					anime.MalId, anime.CoverImage, err = internal.GetAnimeIDAndImage(anime.AnilistId)
					if err != nil {
						internal.Log("Error getting anime ID and image: " + err.Error())
					}
					err = internal.DiscordPresence(anime, false)
					if err != nil {
						internal.Log("Discord presence error, disabling: " + err.Error())
						userCurdConfig.DiscordPresence = false
					}
				}
			} else if anime.MalId == 0 {
				anime.MalId, err = internal.GetAnimeMalID(anime.AnilistId)
				if err != nil {
					internal.Log("Error getting anime MAL ID: " + err.Error())
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
				internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.AllanimeId, anime.Ep.Number, 0, 0, internal.GetAnimeName(anime))

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
					if anime.Ep.NextEpisode.Number == anime.Ep.Number && len(anime.Ep.NextEpisode.Links) > 0 {
						internal.Log("Using prefetched next episode link")
						anime.Ep.Links = anime.Ep.NextEpisode.Links
						break
					}
					time.Sleep(1 * time.Second)
				}

				if len(anime.Ep.Links) == 0 {
					links, err := internal.GetEpisodeURL(userCurdConfig, anime.AllanimeId, anime.Ep.Number)
					if err != nil {
						internal.Log("Failed to get episode links: " + err.Error())
						internal.CurdOut("Failed to get episode links. Try again later.")
						internal.ExitCurd(fmt.Errorf("failed to get episode links: %v", err))
						return
					}
					anime.Ep.Links = links
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
		// Discord presence goroutine
		go func() {
			defer wg.Done()
			if userCurdConfig.DiscordPresence {
				for {
					select {
					case <-skipLoopDone:
						return
					default:
						isPaused, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "pause"})
						if err != nil {
							internal.Log("Error getting pause status: " + err.Error())
						}
						if isPaused == nil {
							isPaused = true
						} else {
							isPaused = isPaused.(bool)
						}
						internal.DiscordPresence(anime, isPaused.(bool))
						time.Sleep(1 * time.Second)
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

		// Video duration goroutine
		go func() {
			for {
				if anime.Ep.Started {
					if anime.Ep.Duration == 0 {
						durationPos, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "duration"})
						if err != nil {
							internal.Log("Error getting video duration: " + err.Error())
						} else if durationPos != nil {
							if duration, ok := durationPos.(float64); ok {
								anime.Ep.Duration = int(duration + 0.5)
								internal.Log(fmt.Sprintf("Video duration: %d seconds", anime.Ep.Duration))
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
			playbackStartTime := time.Now()
			lastPos := -1.0
			lastPosTime := time.Now()
			stuckThreshold := 25 * time.Second // Increased to 25s for slow connections

			for {
				select {
				case <-skipLoopDone:
					return
				default:
					time.Sleep(1000 * time.Millisecond) // Checked every second

					timePos, err := internal.MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"get_property", "time-pos"})
					if err != nil {
						internal.Log("Error getting playback time: " + err.Error())

						// MPV is gone — decide what to do
						if !internal.IsMPVRunning(anime.Ep.Player.SocketPath) {
							percentageWatched := internal.PercentageWatched(anime.Ep.Player.PlaybackTime, anime.Ep.Duration)
							internal.Log(fmt.Sprintf("MPV closed. Watched: %.1f%%, Required: %d%%", percentageWatched, userCurdConfig.PercentageToMarkComplete))

							if int(percentageWatched) >= userCurdConfig.PercentageToMarkComplete {
								// ── Episode completed ──────────────────────────────────────────────
								anime.Ep.IsCompleted = true

								// Update local DB
								internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.AllanimeId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, internal.ConvertSecondsToMinutes(anime.Ep.Duration), internal.GetAnimeName(anime))

								// Update remote progress BEFORE prompting
								if !anime.Rewatching {
									err2 := internal.UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, anime.Ep.Number, &userCurdConfig)
									if err2 != nil {
										internal.Log("Error updating progress on completion: " + err2.Error())
									} else {
										internal.CurdOut(fmt.Sprintf("Episode %d marked complete! Progress updated.", anime.Ep.Number))
									}
								}

								if !userCurdConfig.NextEpisodePrompt {
									// Auto-advance
									internal.StartNextEpisode(&anime, &userCurdConfig, databaseFile, &user)
									retryProviderCh <- false
								} else if userCurdConfig.RofiSelection {
									if internal.NextEpisodePromptRofi(&userCurdConfig) {
										internal.StartNextEpisode(&anime, &userCurdConfig, databaseFile, &user)
									} else {
										internal.ExitCurd(nil)
									}
									retryProviderCh <- false
								} else {
									// CLI mode: show next-episode prompt
									options := []internal.SelectionOption{
										{Key: "yes", Label: fmt.Sprintf("Continue to next episode (%d)", anime.Ep.Number+1)},
									}
									internal.CurdOut(fmt.Sprintf("Episode %d finished!", anime.Ep.Number))
									sel, _ := internal.DynamicSelect(options)
									if sel.Key == "yes" {
										internal.StartNextEpisode(&anime, &userCurdConfig, databaseFile, &user)
										retryProviderCh <- false
									} else {
										internal.ExitCurd(nil)
									}
								}
							} else {
								// ── Premature close ────────────────────────────────────────────────
								internal.QuitActiveSelectionMenu()

								// Automatic Provider Fallback:
								// If MPV exited with an error OR played for less than 10 seconds in this session,
								// OR if we manually killed it because it was stuck,
								// and there are more links available, try the next one automatically.
								sessionDuration := time.Since(playbackStartTime).Seconds()
								if (anime.Ep.Player.LastExitCode != 0 || sessionDuration < 10 || anime.Ep.StuckDetected) && len(anime.Ep.Links) > 1 {
									internal.Log(fmt.Sprintf("Automatic Fallback: MPV exited (code: %d, session time: %.1fs, stuck: %v). Trying next provider.",
										anime.Ep.Player.LastExitCode, sessionDuration, anime.Ep.StuckDetected))
									internal.CurdOut("Provider failed, stuck, or exited early. Switching to next available provider...")

									// Remove the current link from the list
									if len(anime.Ep.Links) > 0 {
										anime.Ep.Links = anime.Ep.Links[1:]
									}
									anime.Ep.AutoFallback = true
									anime.Ep.StuckDetected = false // Reset
									retryProviderCh <- true
								} else if internal.PromptTryAnotherProvider(&userCurdConfig) {
									retryProviderCh <- true
								} else {
									internal.ExitCurd(nil)
								}
							}

							closeSkipLoop()
							return
						}
						continue
					}

					// timePos obtained — update playback state
					if timePos != nil {
						currentPos, ok := timePos.(float64)
						if ok {
							// Stuck detection logic
							if currentPos != lastPos {
								lastPos = currentPos
								lastPosTime = time.Now()
							} else {
								// Position hasn't changed, check if we're paused
								isPaused, pErr := internal.GetMPVPausedStatus(anime.Ep.Player.SocketPath)
								if pErr == nil && !isPaused {
									if time.Since(lastPosTime) > stuckThreshold {
										internal.Log(fmt.Sprintf("Playback stuck at %.2f for %v. Killing MPV.", currentPos, stuckThreshold))
										internal.CurdOut("Playback stuck. Switching to another provider...")
										anime.Ep.StuckDetected = true
										internal.ExitMPV(anime.Ep.Player.SocketPath)
										continue
									}
								} else {
									// If paused, reset the stuck timer
									lastPosTime = time.Now()
								}
							}

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
							internal.LocalUpdateAnime(databaseFile, anime.AnilistId, anime.AllanimeId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, internal.ConvertSecondsToMinutes(anime.Ep.Duration), internal.GetAnimeName(anime))
						}
					} else {
						// timePos is nil (could be loading/buffering at the very start)
						if time.Since(lastPosTime) > stuckThreshold {
							internal.Log(fmt.Sprintf("MPV stuck loading (no time-pos) for %v. Killing MPV.", stuckThreshold))
							internal.CurdOut("Provider stuck loading. Switching to another provider...")
							anime.Ep.StuckDetected = true
							internal.ExitMPV(anime.Ep.Player.SocketPath)
							continue
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
			if anime.TotalEpisodes > 0 && anime.Ep.Number-1 != anime.TotalEpisodes {
				go func() {
					err = internal.UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, anime.Ep.Number-1, &userCurdConfig)
					if err != nil {
						internal.Log("Error updating progress: " + err.Error())
					}
				}()
			} else {
				internal.ExitMPV(anime.Ep.Player.SocketPath)
				err = internal.UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, anime.Ep.Number-1, &userCurdConfig)
				if err != nil {
					internal.Log("Error updating progress: " + err.Error())
				}
			}

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
					internal.LocalDeleteAnime(databaseFile, anime.AnilistId, anime.AllanimeId)
					internal.ExitCurd(nil)
				}
			}
		}

		if anime.Rewatching && anime.Ep.IsCompleted && anime.Ep.Number-1 == anime.TotalEpisodes {
			anime.Ep.Number = anime.Ep.Number - 1
			internal.CurdOut("Completed anime. (Rewatching so no scoring)")
			internal.LocalDeleteAnime(databaseFile, anime.AnilistId, anime.AllanimeId)
			internal.ExitCurd(nil)
		}

		// Clear links so next iteration fetches fresh ones for the new episode
		anime.Ep.Links = []string{}
	}
}

