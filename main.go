package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type PetState struct {
	Version   int             `json:"version"`
	LastSync  string          `json:"last_sync"`
	Mood      int             `json:"mood"`
	Kindness  int             `json:"kindness"`
	Logic     int             `json:"logic_shards"`
	Evolution string          `json:"evolution"`
	Activity  ActivitySummary `json:"activity"`
}

type ActivitySummary struct {
	Commits        int `json:"commits"`
	MergedPRs      int `json:"merged_prs"`
	Reviews        int `json:"reviews"`
	DocComments    int `json:"doc_comments"`
	RefactorCommits int `json:"refactor_commits"`
	NewRepos       int `json:"new_repos"`
	LargeCommits   int `json:"large_commits"`
	Thoughts       int `json:"thought_fragments"`
	FixCommits     int `json:"fix_commits"`
	DocCommits     int `json:"doc_commits"`
}

type Event struct {
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	Payload   json.RawMessage `json:"payload"`
}

type PushPayload struct {
	Size    int `json:"size"`
	Commits []struct {
		Message string `json:"message"`
	} `json:"commits"`
}

type PullRequestPayload struct {
	PullRequest struct {
		Merged bool `json:"merged"`
	} `json:"pull_request"`
}

type CreatePayload struct {
	RefType string `json:"ref_type"`
}

