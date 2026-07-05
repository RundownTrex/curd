package internal

import (
	"bufio"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gen2brain/beeep"
)

func EditConfig(configFilePath string) {
	// Get the user's preferred editor from the EDITOR environment variable
	editor := os.Getenv("EDITOR")
	if editor == "" {
		// If EDITOR is not set, use system-specific defaults
		if runtime.GOOS == "windows" {
			// Try Notepad++ first
			if _, err := exec.LookPath("notepad++"); err == nil {
				editor = "notepad++"
			} else {
				editor = "notepad.exe"
			}
		} else {
			if _, err := exec.LookPath("vim"); err == nil {
				editor = "vim"
			} else {
				editor = "nano"
			}
		}
	}

	// Construct the command to open the config file
	cmd := exec.Command(editor, configFilePath)

	// Set the command to run in the current terminal
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Run the editor command
	err := cmd.Run()
	if err != nil {
		CurdOut(fmt.Sprintf("Error opening config file: %v", err))
		return
	}

	CurdOut("Config file edited successfully.")
}

// ClearLogFile removes all contents from the specified log file
func ClearLogFile(logFile string) error {
	// Open the file with truncate flag to clear its contents
	file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	return nil
}

// LogData logs the input data into a specified log file with the format [LOG] time lineNumber: logData
func Log(data interface{}) error {
	logFile := GetGlobalLogFile()
	// Open or create the log file
	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer file.Close()

	// Attempt to marshal the data into JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Get the caller information
	_, filename, lineNumber, ok := runtime.Caller(1) // Caller 1 gives the caller of LogData
	if !ok {
		return fmt.Errorf("unable to get caller information")
	}

	// Log the current time and the JSON representation along with caller info
	currentTime := time.Now().Format("2006/01/02 15:04:05")
	logMessage := fmt.Sprintf("[LOG] %s %s:%d: %s\n", currentTime, filename, lineNumber, jsonData)
	_, err = fmt.Fprint(file, logMessage) // Write to the file
	if err != nil {
		return err
	}

	return nil
}

// ClearScreen clears the terminal screen and saves the state
func ClearScreen() {
	userCurdConfig := GetGlobalConfig()

	if userCurdConfig.AlternateScreen == false {
		return
	}

	fmt.Print("\033[?1049h") // Switch to alternate screen buffer
	fmt.Print("\033[2J")     // Clear the entire screen
	fmt.Print("\033[H")      // Move cursor to the top left
}

// RestoreScreen restores the original terminal state
func RestoreScreen() {
	userCurdConfig := GetGlobalConfig()

	if userCurdConfig.AlternateScreen == false {
		return
	}

	fmt.Print("\033[?1049l") // Switch back to the main screen buffer
}

func ExitCurd(err error) {
	RestoreScreen()

	anime := GetGlobalAnime()
	if (anime != nil) && (anime.Ep.Player.SocketPath != "") {
		_, err = MPVSendCommand(anime.Ep.Player.SocketPath, []interface{}{"quit"})
		if err != nil {
			Log("Error closing MPV: " + err.Error())
		}
	}

	CurdOut("Have a great day!")
	// If the error is not about the connection refused, print the error
	if err != nil && !strings.Contains(err.Error(), "dial unix "+anime.Ep.Player.SocketPath+": connect: connection refused") {
		CurdOut(fmt.Sprintf("Error: %v", err))
		if runtime.GOOS == "windows" {
			fmt.Println("Press Enter to exit")
			var wait string
			fmt.Scanln(&wait)
			os.Exit(1)
		} else {
			os.Exit(1)
		}
	}
	os.Exit(0)
}

func CurdOut(data interface{}) {
	userCurdConfig := GetGlobalConfig()
	if userCurdConfig == nil {
		userCurdConfig = &CurdConfig{}
	}
	if !userCurdConfig.RofiSelection {
		fmt.Println(fmt.Sprintf("%v", data))
	} else {
		switch runtime.GOOS {
		case "windows":
			err := beeep.Notify(
				"Curd",
				fmt.Sprintf("%v", data),
				"",
			)

			if err != nil {
				Log(fmt.Sprintf("Failed to send notification: %v", err))
			}
		case "linux":
			// Check if the input starts with "-i" for image notification
			dataStr := fmt.Sprintf("%v", data)
			if strings.HasPrefix(dataStr, "-i") && userCurdConfig.ImagePreview && userCurdConfig.RofiSelection {
				// Split the string to get image path and message
				parts := strings.SplitN(dataStr, " ", 3)
				if len(parts) == 3 {
					// Remove quotes from the message
					message := strings.Trim(parts[2], "\"")
					cmd := exec.Command("notify-send",
						"-a", "Curd",
						"-h", "string:x-canonical-private-synchronous:curd-notification",
						"Curd",
						"-i", parts[1],
						message)
					err := cmd.Run()
					if err != nil {
						Log(fmt.Sprintf("%v", cmd))
						Log(fmt.Sprintf("Failed to send notification: %v", err))
					}
				}
			} else {
				cmd := exec.Command("notify-send",
					"-a", "Curd",
					"-h", "string:x-canonical-private-synchronous:curd-notification",
					"Curd",
					dataStr)
				err := cmd.Run()
				if err != nil {
					Log(fmt.Sprintf("%v", cmd))
					Log(fmt.Sprintf("Failed to send notification: %v", err))
				}
			}
		}
	}
}

