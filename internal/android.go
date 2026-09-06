package internal

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/browser"
)

type PlaybackWaitAction string

const (
	WaitActionComplete PlaybackWaitAction = "complete"
	WaitActionRetry    PlaybackWaitAction = "retry_provider"
	WaitActionQuit     PlaybackWaitAction = "quit"
)

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// FindAndroidAmBinary searches for a usable Android activity manager (am) binary.
// It prioritizes Termux's am wrapper ($PREFIX/bin/am or PATH) over /system/bin/am,
// because since Android 8.0 (Oreo), non-root apps cannot invoke /system/bin/am directly.
func FindAndroidAmBinary() string {
	// 1. Check PATH for "am", verifying it's not pointing to /system/bin
	if path, err := exec.LookPath("am"); err == nil {
		if !strings.HasPrefix(path, "/system/") {
			return path
		}
	}

	// 2. Check PATH for "termux-am"
	if path, err := exec.LookPath("termux-am"); err == nil {
		return path
	}

	// 3. Check Termux $PREFIX/bin explicitly
	if prefix := os.Getenv("PREFIX"); prefix != "" {
		for _, name := range []string{"am", "termux-am"} {
			p := filepath.Join(prefix, "bin", name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && (fi.Mode()&0111 != 0) {
				return p
			}
		}
	}

	// 4. Common Termux filesystem locations
	for _, termuxBin := range []string{
		"/data/data/com.termux/files/usr/bin/am",
		"/data/data/com.termux/files/usr/bin/termux-am",
	} {
		if fi, err := os.Stat(termuxBin); err == nil && !fi.IsDir() && (fi.Mode()&0111 != 0) {
			return termuxBin
		}
	}

	// 5. Fallback to LookPath("am") even if under /system (e.g. adb shell or root)
	if path, err := exec.LookPath("am"); err == nil {
		return path
	}

	// 6. Absolute last resort /system/bin/am
	if fi, err := os.Stat("/system/bin/am"); err == nil && !fi.IsDir() && (fi.Mode()&0111 != 0) {
		return "/system/bin/am"
	}

	return ""
}

func tryIntentCommand(name string, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if err != nil {
		return fmt.Errorf("command '%s %s' failed (%w): %s", name, strings.Join(args, " "), err, outStr)
	}
	if strings.HasPrefix(outStr, "Error:") || strings.Contains(outStr, "SecurityException") || strings.Contains(outStr, "ActivityNotFoundException") {
		return fmt.Errorf("activity manager error: %s", outStr)
	}
	if outStr != "" {
		Log(fmt.Sprintf("Android intent succeeded [%s]: %s", name, outStr))
	}
	return nil
}

// BuildAndroidIntentCommand builds the argument list for Android's activity manager (am start)
// to launch the configured media player (defaulting to is.xyz.mpv/.MPVActivity) with the given URL and title.
func BuildAndroidIntentCommand(config *CurdConfig, link string, title string) []string {
	pkg := "is.xyz.mpv"
	activity := ".MPVActivity"
	if config != nil {
		if strings.TrimSpace(config.AndroidPlayerPackage) != "" {
			pkg = strings.TrimSpace(config.AndroidPlayerPackage)
		}
		if strings.TrimSpace(config.AndroidPlayerActivity) != "" {
			activity = strings.TrimSpace(config.AndroidPlayerActivity)
		}
	}

	args := []string{
		"start", "--user", "0",
		"-a", "android.intent.action.VIEW",
		"-d", link,
		"-n", fmt.Sprintf("%s/%s", pkg, activity),
	}

	if strings.TrimSpace(title) != "" {
		args = append(args, "-e", "title", title)
	}

	return args
}

// LaunchAndroidPlayer launches the video link on Android using the best available method:
// 1. Termux am targeting mpv-android (is.xyz.mpv/.MPVActivity) with --user 0 (proven in ani-cli)
// 2. Termux am targeting mpv-android without --user (current user context)
// 3. Generic VIEW intent without component restriction
// 4. termux-open-url (targeting component or generic)
// 5. termux-open (generic)
func LaunchAndroidPlayer(config *CurdConfig, link string, title string) error {
	pkg := "is.xyz.mpv"
	activity := ".MPVActivity"
	if config != nil {
		if strings.TrimSpace(config.AndroidPlayerPackage) != "" {
			pkg = strings.TrimSpace(config.AndroidPlayerPackage)
		}
		if strings.TrimSpace(config.AndroidPlayerActivity) != "" {
			activity = strings.TrimSpace(config.AndroidPlayerActivity)
		}
	}
	component := fmt.Sprintf("%s/%s", pkg, activity)

	var lastErr error
	amBinary := FindAndroidAmBinary()
	if amBinary != "" {
		// Attempt 1: am start --user 0 -a android.intent.action.VIEW -d <link> -n <component> -e title <title>
		argsWithUser := []string{
			"start", "--user", "0",
			"-a", "android.intent.action.VIEW",
			"-d", link,
			"-n", component,
		}
		if strings.TrimSpace(title) != "" {
			argsWithUser = append(argsWithUser, "-e", "title", title)
		}

		Log(fmt.Sprintf("Launching player via: %s %s", amBinary, strings.Join(argsWithUser, " ")))
		err := tryIntentCommand(amBinary, argsWithUser)
		if err == nil {
			return nil
		}
		Log(fmt.Sprintf("am start with --user 0 failed: %v", err))
		lastErr = err

		// Attempt 2: am start without --user 0
		argsWithoutUser := []string{
			"start",
			"-a", "android.intent.action.VIEW",
			"-d", link,
			"-n", component,
		}
		if strings.TrimSpace(title) != "" {
			argsWithoutUser = append(argsWithoutUser, "-e", "title", title)
		}

		Log(fmt.Sprintf("Retrying player via: %s %s", amBinary, strings.Join(argsWithoutUser, " ")))
		err = tryIntentCommand(amBinary, argsWithoutUser)
		if err == nil {
			return nil
		}
		Log(fmt.Sprintf("am start without --user failed: %v", err))
		lastErr = err

		// Attempt 3: am start generic VIEW intent
		argsGeneric := []string{
			"start",
			"-a", "android.intent.action.VIEW",
			"-d", link,
		}
		if strings.TrimSpace(title) != "" {
			argsGeneric = append(argsGeneric, "-e", "title", title)
		}
		err = tryIntentCommand(amBinary, argsGeneric)
		if err == nil {
			return nil
		}
		Log(fmt.Sprintf("am start generic intent failed: %v", err))
		lastErr = err
	} else {
		Log("FindAndroidAmBinary returned empty, no am binary found")
	}

	// Attempt 4: termux-open-url targeting component
	if hasCommand("termux-open-url") {
		err := tryIntentCommand("termux-open-url", []string{link, component})
		if err == nil {
			return nil
		}
		Log(fmt.Sprintf("termux-open-url with component failed: %v", err))

		// Attempt 5: termux-open-url generic
		err = tryIntentCommand("termux-open-url", []string{link})
		if err == nil {
			return nil
		}
		Log(fmt.Sprintf("termux-open-url generic failed: %v", err))
		lastErr = err
	}

	// Attempt 6: termux-open
	if hasCommand("termux-open") {
		err := tryIntentCommand("termux-open", []string{link})
		if err == nil {
			return nil
		}
		Log(fmt.Sprintf("termux-open failed: %v", err))
		lastErr = err
	}

	if lastErr != nil {
		return fmt.Errorf("failed to open external player (%v). Please verify mpv-android is installed (package %s) and run 'pkg install termux-am' in Termux", lastErr, pkg)
	}

	return fmt.Errorf("no Android activity manager or intent launcher found. Please run 'pkg install termux-am' in Termux")
}

type androidWaitModel struct {
	title  string
	epNum  int
	action PlaybackWaitAction
}

func (m androidWaitModel) Init() tea.Cmd {
	return nil
}

func (m androidWaitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			m.action = WaitActionComplete
			return m, tea.Quit
		case tea.KeyEsc:
			m.action = WaitActionRetry
			return m, tea.Quit
		case tea.KeyCtrlC:
			m.action = WaitActionQuit
			return m, tea.Quit
		case tea.KeyRunes:
			switch string(msg.Runes) {
			case "q", "Q":
				m.action = WaitActionQuit
				return m, tea.Quit
			case "p", "P", "r", "R":
				m.action = WaitActionRetry
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m androidWaitModel) View() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("\033[1;36m▶ Playing:\033[0m %s - Episode %d (in external player)\n\n", m.title, m.epNum))
	b.WriteString("Controls:\n")
	b.WriteString("  \033[1;32m[Enter]\033[0m Mark episode as completed & proceed\n")
	b.WriteString("  \033[1;33m[Esc]\033[0m   Change provider (if video is stuck/buffering)\n")
	b.WriteString("  \033[1;31m[q]\033[0m     Quit Curd\n\n")
	b.WriteString("Waiting for action... ")
	return b.String()
}

