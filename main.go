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
	colorYellow    = "\x1b[33m"
	colorBlue      = "\x1b[34m"
	colorMagenta   = "\x1b[35m"
	colorGrey      = "\x1b[37m"
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
	fmt.Println("Commands: feed | status | suggest")
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
	tone := activityTone(state.Activity)
	art := renderArt(state)
	lines := []string{
		"GitPet Status",
		fmt.Sprintf("Evolution: %s", state.Evolution),
		fmt.Sprintf("Mood: %d | Kindness: %d | Logic Shards: %d", state.Mood, state.Kindness, state.Logic),
		fmt.Sprintf("Last Sync: %s", displayTime(state.LastSync)),
		fmt.Sprintf("Activity (7d): Commits %d, Merged PRs %d, Reviews %d, Docs/Comments %d", state.Activity.Commits, state.Activity.MergedPRs, state.Activity.Reviews, state.Activity.DocComments),
		tone,
		art,
	}
	return strings.Join(lines, "\n")
}

func renderArt(state PetState) string {
	if state.Evolution == "Lonely" {
		return "(._.)\n /|\\\n / \\\nThe Cache is quiet..."
	}
	color := colorFor(state.Evolution)
	reset := colorReset
	halo := ""
	if state.Kindness >= 2 {
		halo = fmt.Sprintf("%s  _  %s\n", color, reset)
	}
	special := ""
	if state.Evolution == "Pioneer" && rand.Intn(5) == 0 {
		special = fmt.Sprintf("%sFound a tiny treasure chest!%s", color, reset)
	}
	if state.Evolution == "Guardian" {
		special = fmt.Sprintf("%sShielding your logs: You got this.%s", color, reset)
	}
	if state.Evolution == "Bard" {
		special = fmt.Sprintf("%sProverb: %s%s", color, dailyProverb(), reset)
	}
	art := artFor(state.Evolution)
	art = color + art + reset
	if halo != "" {
		art = halo + art
	}
	if special != "" {
		return art + "\n" + special
	}
	return art
}

func artFor(evolution string) string {
	switch evolution {
	case "Pioneer":
		return " /-\\\n(o o)\n/|^|\\\n / \\\n  |"
	case "Guardian":
		return "[===]\n(o_o)\n/|=|\\\n / \\"
	case "Bard":
		return " ~~~\n(o o)\n/|~|\\\n / \\\n (_)"
	case "Void":
		return " . .\n( . )\n . ."
	default:
		return "(o_o)\n /|\\\n / \\"
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