func UpdateAnimeEntry(userCurdConfig *CurdConfig, user *User) bool {
	// Create update options
	updateOptions := []SelectionOption{
		{Key: "back", Label: "<- Back"},
		{Key: "CATEGORY", Label: "Change Anime Category"},
		{Key: "PROGRESS", Label: "Change Progress"},
		{Key: "SCORE", Label: "Add/Change Score"},
	}

	// Select update option
	updateSelection, err := DynamicSelect(updateOptions)
	if err != nil {
		Log(fmt.Sprintf("Failed to select update option: %v", err))
		ExitCurd(fmt.Errorf("Failed to select update option"))
	}

	if updateSelection.Key == "-1" {
		ExitCurd(nil)
	}

	// Handle "Back" option
	if updateSelection.Key == "back" {
		return true // Return true to indicate user wants to go back
	}

	// Get user's anime list
	var animeListOptions []SelectionOption
	var animeListMapPreview map[string]RofiSelectPreview

	if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
		animeListMapPreview = make(map[string]RofiSelectPreview)
		// Include anime from all categories
		for _, entry := range user.AnimeList.Watching {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListMapPreview[strconv.Itoa(entry.Media.ID)] = RofiSelectPreview{
				Title:      title,
				CoverImage: entry.CoverImage,
			}
		}
		for _, entry := range user.AnimeList.Completed {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListMapPreview[strconv.Itoa(entry.Media.ID)] = RofiSelectPreview{
				Title:      title,
				CoverImage: entry.CoverImage,
			}
		}
		for _, entry := range user.AnimeList.Paused {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListMapPreview[strconv.Itoa(entry.Media.ID)] = RofiSelectPreview{
				Title:      title,
				CoverImage: entry.CoverImage,
			}
		}
		for _, entry := range user.AnimeList.Dropped {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListMapPreview[strconv.Itoa(entry.Media.ID)] = RofiSelectPreview{
				Title:      title,
				CoverImage: entry.CoverImage,
			}
		}
		for _, entry := range user.AnimeList.Planning {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListMapPreview[strconv.Itoa(entry.Media.ID)] = RofiSelectPreview{
				Title:      title,
				CoverImage: entry.CoverImage,
			}
		}
		for _, entry := range user.AnimeList.Rewatching {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListMapPreview[strconv.Itoa(entry.Media.ID)] = RofiSelectPreview{
				Title:      title,
				CoverImage: entry.CoverImage,
			}
		}
	} else {
		animeListOptions = make([]SelectionOption, 0)
		// Include anime from all categories
		for _, entry := range user.AnimeList.Watching {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListOptions = append(animeListOptions, SelectionOption{
				Key:   strconv.Itoa(entry.Media.ID),
				Label: title,
			})
		}
		for _, entry := range user.AnimeList.Completed {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListOptions = append(animeListOptions, SelectionOption{
				Key:   strconv.Itoa(entry.Media.ID),
				Label: title,
			})
		}
		for _, entry := range user.AnimeList.Paused {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListOptions = append(animeListOptions, SelectionOption{
				Key:   strconv.Itoa(entry.Media.ID),
				Label: title,
			})
		}
		for _, entry := range user.AnimeList.Dropped {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListOptions = append(animeListOptions, SelectionOption{
				Key:   strconv.Itoa(entry.Media.ID),
				Label: title,
			})
		}
		for _, entry := range user.AnimeList.Planning {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListOptions = append(animeListOptions, SelectionOption{
				Key:   strconv.Itoa(entry.Media.ID),
				Label: title,
			})
		}
		for _, entry := range user.AnimeList.Rewatching {
			title := entry.Media.Title.English
			if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
				title = entry.Media.Title.Romaji
			}
			animeListOptions = append(animeListOptions, SelectionOption{
				Key:   strconv.Itoa(entry.Media.ID),
				Label: title,
			})
		}
		// Add "Back" option at the beginning
		animeListOptions = append([]SelectionOption{{Key: "back", Label: "<- Back"}}, animeListOptions...)
	}

	// Select anime to update
	var selectedAnime SelectionOption
	if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
		selectedAnime, err = DynamicSelectPreviewWithBack(animeListMapPreview, false, "<- Back")
	} else {
		selectedAnime, err = DynamicSelect(animeListOptions)
	}
	if err != nil {
		Log(fmt.Sprintf("Failed to select anime: %v", err))
		ExitCurd(fmt.Errorf("Failed to select anime"))
	}

	if selectedAnime.Key == "-1" {
		ExitCurd(nil)
	}

	// Handle "Back" option - restart UpdateAnimeEntry to show update options again
	if selectedAnime.Key == "back" {
		return UpdateAnimeEntry(userCurdConfig, user)
	}

	// After getting anime selection, get the current anime entry
	selectedAnilistAnime, err := FindAnimeByAnilistID(user.AnimeList, selectedAnime.Key)
	if err != nil {
		Log(fmt.Sprintf("Can not find the anime in animelist: %v", err))
		ExitCurd(fmt.Errorf("Can not find the anime in animelist"))
	}
	ClearScreen()
	switch updateSelection.Key {
	case "CATEGORY":
		categories := []SelectionOption{
			{Key: "CURRENT", Label: "Currently Watching"},
			{Key: "COMPLETED", Label: "Completed"},
			{Key: "PAUSED", Label: "On Hold"},
			{Key: "DROPPED", Label: "Dropped"},
			{Key: "PLANNING", Label: "Plan to Watch"},
			{Key: "REPEATING", Label: "Rewatching"}, // Anilist uses REPEATING for rewatching
		}

		currentStatus := "None"
		if selectedAnilistAnime.Status != "" {
			// Find the label for the current status
			for _, cat := range categories {
				if cat.Key == selectedAnilistAnime.Status {
					currentStatus = cat.Label
					break
				}
			}
		}
		CurdOut(fmt.Sprintf("Current category: %s", currentStatus))

		categorySelection, err := DynamicSelect(categories)
		if err != nil {
			Log(fmt.Sprintf("Failed to select category: %v", err))
			ExitCurd(fmt.Errorf("Failed to select category"))
		}

		if categorySelection.Key == "-1" {
			ExitCurd(nil)
		}

		// Use dual tracking if enabled
		err = UpdateAnimeStatusDual(user.AnilistToken, user.MalToken, selectedAnilistAnime.Media.ID, selectedAnilistAnime.Media.ID, categorySelection.Key, userCurdConfig)
		if err != nil {
			Log(fmt.Sprintf("Failed to update anime status: %v", err))
			ExitCurd(fmt.Errorf("Failed to update anime status"))
		}

	case "PROGRESS":
		currentProgress := "None"
		if selectedAnilistAnime.Progress > 0 {
			currentProgress = strconv.Itoa(selectedAnilistAnime.Progress)
		}

		var progress string
		if userCurdConfig.RofiSelection {
			progress, err = GetUserInputFromRofi(fmt.Sprintf("Current progress: %s\nEnter new progress (episode number)", currentProgress))
			if err != nil {
				Log(fmt.Sprintf("Failed to get progress input: %v", err))
				ExitCurd(fmt.Errorf("Failed to get progress input"))
			}
		} else {
			CurdOut(fmt.Sprintf("Current progress: %s", currentProgress))
			CurdOut("Enter new progress (episode number):")
			fmt.Scanln(&progress)
		}

		progressNum, err := strconv.Atoi(progress)
		if err != nil {
			Log(fmt.Sprintf("Failed to convert progress to number: %v", err))
			ExitCurd(fmt.Errorf("Failed to convert progress to number"))
		}

		// Get the correct IDs for both services
		anilistID := selectedAnilistAnime.Media.ID
		malID := selectedAnilistAnime.Media.ID
		service := GetTrackingService(userCurdConfig)
		if service == "mal" {
			// selectedAnilistAnime.Media.ID is MAL ID, convert to AniList ID
			anilistID, _ = ConvertMALIDToAnilist(malID, "")
		} else {
			// selectedAnilistAnime.Media.ID is AniList ID, convert to MAL ID
			malID, _ = GetAnimeMalID(anilistID)
		}

		err = UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anilistID, malID, progressNum, userCurdConfig)
		if err != nil {
			Log(fmt.Sprintf("Failed to update anime progress: %v", err))
			ExitCurd(fmt.Errorf("Failed to update anime progress"))
		}

	case "SCORE":
		currentScore := "None"
		if selectedAnilistAnime.Score > 0 {
			currentScore = strconv.Itoa(int(selectedAnilistAnime.Score))
		}
		CurdOut(fmt.Sprintf("Current score: %s", currentScore))

		// Get the correct IDs for both services
		anilistID := selectedAnilistAnime.Media.ID
		malID := selectedAnilistAnime.Media.ID
		service := GetTrackingService(userCurdConfig)
		if service == "mal" {
			// selectedAnilistAnime.Media.ID is MAL ID, convert to AniList ID
			anilistID, _ = ConvertMALIDToAnilist(malID, "")
		} else {
			// selectedAnilistAnime.Media.ID is AniList ID, convert to MAL ID
			malID, _ = GetAnimeMalID(anilistID)
		}

		err = RateAnimeDual(user.AnilistToken, user.MalToken, anilistID, malID, userCurdConfig)
		if err != nil {
			Log(fmt.Sprintf("Failed to update anime score: %v", err))
			ExitCurd(fmt.Errorf("Failed to update anime score"))
		}
	}

	CurdOut("Anime updated successfully!")
	return false // Return false to indicate normal completion
}

func UpdateCurd(repo, fileName string) error {
	// Get the path of the currently running executable
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("unable to find current executable: %v", err)
	}

	// Determine the correct binary name based on OS and architecture
	var binaryName string
	switch runtime.GOOS {
	case "windows":
		if runtime.GOARCH == "arm64" {
			binaryName = "curd-windows-arm64.exe"
		} else {
			binaryName = "curd-windows-x86_64.exe"
		}
	case "darwin": // macOS
		switch runtime.GOARCH {
		case "amd64":
			binaryName = "curd-macos-x86_64"
		case "arm64":
			binaryName = "curd-macos-arm64"
		default:
			binaryName = "curd-macos-universal"
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			binaryName = "curd-linux-x86_64"
		case "arm64":
			binaryName = "curd-linux-arm64"
		default:
			return fmt.Errorf("unsupported Linux architecture: %s", runtime.GOARCH)
		}
	default:
		return fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}

	// GitHub release URL for curd
	url := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, binaryName)

	// Temporary path for the downloaded curd executable
	tmpPath := executablePath + ".tmp"

	// Download the curd executable
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file: %v", err)
	}
	defer resp.Body.Close()

	// Check if the download was successful
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download file: received status code %d", resp.StatusCode)
	}

	// Create a new temporary file
	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %v", err)
	}
	defer out.Close()

	// Set file permissions
	if err := out.Chmod(0755); err != nil {
		return fmt.Errorf("failed to set file permissions: %v", err)
	}

	// Copy the downloaded content to the temporary file
	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("failed to write to temporary file: %v", err)
	}

	// Close the file before renaming
	out.Close()

	// Replace the old executable with the new one
	if runtime.GOOS == "windows" {
		// On Windows, we need to rename the old file first
		oldPath := executablePath + ".old"
		err = os.Rename(executablePath, oldPath)
		if err != nil {
			return fmt.Errorf("failed to rename old executable: %v", err)
		}
		err = os.Rename(tmpPath, executablePath)
		if err != nil {
			// Try to restore the old executable if the rename fails
			os.Rename(oldPath, executablePath)
			return fmt.Errorf("failed to rename new executable: %v", err)
		}
		os.Remove(oldPath)
	} else {
		// On Unix systems, we can directly rename
		if err := os.Rename(tmpPath, executablePath); err != nil {
			return fmt.Errorf("failed to replace executable: %v", err)
		}
	}

	return nil
}