const (
	configFileName = "gh-pet.json"
	colorRed       = "\x1b[31m"
	colorGreen     = "\x1b[32m"
	colorYellow    = "\x1b[33m"
	colorBlue      = "\x1b[34m"
	colorMagenta   = "\x1b[35m"
	colorCyan      = "\x1b[36m"
	colorGrey      = "\x1b[37m"
	colorBold      = "\x1b[1m"
	colorDim       = "\x1b[2m"
	colorReset     = "\x1b[0m"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	rand.Seed(time.Now().UnixNano())

	switch os.Args[1] {
	case "feed":
		if err := runFeed(); err != nil {
			fatal(err)
		}
	case "status":
		if err := runStatus(); err != nil {
			fatal(err)
		}
	case "suggest":
		if err := runSuggest(); err != nil {
			fatal(err)
		}
	case "post-commit":
		if err := runPostCommit(); err != nil {
			fatal(err)
		}
	case "install-hook":
		if err := runInstallHook(); err != nil {
			fatal(err)
		}
	case "prompt":
		runPrompt()
	case "install-prompt":
		if err := runInstallPrompt(); err != nil {
			fatal(err)
		}
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("GitPet (gh extension)")
	fmt.Println("Usage: gh pet <command>")
	fmt.Println("Commands: feed | status | suggest | post-commit | install-hook | prompt | install-prompt")
}

func runFeed() error {
	state, _ := loadState()

	login, err := ghLogin()
	if err != nil {
		return err
	}

	events, err := ghEvents(login)
	if err != nil {
		return err
	}

	summary := summarize(events)
	thoughts := localThoughtFragments()
	summary.Thoughts = thoughts

	activityTotal := summary.Commits + summary.MergedPRs + summary.Reviews + summary.DocComments + summary.RefactorCommits + summary.NewRepos
	state.Logic += summary.Commits + summary.MergedPRs*3
	state.Kindness += summary.Reviews * 2
	if activityTotal == 0 {
		state.Mood = max(0, state.Mood-1)
	} else {
		state.Mood = min(100, state.Mood+summary.Commits+summary.MergedPRs*5+summary.Reviews+summary.DocComments)
	}
	if thoughts > 0 {
		state.Mood = min(100, state.Mood+1)
	}

	state.Evolution = evolutionFor(summary)
	state.Activity = summary
	state.LastSync = time.Now().UTC().Format(time.RFC3339)
	state.Version = 1

	if err := saveState(state); err != nil {
		return err
	}

	if summary.LargeCommits > 0 {
		shake()
	}

	fmt.Println("Fed GitPet with fresh activity.")
	fmt.Printf("Commits: %d | Merged PRs: %d | Reviews: %d | Docs/Comments: %d\n", summary.Commits, summary.MergedPRs, summary.Reviews, summary.DocComments)
	if summary.MergedPRs > 0 {
		printFireworks(state.Evolution)
	}
	fmt.Printf("Mood: %d | Kindness: %d | Logic Shards: %d\n", state.Mood, state.Kindness, state.Logic)
	fmt.Printf("Evolution: %s\n", state.Evolution)
	return nil
}

func runPrompt() {
	state, _ := loadState()
	if state.Evolution == "" {
		state.Evolution = "Lonely"
	}
	// Compact one-line prompt: 🐾Pioneer(◕‿◕)██░░░░░░░░
	face := promptFace(state.Mood)
	bar := promptBar(state.Mood)
	fmt.Printf("🐾%s%s%s", face, bar, state.Evolution)
}

func promptFace(mood int) string {
	switch {
	case mood >= 80:
		return "ᐛ "
	case mood >= 60:
		return "◕‿◕ "
	case mood >= 40:
		return "•‿• "
	case mood >= 20:
		return "•_• "
	case mood > 0:
		return "._. "
	default:
		return ";_; "
	}
}

func promptBar(mood int) string {
	filled := mood / 20
	if filled > 5 {
		filled = 5
	}
	empty := 5 - filled
	return strings.Repeat("█", filled) + strings.Repeat("░", empty) + " "
}

func runInstallPrompt() error {
	// Get the absolute path to gh-pet binary
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find GitPet binary: %w", err)
	}
	exePath, _ = filepath.Abs(exePath)

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// Detect shell
	shell := os.Getenv("SHELL")
	var rcFile string
	var snippet string

	gitpetPrompt := fmt.Sprintf(`
# GitPet prompt — shows pet status in your terminal
gitpet_prompt() {
  local pet
  pet=$("%s" prompt 2>/dev/null)
  if [[ -n "$pet" ]]; then
    echo "$pet "
  fi
}
`, exePath)

	if strings.Contains(shell, "zsh") {
		rcFile = filepath.Join(home, ".zshrc")
		snippet = gitpetPrompt + `setopt PROMPT_SUBST
RPROMPT='$(gitpet_prompt)'
`
	} else {
		rcFile = filepath.Join(home, ".bashrc")
		snippet = gitpetPrompt + `PS1='$(gitpet_prompt)'"$PS1"
`
	}

	// Check if already installed
	if data, err := os.ReadFile(rcFile); err == nil {
		if strings.Contains(string(data), "GitPet prompt") {
			fmt.Printf("%s✓ GitPet prompt already installed in %s%s\n", colorGreen, rcFile, colorReset)
			return nil
		}
	}

	f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(snippet); err != nil {
		return err
	}

	fmt.Printf("%s✓ GitPet prompt installed in %s%s\n", colorGreen, rcFile, colorReset)
	if strings.Contains(shell, "zsh") {
		fmt.Println("  GitPet will show in RPROMPT (right side)")
	} else {
		fmt.Println("  GitPet will show at the start of your prompt")
	}
	fmt.Println("  Run: source", rcFile)
	fmt.Println()
	fmt.Print("  Preview: ")
	runPrompt()
	fmt.Println()
	return nil
}

func runPostCommit() error {
	state, _ := loadState()

	// Get the latest commit message
	commitMsg := ""
	if out, err := exec.Command("git", "log", "-1", "--pretty=%s").Output(); err == nil {
		commitMsg = strings.TrimSpace(string(out))
	}

	// Auto-sync GitHub activity (replaces manual feed)
	login, err := ghLogin()
	if err == nil {
		events, err := ghEvents(login)
		if err == nil {
			summary := summarize(events)
			state.Activity = summary
			state.Evolution = evolutionFor(summary)
			state.Logic += summary.Commits + summary.MergedPRs*3
			state.Kindness += summary.Reviews * 2
		}
	}

	// Boost mood for this commit
	state.Mood = min(100, state.Mood+3)
	state.Logic += 1
	state.LastSync = time.Now().UTC().Format(time.RFC3339)
	state.Version = 1
	if state.Evolution == "" || state.Evolution == "Lonely" {
		state.Evolution = "Pioneer"
	}

	if err := saveState(state); err != nil {
		return err
	}

	// Proactively display GitPet status with praise
	fmt.Println()
	fmt.Println(renderPostCommit(state, commitMsg))
	return nil
}