// WaitForAndroidPlayback pauses the terminal while the external player runs on Android.
// It listens for:
// - Enter: marks episode as completed and proceeds to next episode
// - Esc (or 'p'/'r'): triggers provider retry for the current episode
// - 'q' (or Ctrl+C): quits Curd
func WaitForAndroidPlayback(title string, epNum int) PlaybackWaitAction {
	p := tea.NewProgram(androidWaitModel{title: title, epNum: epNum})
	m, err := p.Run()
	if err == nil {
		if model, ok := m.(androidWaitModel); ok && model.action != "" {
			return model.action
		}
	}

	// Fallback for non-interactive or unsupported terminal sessions
	fmt.Printf("\n▶ Playing: %s - Episode %d (in external player)\n", title, epNum)
	fmt.Println("Controls: [Enter] Complete & proceed | [Esc/r] Change provider | [q] Quit")
	fmt.Print("Action: ")

	reader := bufio.NewReader(os.Stdin)
	line, readErr := reader.ReadString('\n')
	if readErr != nil {
		return WaitActionComplete
	}

	line = strings.TrimSpace(strings.ToLower(line))
	if line == "q" {
		return WaitActionQuit
	}
	if line == "r" || line == "p" || strings.Contains(line, "esc") {
		return WaitActionRetry
	}

	return WaitActionComplete
}

// OpenURL opens a URL in the browser, supporting Termux API or Android VIEW intent when on Android.
func OpenURL(target string) error {
	if IsAndroid() {
		if hasCommand("termux-open-url") {
			cmd := exec.Command("termux-open-url", target)
			if err := cmd.Start(); err == nil {
				return nil
			}
		}
		if am := FindAndroidAmBinary(); am != "" {
			cmd := exec.Command(am, "start", "--user", "0", "-a", "android.intent.action.VIEW", "-d", target)
			if err := cmd.Start(); err == nil {
				return nil
			}
			cmdWithoutUser := exec.Command(am, "start", "-a", "android.intent.action.VIEW", "-d", target)
			if err := cmdWithoutUser.Start(); err == nil {
				return nil
			}
		}
		if hasCommand("termux-open") {
			cmd := exec.Command("termux-open", target)
			if err := cmd.Start(); err == nil {
				return nil
			}
		}
	}

	return browser.OpenURL(target)
}