func AddNewAnime(userCurdConfig *CurdConfig, anime *Anime, user *User, databaseAnimes *[]Anime) SelectionOption {
	var query string
	// Remove the redeclared variable declaration since animeOptions is already declared above
	var animeMapPreview map[string]RofiSelectPreview
	var animeOptions []SelectionOption
	var err error
	var anilistSelectedOption SelectionOption

	if userCurdConfig.RofiSelection {
		userInput, err := GetUserInputFromRofi("Enter the anime name")
		if err != nil {
			Log("Error getting user input: " + err.Error())
			ExitCurd(fmt.Errorf("Error getting user input: " + err.Error()))
		}
		query = userInput
	} else {
		CurdOut("Enter the anime name:")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		query = strings.TrimSpace(input)
	}
	if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
		animeMapPreview, err = SearchAnimeAnilistPreview(query, user.Token)
	} else {
		result, err := SearchAnimeUnified(query, user.Token, userCurdConfig, false)
		if err != nil {
			Log(fmt.Sprintf("Failed to search anime: %v", err))
			ExitCurd(fmt.Errorf("Failed to search anime"))
		}
		animeOptions = result.([]SelectionOption)
	}
	if err != nil {
		Log(fmt.Sprintf("Failed to search anime: %v", err))
		ExitCurd(fmt.Errorf("Failed to search anime"))
	}
	if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
		anilistSelectedOption, err = DynamicSelectPreview(animeMapPreview, false)
	} else {
		anilistSelectedOption, err = DynamicSelect(animeOptions)
	}

	if anilistSelectedOption.Key == "-1" {
		ExitCurd(nil)
	}

	if err != nil {
		Log(fmt.Sprintf("No anime available: %v", err))
		ExitCurd(fmt.Errorf("No anime available"))
	}
	animeID, err := strconv.Atoi(anilistSelectedOption.Key)
	if err != nil {
		Log(fmt.Sprintf("Failed to convert anime ID to integer: %v", err))
		ExitCurd(fmt.Errorf("Failed to convert anime ID to integer"))
	}

	// Add category selection before adding to list
	categories := []SelectionOption{
		{Key: "CURRENT", Label: "Currently Watching"},
		{Key: "COMPLETED", Label: "Completed"},
		{Key: "PAUSED", Label: "On Hold"},
		{Key: "DROPPED", Label: "Dropped"},
		{Key: "PLANNING", Label: "Plan to Watch"},
		{Key: "REPEATING", Label: "Rewatching"}, // Anilist uses REPEATING for rewatching
	}

	ClearScreen()
	CurdOut("Select which list to add the anime to:")

	categorySelection, err := DynamicSelect(categories)
	if err != nil {
		Log(fmt.Sprintf("Failed to select category: %v", err))
		ExitCurd(fmt.Errorf("Failed to select category"))
	}

	if categorySelection.Key == "-1" {
		ExitCurd(nil)
	}

	err = UpdateAnimeStatusUnified(user.Token, animeID, categorySelection.Key, userCurdConfig)
	if err != nil {
		Log(fmt.Sprintf("Failed to add anime to list: %v", err))
		ExitCurd(fmt.Errorf("Failed to add anime to list"))
	}

	// Refresh user's anime list after adding
	if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
		anilistUserDataPreview, err := GetUserDataPreview(user.Token, user.Id)
		if err != nil {
			Log(fmt.Sprintf("Failed to refresh anime list: %v", err))
			ExitCurd(fmt.Errorf("Failed to refresh anime list"))
		}
		user.AnimeList = ParseAnimeList(anilistUserDataPreview)
	} else {
		anilistUserData, err := GetUserData(user.Token, user.Id)
		if err != nil {
			Log(fmt.Sprintf("Failed to refresh anime list: %v", err))
			ExitCurd(fmt.Errorf("Failed to refresh anime list"))
		}
		user.AnimeList = ParseAnimeList(anilistUserData)
	}

	return anilistSelectedOption
}

