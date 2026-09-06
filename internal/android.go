package internal

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
		if hasCommand("am") {
			cmd := exec.Command("am", "start", "--user", "0", "-a", "android.intent.action.VIEW", "-d", target)
			if err := cmd.Start(); err == nil {
				return nil
			}
		}
	}

	return browser.OpenURL(target)
}
