package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// --- Data types (shared with main.go) ---

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
	Commits         int `json:"commits"`
	MergedPRs       int `json:"merged_prs"`
	Reviews         int `json:"reviews"`
	DocComments     int `json:"doc_comments"`
	RefactorCommits int `json:"refactor_commits"`
	NewRepos        int `json:"new_repos"`
	LargeCommits    int `json:"large_commits"`
	Thoughts        int `json:"thought_fragments"`
	FixCommits      int `json:"fix_commits"`
	DocCommits      int `json:"doc_commits"`
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

const configFileName = "gh-pet.json"

func main() {
	rand.Seed(time.Now().UnixNano())

	s := server.NewMCPServer(
		"gitpet",
		"0.3.0",
		server.WithToolCapabilities(true),
	)

	// pet_status tool
	statusTool := mcp.NewTool("pet_status",
		mcp.WithDescription("Show GitPet's current status: evolution, mood, kindness, logic shards, and recent activity summary."),
	)
	s.AddTool(statusTool, handleStatus)

	// pet_feed tool
	feedTool := mcp.NewTool("pet_feed",
		mcp.WithDescription("Feed GitPet by syncing your recent GitHub activity (commits, PRs, reviews) from the last 7 days. Updates mood, evolution, and stats."),
	)
	s.AddTool(feedTool, handleFeed)

	// pet_suggest tool
	suggestTool := mcp.NewTool("pet_suggest",
		mcp.WithDescription("Get creative git commit message suggestions from GitPet based on its current personality and mood."),
		mcp.WithNumber("count",
			mcp.Description("Number of suggestions to generate (default: 5)"),
		),
	)
	s.AddTool(suggestTool, handleSuggest)

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintf(os.Stderr, "gitpet mcp server error: %v\n", err)
		os.Exit(1)
	}
}

func handleStatus(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	state, _ := loadState()
	if state.Evolution == "" {
		state.Evolution = "Lonely"
	}
	text := renderStatus(state)
	return mcp.NewToolResultText(text), nil
}

func handleFeed(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	state, _ := loadState()

	login, err := ghLogin()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get GitHub login: %v", err)), nil
	}

	events, err := fetchEvents(login)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to fetch events: %v", err)), nil
	}

	summary := summarize(events)
	thoughts := localThoughtFragments()
	summary.Thoughts = thoughts

	activityTotal := summary.Commits + summary.MergedPRs + summary.Reviews + summary.DocComments + summary.RefactorCommits + summary.NewRepos
	state.Logic += summary.Commits + summary.MergedPRs*3
	state.Kindness += summary.Reviews * 2
	if activityTotal == 0 {
		state.Mood = maxInt(0, state.Mood-1)
	} else {
		state.Mood = minInt(100, state.Mood+summary.Commits+summary.MergedPRs*5+summary.Reviews+summary.DocComments)
	}
	if thoughts > 0 {
		state.Mood = minInt(100, state.Mood+1)
	}

	state.Evolution = evolutionFor(summary)
	state.Activity = summary
	state.LastSync = time.Now().UTC().Format(time.RFC3339)
	state.Version = 1

	if err := saveState(state); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to save state: %v", err)), nil
	}

	var sb strings.Builder
	sb.WriteString("🍖 Fed GitPet with fresh activity!\n\n")
	sb.WriteString(fmt.Sprintf("Commits: %d | Merged PRs: %d | Reviews: %d | Docs/Comments: %d\n", summary.Commits, summary.MergedPRs, summary.Reviews, summary.DocComments))
	if summary.MergedPRs > 0 {
		sb.WriteString("🎆 Fireworks! PRs merged!\n")
	}
	sb.WriteString(fmt.Sprintf("Mood: %d | Kindness: %d | Logic Shards: %d\n", state.Mood, state.Kindness, state.Logic))
	sb.WriteString(fmt.Sprintf("Evolution: %s\n", state.Evolution))
	sb.WriteString("\n" + renderArt(state))

	return mcp.NewToolResultText(sb.String()), nil
}