func SetupCurd(userCurdConfig *CurdConfig, anime *Anime, user *User, databaseAnimes *[]Anime, databaseFile string) {
	var err error
	var anilistUserData map[string]interface{}
	var anilistUserDataPreview map[string]interface{}

	// Filter anime list based on selected category
	var animeListOptions []SelectionOption
	var animeListMapPreview map[string]RofiSelectPreview

	// Get user id, username and Anime list
	user.Id, user.Username, err = GetUserIDUnified(user.Token, userCurdConfig)
	if err != nil {
		Log(fmt.Sprintf("Failed to get user ID: %v", err))
		serviceName := GetServiceName(userCurdConfig)
		ExitCurd(fmt.Errorf("Failed to get user ID from %s\nYou can reset the token by running `curd -change-token` or `curd -change-mal-token`", serviceName))
	}

	// Get the anime list data
	if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
		anilistUserDataPreview, err = GetUserDataUnified(user.Token, user.Id, userCurdConfig, true)
		if err != nil {
			Log(fmt.Sprintf("Failed to get user data preview: %v", err))
			ExitCurd(fmt.Errorf("Failed to get user data preview"))
		}
		user.AnimeList = ParseAnimeList(anilistUserDataPreview)
	} else {
		anilistUserData, err = GetUserDataUnified(user.Token, user.Id, userCurdConfig, false)
		if err != nil {
			Log(fmt.Sprintf("Failed to get user data: %v", err))
			ExitCurd(fmt.Errorf("Failed to get user ID\nYou can reset the token by running `curd -change-token`"))
		}
		user.AnimeList = ParseAnimeList(anilistUserData)
	}

	// If continueLast flag is set, directly get the last watched anime
	if anime.Ep.ContinueLast {
		// Get the last anime ID from the curd_id file
		idFilePath := filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "curd_id")
		idBytes, err := os.ReadFile(idFilePath)
		if err != nil {
			Log("Error reading curd_id file: " + err.Error())
			ExitCurd(fmt.Errorf("No last watched anime found"))
		}

		anilistID, err := strconv.Atoi(string(idBytes))
		if err != nil {
			Log("Error converting anilist ID: " + err.Error())
			ExitCurd(fmt.Errorf("Invalid anime ID in curd_id file"))
		}

		// Find the anime in database
		animePointer := LocalFindAnime(*databaseAnimes, anilistID, "")
		if animePointer == nil {
			ExitCurd(fmt.Errorf("Last watched anime not found in database"))
		}

		// Set the anime details
		anime.AnilistId = animePointer.AnilistId
		// anime.AllanimeId = animePointer.AllanimeId
		// anime.Title = animePointer.Title
		// anime.Ep.Number = animePointer.Ep.Number
		// anime.Ep.Player.PlaybackTime = animePointer.Ep.Player.PlaybackTime
		// anime.Ep.Resume = true

	} else {
		// Skip category selection if Current flag is set
		var categorySelection SelectionOption
		if userCurdConfig.CurrentCategory {
			categorySelection = SelectionOption{
				Key:   "CURRENT",
				Label: "Currently Watching",
			}
		} else {
			// Create category selection map
			// Get ordered categories
			orderedCategories := getOrderedCategories(userCurdConfig)

			// Use DynamicSelect with ordered categories directly
			categorySelection, err = DynamicSelect(orderedCategories)

			if err != nil {
				Log(fmt.Sprintf("Failed to select category: %v", err))
				ExitCurd(fmt.Errorf("Failed to select category"))
			}

			if categorySelection.Key == "-1" {
				ExitCurd(nil)
			}

			// Handle options
			if categorySelection.Key == "UPDATE" {
				ClearScreen()
				goBack := UpdateAnimeEntry(userCurdConfig, user)
				if goBack {
					// User selected back, so restart SetupCurd to show main menu again
					SetupCurd(userCurdConfig, anime, user, databaseAnimes, databaseFile)
					return
				}
				ExitCurd(nil)
			} else if categorySelection.Key == "DOWNLOAD" {
				ClearScreen()
				DownloadAnimeMenu(userCurdConfig, user, databaseAnimes)
				// After download, go back to main menu
				SetupCurd(userCurdConfig, anime, user, databaseAnimes, databaseFile)
				return
			} else if categorySelection.Key == "UNTRACKED" {
				ClearScreen()
				WatchUntracked(userCurdConfig)
			} else if categorySelection.Key == "CONTINUE_LAST" {
				anime.Ep.ContinueLast = true
			}

			ClearScreen()
		}

		if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
			animeListMapPreview = make(map[string]RofiSelectPreview)
			for _, entry := range getEntriesByCategory(user.AnimeList, categorySelection.Key) {
				title := entry.Media.Title.English
				if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
					title = entry.Media.Title.Romaji
				}
				animeListMapPreview[strconv.Itoa(entry.Media.ID)] = RofiSelectPreview{
					Title:      title,
					CoverImage: entry.CoverImage,
				}
			}
		} else {
			animeListOptions = make([]SelectionOption, 0)
			for _, entry := range getEntriesByCategory(user.AnimeList, categorySelection.Key) {
				title := entry.Media.Title.English
				if title == "" || userCurdConfig.AnimeNameLanguage == "romaji" {
					title = entry.Media.Title.Romaji
				}
				animeListOptions = append(animeListOptions, SelectionOption{
					Key:   strconv.Itoa(entry.Media.ID),
					Label: title,
				})
			}
			// Add "Back" option at the beginning
			animeListOptions = append([]SelectionOption{{Key: "back", Label: "<- Back"}}, animeListOptions...)
		}
	}

	var anilistSelectedOption SelectionOption
	var userQuery string

	if anime.Ep.ContinueLast {
		// Get the last watched anime ID from the curd_id file
		curdIDPath := filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "curd_id")
		curdIDBytes, err := os.ReadFile(curdIDPath)
		if err != nil {
			Log(fmt.Sprintf("Error reading curd_id file: %v", err))
			ExitCurd(fmt.Errorf("Error reading curd_id file"))
		}

		lastWatchedID, err := strconv.Atoi(strings.TrimSpace(string(curdIDBytes)))
		if err != nil {
			Log(fmt.Sprintf("Error converting curd_id to integer: %v", err))
			ExitCurd(fmt.Errorf("Error converting curd_id to integer"))
		}

		anime.AnilistId = lastWatchedID
		anilistSelectedOption.Key = strconv.Itoa(lastWatchedID)
	} else {
		// Select anime to watch (Anilist)
		var err error
		if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
			anilistSelectedOption, err = DynamicSelectPreviewWithBack(animeListMapPreview, true, "<- Back")
		} else {
			// Add "Add new anime" option to the slice
			animeListOptions = append(animeListOptions, SelectionOption{
				Key:   "add_new",
				Label: "Add new anime",
			})

			anilistSelectedOption, err = DynamicSelect(animeListOptions)
		}
		if err != nil {
			Log(fmt.Sprintf("Error selecting anime: %v", err))
			ExitCurd(fmt.Errorf("Error selecting anime"))
		}

		Log(anilistSelectedOption)

		if anilistSelectedOption.Key == "-1" {
			ExitCurd(nil)
		}

		// Handle "Back" option - restart SetupCurd to show category selection again
		if anilistSelectedOption.Key == "back" {
			SetupCurd(userCurdConfig, anime, user, databaseAnimes, databaseFile)
			return
		}

		if anilistSelectedOption.Label == "add_new" || anilistSelectedOption.Key == "add_new" {
			anilistSelectedOption = AddNewAnime(userCurdConfig, anime, user, databaseAnimes)
		}
	}

	// The selection key may be an AniList ID or a MAL ID depending on the configured tracking service.
	selectedID, err := strconv.Atoi(anilistSelectedOption.Key)
	if err != nil {
		Log(fmt.Sprintf("Error converting selected anime ID: %v", err))
		ExitCurd(fmt.Errorf("Error converting selected anime ID"))
	}

	// Ensure Anime.AnilistId always holds the AniList ID and Anime.MalId holds the MAL ID.
	service := GetTrackingService(userCurdConfig)
	if service == "mal" {
		// selectedID is a MAL ID; convert to AniList ID
		malID := selectedID
		anilistID, convErr := ConvertMALIDToAnilist(malID, "")
		if convErr != nil {
			Log(fmt.Sprintf("Failed to convert MAL ID to AniList ID: %v", convErr))
			ExitCurd(fmt.Errorf("Failed to convert MAL ID to AniList ID"))
		}
		anime.AnilistId = anilistID
		anime.MalId = malID
	} else {
		// selectedID is an AniList ID
		anime.AnilistId = selectedID
		// try to fetch MAL ID (non-fatal here; main.go may fetch later)
		malID, _ := GetAnimeMalID(anime.AnilistId)
		if malID != 0 {
			anime.MalId = malID
		}
	}

	// Fetch anime metadata from user list
	idStrToFind := strconv.Itoa(selectedID)
	selectedAnilistAnime, err := FindAnimeByAnilistID(user.AnimeList, idStrToFind)
	if err != nil {
		Log(fmt.Sprintf("Can not find the anime in animelist: %v", err))
		ExitCurd(fmt.Errorf("Can not find the anime in animelist"))
	}

	// Set anime metadata
	anime.Title = selectedAnilistAnime.Media.Title
	anime.TotalEpisodes = selectedAnilistAnime.Media.Episodes
	anime.CoverImage = selectedAnilistAnime.CoverImage
	anime.Ep.Number = selectedAnilistAnime.Progress + 1
	userQuery = anime.Title.Romaji

	// Find anime in Local history
	animePointer := LocalFindAnime(*databaseAnimes, anime.AnilistId, "")

	// if anime found in database, use it
	if animePointer != nil {
		anime.ProviderId = animePointer.ProviderId
		anime.ProviderName = animePointer.ProviderName

		anime.Ep.Player.PlaybackTime = animePointer.Ep.Player.PlaybackTime
		if anime.Ep.Number == animePointer.Ep.Number {
			anime.Ep.Resume = true
		}
	}

	// If ProviderId is missing, resolve provider mapping
	if anime.ProviderId == "" {
		Log("ProviderId is missing, resolving mapping...")
		outcome, err := ResolveAnimeProviderMapping(userCurdConfig, anime, string(userQuery), selectedAnilistAnime)
		if err != nil {
			ExitCurd(fmt.Errorf("Failed to resolve anime mapping: %v", err))
		}
		if outcome != ProviderMappingOK {
			ExitCurd(nil)
		}

		// Save the anime selection to database immediately so we don't ask again
		err = LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, anime.Ep.Duration, GetAnimeName(*anime), anime.ProviderName)
		if err != nil {
			Log(fmt.Sprintf("Warning: Failed to save anime selection to database: %v", err))
		}
	}
	// Prompt user to select a provider for this session
	selectedProvider := PromptProviderSelection()
	if selectedProvider == "" {
		ExitCurd(nil)
		return
	}
	Log(fmt.Sprintf("User selected provider: %s (current: %s)", selectedProvider, anime.ProviderName))
	CurdOut(fmt.Sprintf("\033[1;36mUser explicitly selected provider: %s\033[0m", selectedProvider))
	if selectedProvider != anime.ProviderName {
		anime.ProviderName = selectedProvider
		anime.ProviderId = "" // Force re-search on the chosen provider
		Log(fmt.Sprintf("Switched provider to %s, will search for anime on new provider", selectedProvider))
	}
	userCurdConfig.Provider = selectedProvider

	// If anime is not in watching list, prompt user to add it into watching list
	isInWatchingList := false
	// When using MAL tracking, entry.Media.ID contains MAL IDs
	// When using AniList tracking, entry.Media.ID contains AniList IDs
	idToCheck := anime.AnilistId
	if service == "mal" {
		idToCheck = anime.MalId
	}
	for _, entry := range user.AnimeList.Watching {
		if entry.Media.ID == idToCheck {
			isInWatchingList = true
			break
		}
	}

	if !isInWatchingList {
		// Create options for the prompt
		options := []SelectionOption{
			{Key: "yes", Label: "Add to watching list"},
			{Key: "no", Label: "Continue without adding"},
		}

		// Use rofi for selection
		selectedOption, err := DynamicSelect(options)
		if err != nil {
			Log("Error in selection: " + err.Error())
			ExitCurd(err)
		}

		if selectedOption.Key == "yes" {
			err = AddAnimeToWatchingListUnified(anime.AnilistId, user.Token, userCurdConfig)
			if err != nil {
				Log("Error adding anime to watching list: " + err.Error())
				ExitCurd(err)
			}
			// Refresh user's anime list after adding
			if userCurdConfig.RofiSelection && userCurdConfig.ImagePreview {
				anilistUserDataPreview, err := GetUserDataPreview(user.Token, user.Id)
				if err != nil {
					Log("Error refreshing anime list: " + err.Error())
					ExitCurd(err)
				}
				user.AnimeList = ParseAnimeList(anilistUserDataPreview)
			} else {
				anilistUserData, err := GetUserData(user.Token, user.Id)
				if err != nil {
					Log("Error refreshing anime list: " + err.Error())
					ExitCurd(err)
				}
				user.AnimeList = ParseAnimeList(anilistUserData)
			}
		} else if selectedOption.Key == "-1" {
			ExitCurd(nil)
		}
	}

	// If upstream is ahead, update the episode number
	// Use the correct ID format based on tracking service
	idStrToFind = strconv.Itoa(anime.AnilistId)
	if service == "mal" {
		idStrToFind = strconv.Itoa(anime.MalId)
	}
	if temp_anime, err := FindAnimeByAnilistID(user.AnimeList, idStrToFind); err == nil {
		if temp_anime.Progress > anime.Ep.Number {
			anime.Ep.Number = temp_anime.Progress
			anime.Ep.Player.PlaybackTime = 0
			anime.Ep.Resume = false
		}
	}

	// Always fetch up-to-date anime metadata (airing status + total episodes).
	// This ensures anime.IsAiring is correctly set so we don't accidentally
	// show a rating prompt or mark a seasonal anime as completed when the user
	// catches up to the latest released episode.
	{
		updatedAnime, err := GetAnimeDataByID(anime.AnilistId, user.Token)
		if err != nil {
			Log(fmt.Sprintf("Error getting updated anime data: %v", err))
		} else {
			if anime.TotalEpisodes == 0 {
				anime.TotalEpisodes = updatedAnime.TotalEpisodes
				Log(fmt.Sprintf("Updated total episodes: %d", anime.TotalEpisodes))
			}
			anime.IsAiring = updatedAnime.IsAiring
			Log(fmt.Sprintf("Anime IsAiring: %v", anime.IsAiring))
		}
	}

	if anime.TotalEpisodes == 0 { // If failed to get anime data
		CurdOut("Failed to get anime data. Attempting to retrieve from anime list.")
		animeList, err := SearchAnime(string(userQuery), userCurdConfig.SubOrDub)
		if err != nil {
			CurdOut(fmt.Sprintf("Failed to retrieve anime list: %v", err))
		} else {
			for _, option := range animeList {
				if option.Key == anime.ProviderId {
					// Extract total episodes from the label
					if matches := regexp.MustCompile(`\((\d+) episodes\)`).FindStringSubmatch(option.Label); len(matches) > 1 {
						anime.TotalEpisodes, _ = strconv.Atoi(matches[1])
						CurdOut(fmt.Sprintf("Retrieved total episodes: %d", anime.TotalEpisodes))
						break
					}
				}
			}
		}

		if anime.TotalEpisodes == 0 {
			CurdOut("Still unable to determine total episodes.")
			CurdOut(fmt.Sprintf("Your AniList progress: %d", selectedAnilistAnime.Progress))
			var episodeNumber int
			if userCurdConfig.RofiSelection {
				userInput, err := GetUserInputFromRofi("Enter the episode you want to start from")
				if err != nil {
					Log("Error getting user input: " + err.Error())
					ExitCurd(fmt.Errorf("Error getting user input: " + err.Error()))
				}
				episodeNumber, err = strconv.Atoi(userInput)
			} else {
				fmt.Print("Enter the episode you want to start from: ")
				fmt.Scanln(&episodeNumber)
			}
			anime.Ep.Number = episodeNumber
		} else {
			anime.Ep.Number = selectedAnilistAnime.Progress + 1
		}
	} else if anime.TotalEpisodes < anime.Ep.Number { // Handle weird cases
		Log(fmt.Sprintf("Weird case: anime.TotalEpisodes < anime.Ep.Number: %v < %v", anime.TotalEpisodes, anime.Ep.Number))
		var answer string
		if userCurdConfig.RofiSelection {
			userInput, err := GetUserInputFromRofi("Would like to start the anime from beginning? (y/n)")
			if err != nil {
				Log("Error getting user input: " + err.Error())
				ExitCurd(fmt.Errorf("Error getting user input: " + err.Error()))
			}
			answer = userInput
		} else {
			fmt.Printf("Would like to start the anime from beginning? (y/n)\n")
			fmt.Scanln(&answer)
		}
		if answer == "y" {
			anime.Ep.Number = 1
		} else {
			anime.Ep.Number = anime.TotalEpisodes
		}
	}
}