func runInstallHook() error {
	// Find the git root
	out, err := exec.Command("git", "rev-parse", "--git-dir").Output()
	if err != nil {
		return fmt.Errorf("not a git repository")
	}
	gitDir := strings.TrimSpace(string(out))
	hookDir := filepath.Join(gitDir, "hooks")
	hookPath := filepath.Join(hookDir, "post-commit")

	// Get the absolute path to gh-pet binary
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find GitPet binary: %w", err)
	}
	exePath, _ = filepath.Abs(exePath)

	hookContent := fmt.Sprintf(`#!/usr/bin/env bash
# GitPet post-commit hook — auto-feed & show status
"%s" post-commit
`, exePath)

	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return err
	}

	// Check if hook already exists
	if data, err := os.ReadFile(hookPath); err == nil {
		if strings.Contains(string(data), "GitPet") {
			fmt.Printf("%s✓ GitPet hook already installed at %s%s\n", colorGreen, hookPath, colorReset)
			return nil
		}
		// Append to existing hook
		hookContent = string(data) + "\n" + hookContent
	}

	if err := os.WriteFile(hookPath, []byte(hookContent), 0o755); err != nil {
		return err
	}
	fmt.Printf("%s✓ GitPet post-commit hook installed!%s\n", colorGreen, colorReset)
	fmt.Printf("  → %s\n", hookPath)
	fmt.Println("  GitPet will now auto-show after every commit 🐾")
	return nil
}

func runStatus() error {
	state, _ := loadState()
	if state.Evolution == "" {
		state.Evolution = "Lonely"
	}
	fmt.Println(renderStatus(state))
	return nil
}