func handleSuggest(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	state, _ := loadState()
	personality := state.Evolution
	if personality == "" || personality == "Lonely" {
		personality = "Companion"
	}

	count := 5
	if args := req.GetArguments(); args != nil {
		if c, ok := args["count"].(float64); ok && c > 0 {
			count = int(c)
		}
	}

	suggestions := generateSuggestions(personality, moodDescriptor(state.Mood), count)
	return mcp.NewToolResultText(suggestions), nil
}

// --- Core Logic ---

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

func fetchEvents(login string) ([]Event, error) {
	out, err := exec.Command("gh", "api", fmt.Sprintf("users/%s/events", login)).Output()
	if err != nil {
		// Fallback to direct HTTP if gh CLI not available
		return fetchEventsHTTP(login)
	}
	var events []Event
	if err := json.Unmarshal(out, &events); err != nil {
		return nil, fmt.Errorf("unable to parse events: %w", err)
	}
	return events, nil
}

func fetchEventsHTTP(login string) ([]Event, error) {
	url := fmt.Sprintf("https://api.github.com/users/%s/events", login)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gitpet-mcp-server")

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var events []Event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, err
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
	}
	return best
}

// --- Rendering ---

func renderStatus(state PetState) string {
	tone := activityTone(state.Activity)
	art := renderArt(state)
	lines := []string{
		"🐾 GitPet Status",
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

func activityTone(summary ActivitySummary) string {
	total := summary.Commits + summary.MergedPRs + summary.Reviews + summary.DocComments + summary.NewRepos + summary.RefactorCommits
	switch {
	case total >= 20:
		return "🔥 Intensity: blazing. GitPet is thriving in the Cache."
	case total >= 8:
		return "✨ Intensity: steady. GitPet hums with creative heat."
	case total >= 1:
		return "🌱 Intensity: gentle. GitPet feels acknowledged."
	default:
		return "💤 Intensity: quiet. GitPet grows a little lonely."
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

func generateSuggestions(personality, mood string, count int) string {
	templates := map[string][]string{
		"Pioneer": {
			"🗺️ feat: chart unknown territory in %s",
			"⛏️ feat: dig deeper into the codebase mines",
			"🏗️ feat: lay the foundation for the next expedition",
			"🧭 feat: navigate through uncharted logic",
			"🌄 feat: plant a flag on the summit of progress",
			"🔭 feat: discover a new pattern in the wilderness",
			"🚀 feat: launch into unexplored modules",
		},
		"Guardian": {
			"🛡️ fix: fortify the walls against regression",
			"🔒 fix: seal the breach in input validation",
			"⚔️ fix: defend the tests from flaky behavior",
			"🏰 fix: reinforce the castle of type safety",
			"🗡️ fix: vanquish the lurking null pointer",
			"🛡️ chore: patrol the perimeter of dependencies",
			"⚙️ fix: repair the shield of error handling",
		},
		"Bard": {
			"📜 docs: compose a ballad of API documentation",
			"🎵 docs: sing the changelog's latest verse",
			"📖 docs: illuminate the README with fresh wisdom",
			"🎭 refactor: perform a dramatic code transformation",
			"🎶 docs: harmonize the inline comments",
			"📝 docs: inscribe the wisdom of edge cases",
			"🎪 docs: narrate the story of this module",
		},
		"Void": {
			"🌑 refactor: dissolve unnecessary complexity",
			"✂️ refactor: trim the excess from the void",
			"🕳️ refactor: collapse redundant abstractions",
			"💫 refactor: distill logic to its purest form",
			"🌌 chore: let the void reclaim dead code",
			"⚫ refactor: simplify until nothing remains but clarity",
			"🔮 refactor: reshape the formless into structure",
		},
		"Companion": {
			"💡 feat: breathe life into the first feature",
			"🌱 feat: plant the seed of something new",
			"🤝 chore: set up a welcoming project structure",
			"🎯 feat: take the first step on the journey",
			"✨ feat: spark the initial implementation",
		},
	}

	msgs, ok := templates[personality]
	if !ok {
		msgs = templates["Companion"]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🐾 GitPet (%s, Mood: %s) suggests:\n\n", personality, mood))
	for i := 0; i < count && i < len(msgs); i++ {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, msgs[i]))
	}
	return sb.String()
}

// --- State persistence ---

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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