// CreateOrWriteTokenFile creates the token file if it doesn't exist and writes the token to it
func WriteTokenToFile(token string, filePath string) error {
	// Extract the directory path
	dir := filepath.Dir(filePath)

	// Create all necessary parent directories
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directories: %v", err)
	}

	// Write the token to the file, creating it if it doesn't exist
	err := os.WriteFile(filePath, []byte(token), 0644)
	if err != nil {
		return fmt.Errorf("failed to write token to file: %v", err)
	}

	return nil
}

func StartCurd(userCurdConfig *CurdConfig, anime *Anime) string {
	if err := resolveRuntimeProviderID(userCurdConfig, anime); err != nil {
		Log(fmt.Sprintf("Failed to resolve provider id: %v", err))
		CurdOut("Failed to resolve anime provider id: " + err.Error())
		RestoreScreen()
		os.Exit(1)
	}

	// Validate inputs
	if anime.ProviderId == "" {
		CurdOut("Error: No anime ID found")
		os.Exit(1)
	}
	if anime.Ep.Number <= 0 {
		CurdOut("Error: Invalid episode number")
		os.Exit(1)
	}

	if (anime.Ep.NextEpisode.Number == anime.Ep.Number) && (len(anime.Ep.NextEpisode.Links) > 0) {
		anime.Ep.Links = anime.Ep.NextEpisode.Links
		anime.Ep.StreamReferrer = ""
		anime.Ep.SubtitleURL = ""
		if anime.Ep.NextEpisode.ProviderName != "" {
			anime.ProviderName = anime.Ep.NextEpisode.ProviderName
			anime.ProviderId = anime.Ep.NextEpisode.ProviderId
		}
	} else {
		// Get episode link
		episodeResult, err := ResolveEpisodeURLForPlayback(*userCurdConfig, anime, anime.Ep.Number)
		link := episodeResult.Links
		if len(link) > 0 {
			Log(fmt.Sprintf("Links details from %s/%s: %+v", episodeResult.ProviderName, episodeResult.Mode, link))
		}
		if err != nil {
			linkErr := err
			Log(fmt.Sprintf("ResolveEpisodeURL failed: %v", linkErr))
			if reselectProviderAnime(userCurdConfig, anime, linkErr) {
				episodeResult, err = ResolveEpisodeURLForPlayback(*userCurdConfig, anime, anime.Ep.Number)
				link = episodeResult.Links
				if err == nil {
					Log(fmt.Sprintf("Successfully retrieved %s/%s episode link after provider reselect. Links count: %d", episodeResult.ProviderName, episodeResult.Mode, len(link)))
					anime.Ep.Links = link
					applyStreamPlaybackHints(anime, anime.Ep.Links, episodeResult.LinkHints)
					goto episodeLinksReady
				}
				linkErr = err
				Log(fmt.Sprintf("ResolveEpisodeURL still failed after provider reselect: %v", linkErr))
			}
			for {
				switch promptEpisodeLinkFailureRecovery(userCurdConfig) {
				case "remap":
					if RemapAnimeProviderOnEpisodeFailure(userCurdConfig, anime, nil) {
						episodeResult, err = ResolveEpisodeURLForPlayback(*userCurdConfig, anime, anime.Ep.Number)
						link = episodeResult.Links
						if err == nil && len(link) > 0 {
							anime.Ep.Links = link
							applyStreamPlaybackHints(anime, anime.Ep.Links, episodeResult.LinkHints)
							goto episodeLinksReady
						}
						if err != nil {
							linkErr = err
							Log(fmt.Sprintf("ResolveEpisodeURL failed after provider remap: %v", linkErr))
						}
					}
				case "episode":
					episodeProviderName, episodeProviderID := AnimeProviderID(anime)
					episodeList, listErr := EpisodesList(QualifyProviderID(episodeProviderName, episodeProviderID), userCurdConfig.SubOrDub)
					if listErr != nil {
						CurdOut("No episode list found: " + listErr.Error())
						Log(fmt.Sprintf("EpisodesList failed: %v", listErr))
						continue
					}
					if len(episodeList) == 0 {
						CurdOut("No episodes were returned by the current provider for this anime.")
						Log(fmt.Sprintf("EpisodesList returned no episodes for provider %s and id %s after ResolveEpisodeURL error: %v", episodeProviderName, episodeProviderID, linkErr))
						continue
					}
					episodeNumber, promptErr := promptPositiveEpisodeNumber(userCurdConfig, fmt.Sprintf("Enter the episode (%v episodes)", episodeList[len(episodeList)-1]))
					if promptErr != nil {
						Log("Invalid episode input: " + promptErr.Error())
						CurdOut("Invalid episode number")
						continue
					}
					anime.Ep.Number = episodeNumber
					episodeResult, err = ResolveEpisodeURLForPlayback(*userCurdConfig, anime, anime.Ep.Number)
					link = episodeResult.Links
					if err == nil && len(link) > 0 {
						anime.Ep.Links = link
						applyStreamPlaybackHints(anime, anime.Ep.Links, episodeResult.LinkHints)
						goto episodeLinksReady
					}
					if err != nil {
						linkErr = err
						Log(fmt.Sprintf("ResolveEpisodeURL failed for episode %d: %v", anime.Ep.Number, linkErr))
					} else {
						CurdOut("Failed to get episode link")
					}
				default:
					RestoreScreen()
					return ""
				}
			}
		} else {
			Log(fmt.Sprintf("Successfully retrieved %s/%s episode link on first try. Links count: %d", episodeResult.ProviderName, episodeResult.Mode, len(link)))
		}
		anime.Ep.Links = link
		applyStreamPlaybackHints(anime, anime.Ep.Links, episodeResult.LinkHints)
	}

episodeLinksReady:
	if len(anime.Ep.Links) == 0 {
		CurdOut("No episode links found")
		RestoreScreen()
		return ""
	} else {
		Log(fmt.Sprintf("Episode links validation passed. Found %d links", len(anime.Ep.Links)))
	}

	// Modify the goroutine in main.go where next episode links are fetched
	// Get next episode link in parallel
	go func() {
		nextEpNum := anime.Ep.Number + 1
		if nextEpNum <= anime.TotalEpisodes {
			// Get next canon episode number if filler skip is enabled
			if userCurdConfig.SkipFiller && IsEpisodeFiller(anime.FillerEpisodes, anime.Ep.Number) {
				nextEpNum = GetNextCanonEpisode(anime.FillerEpisodes, nextEpNum)
			}
			nextResult, err := ResolveEpisodeURLForPlayback(*userCurdConfig, anime, nextEpNum)
			if err != nil {
				Log(fmt.Sprintf("Error getting next episode link for ep %d: %v", nextEpNum, err))
			} else {
				anime.Ep.NextEpisode = NextEpisode{
					Number:       nextEpNum,
					Links:        nextResult.Links,
					ProviderName: nextResult.ProviderName,
					ProviderId:   nextResult.ProviderID,
					Mode:         nextResult.Mode,
				}
			}
		} else {
			Log(fmt.Sprintf("Next episode %d exceeds total episodes %d, skipping prefetch", nextEpNum, anime.TotalEpisodes))
		}
	}()

	// Write anime.AnilistId to curd_id in the storage path
	idFilePath := filepath.Join(os.ExpandEnv(userCurdConfig.StoragePath), "curd_id")
	Log(fmt.Sprintf("idFilePath: %v", idFilePath))
	if err := os.MkdirAll(filepath.Dir(idFilePath), 0755); err != nil {
		Log(fmt.Sprintf("Failed to create directory for curd_id: %v", err))
	} else {
		if err := os.WriteFile(idFilePath, []byte(fmt.Sprintf("%d", anime.AnilistId)), 0644); err != nil {
			Log(fmt.Sprintf("Failed to write AnilistId to file: %v", err))
		}
	}

	// Display starting message with cover image and episode info
	if anime.CoverImage != "" && userCurdConfig.ImagePreview && userCurdConfig.RofiSelection {
		// Get the cached image path
		cacheDir := os.ExpandEnv("${HOME}/.cache/curd/images")
		filename := fmt.Sprintf("%x.jpg", md5.Sum([]byte(anime.CoverImage)))
		cachePath := filepath.Join(cacheDir, filename)

		// Display the image if it exists in cache
		_, err := os.Stat(cachePath)
		if err == nil {
			// File exists
			Log(fmt.Sprintf("Image found at %s", cachePath))
			CurdOut(fmt.Sprintf("-i %s \"%s - Episode %d\"", cachePath, GetAnimeName(*anime), anime.Ep.Number))
		} else {
			// File does not exist
			Log(fmt.Sprintf("Image does not exist at %s", cachePath))
			CurdOut(fmt.Sprintf("%s - Episode %d",
				GetAnimeName(*anime),
				anime.Ep.Number))

		}
	} else {
		CurdOut(fmt.Sprintf("%s - Episode %d", GetAnimeName(*anime), anime.Ep.Number))
	}
	mpvSocketPath, err := StartVideo(PrioritizeLink(anime.Ep.Links), []string{}, fmt.Sprintf("%s - Episode %d", GetAnimeName(*anime), anime.Ep.Number), anime)

	if err != nil {
		Log("Failed to start mpv")
		RestoreScreen()
		os.Exit(1)
	}

	return mpvSocketPath
}