func runSuggest() error {
	state, _ := loadState()
	personality := state.Evolution
	if personality == "" || personality == "Lonely" {
		personality = "Companion"
	}
	prompt := fmt.Sprintf("Generate 5 creative git commit messages in the voice of the %s GitPet. Mood: %s. Be supportive and witty, one line each.", personality, moodDescriptor(state.Mood))
	cmd := exec.Command("gh", "copilot", "suggest", prompt)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func renderStatus(state PetState) string {
	color := colorFor(state.Evolution)
	art := renderArt(state)
	moodBar := renderMoodBar(state.Mood)
	face := moodFace(state.Mood)
	tone := activityTone(state.Activity)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n%s%s╭──────────────────────────────────╮%s\n", colorBold, color, colorReset))
	sb.WriteString(fmt.Sprintf("%s│%s       🐾 GitPet Status           %s│%s\n", color, colorReset, color, colorReset))
	sb.WriteString(fmt.Sprintf("%s├──────────────────────────────────┤%s\n", color, colorReset))
	sb.WriteString(fmt.Sprintf("%s│%s  Evolution : %-20s%s│%s\n", color, colorReset, state.Evolution, color, colorReset))
	sb.WriteString(fmt.Sprintf("%s│%s  Mood      : %s %s%s\n", color, colorReset, moodBar, face, colorReset))
	sb.WriteString(fmt.Sprintf("%s│%s  Kindness  : %-5d  Shards: %-5d%s│%s\n", color, colorReset, state.Kindness, state.Logic, color, colorReset))
	sb.WriteString(fmt.Sprintf("%s│%s  Synced    : %-20s%s│%s\n", color, colorReset, displayTime(state.LastSync), color, colorReset))
	sb.WriteString(fmt.Sprintf("%s├──────────────────────────────────┤%s\n", color, colorReset))
	sb.WriteString(fmt.Sprintf("%s│%s  7d: %dc %dp %dr %dd\n", color, colorReset,
		state.Activity.Commits, state.Activity.MergedPRs, state.Activity.Reviews, state.Activity.DocComments))
	sb.WriteString(fmt.Sprintf("%s├──────────────────────────────────┤%s\n", color, colorReset))
	for _, line := range strings.Split(art, "\n") {
		sb.WriteString(fmt.Sprintf("%s│%s  %s\n", color, colorReset, line))
	}
	sb.WriteString(fmt.Sprintf("%s├──────────────────────────────────┤%s\n", color, colorReset))
	sb.WriteString(fmt.Sprintf("%s│%s  %s\n", color, colorReset, tone))
	sb.WriteString(fmt.Sprintf("%s╰──────────────────────────────────╯%s\n", color, colorReset))
	return sb.String()
}

func renderPostCommit(state PetState, commitMsg string) string {
	color := colorFor(state.Evolution)
	art := renderArt(state)
	praise := randomPraise()
	face := moodFace(state.Mood)
	moodBar := renderMoodBar(state.Mood)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s%s╭──── 🐾 GitPet ─────────────────╮%s\n", colorBold, color, colorReset))
	for _, line := range strings.Split(art, "\n") {
		sb.WriteString(fmt.Sprintf("%s│%s  %s\n", color, colorReset, line))
	}
	sb.WriteString(fmt.Sprintf("%s│%s\n", color, colorReset))
	sb.WriteString(fmt.Sprintf("%s│%s  %s %s\n", color, colorReset, face, praise))
	sb.WriteString(fmt.Sprintf("%s│%s  Mood: %s  +3 ⬆\n", color, colorReset, moodBar))
	if commitMsg != "" {
		display := commitMsg
		if len(display) > 28 {
			display = display[:25] + "..."
		}
		sb.WriteString(fmt.Sprintf("%s│%s  📝 %s\n", color, colorReset, display))
	}
	sb.WriteString(fmt.Sprintf("%s╰──────────────────────────────────╯%s\n", color, colorReset))
	return sb.String()
}

func renderMoodBar(mood int) string {
	filled := mood / 10
	if filled > 10 {
		filled = 10
	}
	empty := 10 - filled
	var color string
	switch {
	case mood >= 70:
		color = colorGreen
	case mood >= 40:
		color = colorYellow
	case mood > 0:
		color = colorRed
	default:
		color = colorGrey
	}
	return fmt.Sprintf("%s%s%s%s", color, strings.Repeat("█", filled), colorDim+strings.Repeat("░", empty)+colorReset, colorReset)
}

func moodFace(mood int) string {
	switch {
	case mood >= 80:
		return "ᕕ( ᐛ )ᕗ"
	case mood >= 60:
		return "(◕‿◕)"
	case mood >= 40:
		return "(•‿•)"
	case mood >= 20:
		return "(•_•)"
	case mood > 0:
		return "(._. )"
	default:
		return "(；_；)"
	}
}

func randomPraise() string {
	praises := []string{
		"Nice commit! 🔥",
		"You're on fire! 💪",
		"Keep it up! ✨",
		"Great work! 🌟",
		"Awesome sauce! 🎉",
		"You rock! 🤘",
		"Legendary! ⚡",
		"Brilliant! 💎",
		"Ship it! 🚀",
		"Code warrior! ⚔️",
		"Well done! 🏆",
		"Commit hero! 🦸",
	}
	return praises[rand.Intn(len(praises))]
}

func renderArt(state PetState) string {
	art := artFor(state.Evolution)
	special := ""
	if state.Evolution == "Pioneer" && rand.Intn(5) == 0 {
		special = "\n🗝️  Found a tiny treasure chest!"
	}
	if state.Evolution == "Guardian" {
		special = "\n🛡️  Shielding your logs."
	}
	if state.Evolution == "Bard" {
		special = fmt.Sprintf("\n📜 %s", dailyProverb())
	}
	return art + special
}

func artFor(evolution string) string {
	switch evolution {
	case "Pioneer":
		return "" +
			"    ╭───╮\n" +
			"   (⊙ ⊙ )\n" +
			"  ╭┤ ▽ ├╮  ⛏️\n" +
			"  │╰───╯│\n" +
			"  ╰┬───┬╯\n" +
			"   │   │\n" +
			"   ╰───╯"
	case "Guardian":
		return "" +
			"   ╔═══╗\n" +
			"   ║ ⊕ ║\n" +
			"  ╭╨───╨╮\n" +
			"  (◉_◉ )\n" +
			"  ├┤═══├┤ 🛡️\n" +
			"  ╰┬───┬╯\n" +
			"   │   │\n" +
			"   ╰───╯"
	case "Bard":
		return "" +
			"   ♪ ♫ ♪\n" +
			"   ╭~~~╮\n" +
			"  (◕ ◡ ◕)\n" +
			"  ╭┤ ♪ ├╮  📜\n" +
			"  │╰~~~╯│\n" +
			"  ╰┬───┬╯\n" +
			"   │   │\n" +
			"   ╰─♪─╯"
	case "Void":
		return "" +
			"    · · ·\n" +
			"   ╭─·─╮\n" +
			"  ( ·_· )\n" +
			"  ┤     ├\n" +
			"   · · ·\n" +
			"    ···"
	case "Lonely":
		return "" +
			"   ╭───╮\n" +
			"  (；_；)\n" +
			"  ╭┤   ├╮\n" +
			"  │╰───╯│\n" +
			"  ╰┬───┬╯  💤\n" +
			"   │   │\n" +
			"   ╰───╯\n" +
			"  zzz..."
	default:
		return "" +
			"   ╭───╮\n" +
			"  (o_o )\n" +
			"  ╭┤   ├╮\n" +
			"  │╰───╯│\n" +
			"  ╰┬───┬╯\n" +
			"   │   │\n" +
			"   ╰───╯"
	}
}

func evolutionFor(summary ActivitySummary) string {
	if summary.Commits+summary.MergedPRs+summary.Reviews+summary.DocComments+summary.RefactorCommits+summary.NewRepos == 0 {
		return "Lonely"
	}
	pioneer := summary.Commits + summary.NewRepos*2
	guardian := summary.Reviews*2 + summary.MergedPRs*2 + summary.FixCommits
	bard := summary.DocComments*2 + summary.DocCommits
	voidScore := summary.RefactorCommits * 2
	best := "Pioneer"
	bestScore := pioneer
	if guardian > bestScore {
		best = "Guardian"
		bestScore = guardian
	}
	if bard > bestScore {
		best = "Bard"
		bestScore = bard
	}
	if voidScore > bestScore {
		best = "Void"
		bestScore = voidScore
	}
	_ = bestScore
	return best
}

func colorFor(evolution string) string {
	switch evolution {
	case "Pioneer":
		return colorYellow
	case "Guardian":
		return colorBlue
	case "Bard":
		return colorMagenta
	case "Void":
		return colorGrey
	default:
		return colorGrey
	}
}

func summarize(events []Event) ActivitySummary {
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	summary := ActivitySummary{}
	for _, event := range events {
		if event.CreatedAt.Before(cutoff) {
			continue
		}
		switch event.Type {
		case "PushEvent":
			var payload PushPayload
			if json.Unmarshal(event.Payload, &payload) == nil {
				summary.Commits += len(payload.Commits)
				if payload.Size >= 10 {
					summary.LargeCommits++
				}
				for _, commit := range payload.Commits {
					classifyCommit(commit.Message, &summary)
				}
			}
		case "PullRequestEvent":
			var payload PullRequestPayload
			if json.Unmarshal(event.Payload, &payload) == nil && payload.PullRequest.Merged {
				summary.MergedPRs++
			}
		case "PullRequestReviewEvent":
			summary.Reviews++
		case "PullRequestReviewCommentEvent":
			summary.Reviews++
			summary.DocComments++
		case "IssueCommentEvent":
			summary.DocComments++
		case "CreateEvent":
			var payload CreatePayload
			if json.Unmarshal(event.Payload, &payload) == nil && payload.RefType == "repository" {
				summary.NewRepos++
			}
		}
	}
	return summary
}

func classifyCommit(message string, summary *ActivitySummary) {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "fix") || strings.Contains(lower, "bug") {
		summary.FixCommits++
	}
	if strings.Contains(lower, "doc") || strings.Contains(lower, "readme") || strings.Contains(lower, "comment") {
		summary.DocCommits++
	}
	if strings.Contains(lower, "refactor") || strings.Contains(lower, "cleanup") || strings.Contains(lower, "remove") || strings.Contains(lower, "delete") {
		summary.RefactorCommits++
	}
}