func resolveRuntimeProviderID(userCurdConfig *CurdConfig, anime *Anime) error {
	if anime == nil || anime.ProviderId == "" {
		return nil
	}

	providerName, providerID := AnimeProviderID(anime)
	if providerName != "animepahe" || !ProviderEnabled("animepahe") {
		return nil
	}
	if !ProviderStackContains(userCurdConfig, "animepahe") {
		return nil
	}

	provider, err := ProviderByName(providerName)
	if err != nil {
		return err
	}

	query := GetAnimeName(*anime)
	if query == "" {
		query = anime.Title.Romaji
	}
	if query == "" {
		query = anime.Title.English
	}

	resolved, err := resolveProviderID(provider, providerID, query)
	if err != nil {
		return err
	}
	if resolved != "" && resolved != providerID {
		Log(fmt.Sprintf("Resolved Animepahe provider id %s to runtime id %s", providerID, resolved))
		anime.ProviderId = resolved
		anime.ProviderName = providerName
	}

	return nil
}

func reselectProviderAnime(userCurdConfig *CurdConfig, anime *Anime, reason error) bool {
	providerName, _ := AnimeProviderID(anime)
	if providerName != "animepahe" || !ProviderEnabled("animepahe") {
		return false
	}

	if reason != nil {
		Log(fmt.Sprintf("Attempting Animepahe provider reselect after error: %v", reason))
	}

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

	options, err := SearchAnime(query, userCurdConfig.SubOrDub)
	if err != nil {
		Log(fmt.Sprintf("Animepahe provider reselect search failed for %q: %v", query, err))
		return false
	}
	if len(options) == 0 {
		Log(fmt.Sprintf("Animepahe provider reselect found no results for %q", query))
		return false
	}

	CurdOut("The saved Animepahe mapping is stale. Please select the anime again.")
	selected, err := DynamicSelect(options)
	if err != nil || selected.Key == "-1" || selected.Key == "-2" || selected.Key == "" {
		if err != nil {
			Log(fmt.Sprintf("Animepahe provider reselect failed: %v", err))
		}
		return false
	}

	if selectedProviderName, rawProviderID, ok := ParseProviderQualifiedID(selected.Key); ok {
		anime.ProviderName = selectedProviderName
		anime.ProviderId = rawProviderID
	} else {
		anime.ProviderName = "animepahe"
		anime.ProviderId = selected.Key
	}
	anime.Ep.NextEpisode = NextEpisode{}
	Log(fmt.Sprintf("Updated Animepahe ProviderId to %s after stale mapping", anime.ProviderId))
	return true
}

func CheckAndDownloadFiles(storagePath string, filesToCheck []string) error {
	// Create storage directory if it doesn't exist
	storagePath = os.ExpandEnv(storagePath)
	if err := os.MkdirAll(storagePath, 0755); err != nil {
		return fmt.Errorf("failed to create storage directory: %v", err)
	}

	// Base URL for downloading config files
	baseURL := "https://raw.githubusercontent.com/Wraient/curd/refs/heads/main/rofi/"

	// Check each file
	for _, fileName := range filesToCheck {
		filePath := filepath.Join(os.ExpandEnv(storagePath), fileName)

		// Skip if file already exists
		if _, err := os.Stat(filePath); err == nil {
			continue
		}

		// Download file if it doesn't exist
		resp, err := http.Get(baseURL + fileName)
		if err != nil {
			return fmt.Errorf("failed to download %s: %v", fileName, err)
		}
		defer resp.Body.Close()

		// Create the file
		out, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %v", fileName, err)
		}
		defer out.Close()

		// Write the content
		if _, err := io.Copy(out, resp.Body); err != nil {
			return fmt.Errorf("failed to write file %s: %v", fileName, err)
		}
	}

	return nil
}

func getEntriesByCategory(list AnimeList, category string) []Entry {
	switch category {
	case "ALL":
		// Combine all categories into one slice
		allEntries := make([]Entry, 0)
		allEntries = append(allEntries, list.Watching...)
		allEntries = append(allEntries, list.Completed...)
		allEntries = append(allEntries, list.Paused...)
		allEntries = append(allEntries, list.Dropped...)
		allEntries = append(allEntries, list.Planning...)
		allEntries = append(allEntries, list.Rewatching...)
		return allEntries
	case "CURRENT":
		return list.Watching
	case "COMPLETED":
		return list.Completed
	case "PAUSED":
		return list.Paused
	case "DROPPED":
		return list.Dropped
	case "PLANNING":
		return list.Planning
	case "REWATCHING": // Added for completeness, though "ALL" covers it.
		return list.Rewatching
	default:
		return []Entry{}
	}
}

func NextEpisodePromptCLI(userCurdConfig *CurdConfig) bool {
	anime := GetGlobalAnime()

	// Show the next episode number that will be started
	nextEpisodeNum := anime.Ep.Number + 1
	CurdOut(fmt.Sprintf("Start next episode (%d)?", nextEpisodeNum))

	// Create options for the selection - no "quit" option since it's built into selection menu
	options := []SelectionOption{
		{Key: "yes", Label: fmt.Sprintf("Yes, continue to episode %d", nextEpisodeNum)},
	}

	// Use DynamicSelect for CLI mode
	selectedOption, err := DynamicSelect(options)
	if err != nil {
		Log(fmt.Sprintf("Error in CLI next episode prompt selection: %v", err))
		return false
	}

	Log(fmt.Sprintf("CLI User Selected Key: '%s', Label: '%s'", selectedOption.Key, selectedOption.Label))

	if selectedOption.Key == "-1" {
		// User selected to quit via the built-in quit option
		CurdOut("Exiting")
		return false
	}

	return selectedOption.Key == "yes"
}

// NextEpisodePromptContinuous provides a continuous next episode prompt for CLI mode
// This runs throughout the episode duration and handles completion logic
func NextEpisodePromptContinuous(userCurdConfig *CurdConfig, databaseFile string, user *User) {
	anime := GetGlobalAnime()

	for {
		// Check if episode has started
		if !anime.Ep.Started {
			time.Sleep(1 * time.Second)
			continue
		}

		// Check if MPV is still running
		if !IsMPVRunning(anime.Ep.Player.SocketPath) {
			return
		}

		// Show the next episode number that will be started
		nextEpisodeNum := anime.Ep.Number + 1
		CurdOut(fmt.Sprintf("Continue to next episode (%d) or quit?", nextEpisodeNum))

		// Create options for the selection - no "quit" option since it's built into selection menu
		options := []SelectionOption{
			{Key: "yes", Label: "Yes, start next episode now"},
		}

		// Use DynamicSelect for CLI mode
		selectedOption, err := DynamicSelect(options)
		if err != nil {
			Log(fmt.Sprintf("Error in CLI continuous next episode prompt: %v", err))
			break
		}

		Log(fmt.Sprintf("CLI Continuous User Selected Key: '%s', Label: '%s'", selectedOption.Key, selectedOption.Label))

		if selectedOption.Key == "-1" {
			// User selected to quit via the built-in quit option

			// Check completion percentage
			percentageWatched := PercentageWatched(anime.Ep.Player.PlaybackTime, anime.Ep.Duration)

			if int(percentageWatched) >= userCurdConfig.PercentageToMarkComplete {
				// Episode is considered completed, mark it and update progress
				anime.Ep.IsCompleted = true

				// Update local database
				err = LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, ConvertSecondsToMinutes(anime.Ep.Duration), GetAnimeName(*anime), anime.ProviderName)
				if err != nil {
					Log("Error updating local database on quit: " + err.Error())
				}

				// Update progress if not rewatching (synchronously to ensure it completes)
				if !anime.Rewatching {
					err = UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, anime.Ep.Number, userCurdConfig)
					if err != nil {
						Log("Error updating progress on quit: " + err.Error())
					} else {
						CurdOut(fmt.Sprintf("Episode marked as completed! Progress updated: %d", anime.Ep.Number))
					}
				}

				CurdOut(fmt.Sprintf("Episode completed (%.1f%% watched). Exiting.", percentageWatched))
			} else {
				CurdOut(fmt.Sprintf("Episode not completed (%.1f%% watched). Exiting.", percentageWatched))
			}

			ExitMPV(anime.Ep.Player.SocketPath)
			ExitCurd(nil)
			return
		}

		if selectedOption.Key == "yes" {
			// User wants to start next episode immediately
			anime.Ep.IsCompleted = true

			// Update database with completed episode first
			err = LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, anime.Ep.Player.PlaybackTime, ConvertSecondsToMinutes(anime.Ep.Duration), GetAnimeName(*anime), anime.ProviderName)
			if err != nil {
				Log("Error updating local database with completed episode: " + err.Error())
			}

			// Update progress for the completed episode if not rewatching
			if !anime.Rewatching {
				go func() {
					err = UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, anime.Ep.Number, userCurdConfig)
					if err != nil {
						Log("Error updating progress: " + err.Error())
					} else {
						CurdOut(fmt.Sprintf("Episode completed! Progress updated: %d", anime.Ep.Number))
					}
				}()
			}

			// Increment to next episode and update database with next episode number and 0 playback time
			anime.Ep.Number++

			// Use prefetched links if available for the next episode
			if (anime.Ep.NextEpisode.Number == anime.Ep.Number) && (len(anime.Ep.NextEpisode.Links) > 0) {
				anime.Ep.Links = anime.Ep.NextEpisode.Links
				Log(fmt.Sprintf("Using prefetched links for episode %d", anime.Ep.Number))
			} else {
				// Clear links to force fetching new ones
				anime.Ep.Links = []string{}
				Log(fmt.Sprintf("No prefetched links available for episode %d, will fetch new ones", anime.Ep.Number))
			}

			err = LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, 0, 0, GetAnimeName(*anime), anime.ProviderName)
			if err != nil {
				Log("Error updating local database with next episode: " + err.Error())
			}

			CurdOut("Starting next episode now...")
			ExitMPV(anime.Ep.Player.SocketPath)
			return // Exit this function, let the main loop handle next episode
		}
	}
}