func ghLogin() (string, error) {
	out, err := exec.Command("gh", "api", "user", "--jq", ".login").Output()
	if err != nil {
		return "", fmt.Errorf("gh api user failed: %w", err)
	}
	login := strings.TrimSpace(string(out))
	if login == "" {
		return "", errors.New("unable to determine GitHub login")
	}
	return login, nil
}

func ghEvents(login string) ([]Event, error) {
	cmd := exec.Command("gh", "api", fmt.Sprintf("users/%s/events", login))
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api events failed: %w", err)
	}
	var events []Event
	if err := json.Unmarshal(out, &events); err != nil {
		return nil, fmt.Errorf("unable to parse events: %w", err)
	}
	return events, nil
}

func localThoughtFragments() int {
	if exec.Command("git", "rev-parse", "--is-inside-work-tree").Run() != nil {
		return 0
	}
	status, _ := exec.Command("git", "status", "--porcelain").Output()
	diff, _ := exec.Command("git", "diff", "--stat").Output()
	if len(bytes.TrimSpace(status)) > 0 || len(bytes.TrimSpace(diff)) > 0 {
		return 1
	}
	return 0
}

func loadState() (PetState, error) {
	path, err := configPath()
	if err != nil {
		return PetState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PetState{Mood: 5, Evolution: "Lonely"}, nil
		}
		return PetState{}, err
	}
	var state PetState
	if err := json.Unmarshal(data, &state); err != nil {
		return PetState{}, err
	}
	return state, nil
}

func saveState(state PetState) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func configPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "gh", configFileName), nil
}

func printFireworks(evolution string) {
	color := colorFor(evolution)
	reset := colorReset
	fmt.Println(color + "  .''." + reset)
	fmt.Println(color + " ( * )" + reset)
	fmt.Println(color + "  .''." + reset)
}

func shake() {
	for i := 0; i < 4; i++ {
		fmt.Print("\x1b[1A\x1b[1B")
		time.Sleep(15 * time.Millisecond)
	}
}

func moodDescriptor(mood int) string {
	switch {
	case mood >= 70:
		return "Radiant"
	case mood >= 40:
		return "Steady"
	case mood > 0:
		return "Faint"
	default:
		return "Quiet"
	}
}

func activityTone(summary ActivitySummary) string {
	total := summary.Commits + summary.MergedPRs + summary.Reviews + summary.DocComments + summary.NewRepos + summary.RefactorCommits
	switch {
	case total >= 20:
		return "Intensity: blazing. GitPet is thriving in the Cache."
	case total >= 8:
		return "Intensity: steady. GitPet hums with creative heat."
	case total >= 1:
		return "Intensity: gentle. GitPet feels acknowledged."
	default:
		return "Intensity: quiet. GitPet grows a little lonely."
	}
}

func dailyProverb() string {
	proverbs := []string{
		"Small diffs travel far.",
		"Tests are lanterns in the fog.",
		"Readability is a form of kindness.",
		"Rename first, refactor second.",
		"Bugs fear patient eyes.",
	}
	today := time.Now().YearDay()
	return proverbs[today%len(proverbs)]
}

func displayTime(ts string) string {
	if ts == "" {
		return "Never"
	}
	return ts
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}