// Simple next episode prompt for Rofi mode - just asks if user wants to continue
func NextEpisodePromptRofi(userCurdConfig *CurdConfig) bool {
	anime := GetGlobalAnime()

	// Show the next episode number that will be started
	nextEpisodeNum := anime.Ep.Number + 1

	// Create options for the selection
	options := []SelectionOption{
		{Key: "yes", Label: fmt.Sprintf("Yes, start episode %d", nextEpisodeNum)},
	}

	// Use DynamicSelect for Rofi mode
	selectedOption, err := DynamicSelect(options)
	if err != nil {
		Log(fmt.Sprintf("Error in next episode prompt selection: %v", err))
		return false
	}

	Log(fmt.Sprintf("Rofi User Selected Key: '%s', Label: '%s'", selectedOption.Key, selectedOption.Label))

	return selectedOption.Key == "yes"
}

// StartNextEpisode handles the logic for starting the next episode
// It updates the episode number, resets necessary flags, and handles database updates
func StartNextEpisode(anime *Anime, userCurdConfig *CurdConfig, databaseFile string, user *User) {
	// Save previous episode number for progress update
	prevEpisode := anime.Ep.Number

	// Check if we just completed the last episode of a FINISHED series.
	// For currently-airing anime, skip this block entirely so we don't show the
	// rating prompt or mark the series as completed just because the user caught
	// up with the latest released episode.
	if anime.TotalEpisodes > 0 && anime.Ep.Number == anime.TotalEpisodes && !anime.IsAiring {
		// Handle scoring and completion for the last episode
		HandleLastEpisodeCompletion(userCurdConfig, anime, user)

		// Update progress for the last episode if not rewatching (synchronously to ensure it completes)
		if !anime.Rewatching {
			err := UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, prevEpisode, userCurdConfig)
			if err != nil {
				Log("Error updating progress: " + err.Error())
			} else {
				CurdOut(fmt.Sprintf("Anime progress updated! Latest watched episode: %d", prevEpisode))
			}
		}

		CurdOut("Series completed!")
		ExitCurd(nil)
		return
	}

	// For airing anime where user has caught up to the latest released episode,
	// just update progress and exit gracefully without rating or marking complete.
	if anime.IsAiring && anime.TotalEpisodes > 0 && anime.Ep.Number == anime.TotalEpisodes {
		if !anime.Rewatching {
			err := UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, prevEpisode, userCurdConfig)
			if err != nil {
				Log("Error updating progress for airing anime: " + err.Error())
			} else {
				CurdOut(fmt.Sprintf("Anime progress updated! Latest watched episode: %d", prevEpisode))
			}
		}
		CurdOut("You're caught up! New episodes air weekly.")
		ExitCurd(nil)
		return
	}

	// Increment episode number
	anime.Ep.Number++

	// Check if we've reached the end of the series
	if anime.TotalEpisodes > 0 && anime.Ep.Number > anime.TotalEpisodes {
		CurdOut("Reached end of series")
		ExitCurd(nil)
		return
	}

	// Use prefetched links if available for the next episode
	if (anime.Ep.NextEpisode.Number == anime.Ep.Number) && (len(anime.Ep.NextEpisode.Links) > 0) {
		anime.Ep.Links = anime.Ep.NextEpisode.Links
		Log(fmt.Sprintf("Using prefetched links for episode %d", anime.Ep.Number))
	} else {
		// Clear links to force fetching new ones
		anime.Ep.Links = []string{}
		Log(fmt.Sprintf("No prefetched links available for episode %d, will fetch new ones", anime.Ep.Number))
	}

	// Reset episode flags
	anime.Ep.Started = false
	anime.Ep.IsCompleted = false

	// Log the transition
	Log("Completed episode, starting next.")

	// Update local database
	err := LocalUpdateAnime(databaseFile, anime.AnilistId, anime.ProviderId, anime.Ep.Number, 0, 0, GetAnimeName(*anime), anime.ProviderName)
	if err != nil {
		Log("Error updating local database: " + err.Error())
	}

	// Update progress for the previous episode if not rewatching
	if !anime.Rewatching {
		go func() {
			err = UpdateAnimeProgressDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, prevEpisode, userCurdConfig)
			if err != nil {
				Log("Error updating progress: " + err.Error())
			} else {
				CurdOut(fmt.Sprintf("Anime progress updated! Latest watched episode: %d", prevEpisode))
			}
		}()
	}

	// Output message to user
	CurdOut(fmt.Sprint("Starting next episode: ", anime.Ep.Number))
}

// HandleLastEpisodeCompletion handles scoring and completion for the last episode.
// It is only called for anime that have FINISHED airing (IsAiring == false).
func HandleLastEpisodeCompletion(userCurdConfig *CurdConfig, anime *Anime, user *User) {
	// Safety guard: never rate or mark complete if the anime is still airing.
	// This prevents false-positive completions when the user catches up to the
	// latest released episode of a seasonal series.
	if anime.IsAiring {
		Log("HandleLastEpisodeCompletion called for an airing anime – skipping rating/completion.")
		return
	}

	// Check if this is the last episode and scoring is enabled
	if userCurdConfig.ScoreOnCompletion && anime.TotalEpisodes > 0 && anime.Ep.Number == anime.TotalEpisodes {
		// Prompt user to score the anime
		CurdOut("You've completed this anime! Would you like to rate it?")

		scoreOptions := []SelectionOption{
			{Key: "yes", Label: "Yes, rate this anime"},
			{Key: "no", Label: "No, skip rating"},
		}

		selectedOption, err := DynamicSelect(scoreOptions)
		if err != nil {
			Log(fmt.Sprintf("Error in score prompt selection: %v", err))
			return
		}

		if selectedOption.Key == "yes" {
			err = RateAnimeDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, userCurdConfig)
			if err != nil {
				Log(fmt.Sprintf("Error rating anime: %v", err))
				CurdOut("Failed to rate anime")
			} else {
				CurdOut("Anime rated successfully!")
			}
		}

		// Update anime status to completed on both services (synchronously to ensure it completes)
		if !anime.Rewatching {
			err := UpdateAnimeStatusDual(user.AnilistToken, user.MalToken, anime.AnilistId, anime.MalId, "COMPLETED", userCurdConfig)
			if err != nil {
				Log("Error updating anime status to completed: " + err.Error())
			} else {
				CurdOut("Anime status updated to completed!")
			}
		}
	}
}

// SelectProviderInteractive presents a menu to select which provider to use
// Uses DynamicSelect for automatic rofi/fzf support
func SelectProviderInteractive(links []string) string {
	if len(links) == 0 {
		return ""
	}
	if len(links) == 1 {
		return links[0]
	}

	// Just use the first link for now, since provider system handles quality selection differently
	// We'll trust the provider system to return the prioritized links
	return links[0]
}


// DisplayEpisodeLinks displays all fetched episode links in a user-friendly format
func DisplayEpisodeLinks(links []string) {
}

func PromptTryAnotherProvider(userCurdConfig *CurdConfig) bool {
	options := []SelectionOption{
		{Key: "try_another", Label: "Try another provider"},
	}

	ClearScreen()
	if userCurdConfig.RofiSelection {
		CurdOut("Episode ended or mpv was closed.\nWould you like to:")
	} else {
		CurdOut("\033[1;35mEpisode ended or mpv was closed\033[0m")
	}
	sel, err := DynamicSelect(options)
	if err == nil && sel.Key == "try_another" {
		return true
	}
	return false
}
