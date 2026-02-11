package handler

import (
"encoding/json"
"errors"
"fmt"
"io"
"math/rand"
"net/http"
"strings"
"time"
)

type Request struct {
Input string `json:"input"`
User  struct {
Login string `json:"login"`
} `json:"user"`
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

type ActivitySummary struct {
Commits         int
MergedPRs       int
Reviews         int
Issues          int
DocComments     int
RefactorCommits int
NewRepos        int
LargeCommits    int
FixCommits      int
DocCommits      int
}

type PetState struct {
Mood      int
Kindness  int
Logic     int
Evolution string
Activity  ActivitySummary
}

const (
colorYellow  = "\x1b[33m"
colorBlue    = "\x1b[34m"
colorMagenta = "\x1b[35m"
colorGrey    = "\x1b[37m"
colorReset   = "\x1b[0m"
)

func Handler(w http.ResponseWriter, r *http.Request) {
if r.Method != http.MethodPost {
http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
return
}

var req Request
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
http.Error(w, "invalid json", http.StatusBadRequest)
return
}
login := strings.TrimSpace(req.User.Login)
if login == "" {
login = guessLogin(req.Input)
}
if login == "" {
writeError(w, errors.New("missing user login"))
return
}

client := http.Client{Timeout: 10 * time.Second}
token := readToken(r)
events, err := fetchEvents(client, login, token)
if err != nil {
writeError(w, err)
return
}

summary := summarize(events)
state := buildState(summary)
text := renderStatus(state, login)

w.Header().Set("Content-Type", "application/x-ndjson")
w.WriteHeader(http.StatusOK)
writeEvent(w, "ack", "")
writeEvent(w, "text", text)
writeEvent(w, "done", "")
}

func writeEvent(w io.Writer, event, data string) {
payload := map[string]string{"event": event}
if data != "" {
payload["data"] = data
}
encoded, _ := json.Marshal(payload)
fmt.Fprintln(w, string(encoded))
}

func writeError(w http.ResponseWriter, err error) {
w.Header().Set("Content-Type", "application/x-ndjson")
w.WriteHeader(http.StatusOK)
writeEvent(w, "ack", "")
writeEvent(w, "text", fmt.Sprintf("GitPet stumbled: %s", err.Error()))
writeEvent(w, "done", "")
}

func fetchEvents(client http.Client, login, token string) ([]Event, error) {
url := fmt.Sprintf("https://api.github.com/users/%s/events", login)
req, err := http.NewRequest(http.MethodGet, url, nil)
if err != nil {
return nil, err
}
req.Header.Set("Accept", "application/vnd.github+json")
req.Header.Set("User-Agent", "gitpet-copilot-extension")
if token != "" {
req.Header.Set("Authorization", "Bearer "+token)
}

resp, err := client.Do(req)
if err != nil {
return nil, err
}
defer resp.Body.Close()
if resp.StatusCode >= 400 {
body, _ := io.ReadAll(resp.Body)
return nil, fmt.Errorf("github api error: %s", strings.TrimSpace(string(body)))
}

var events []Event
if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
return nil, err
}
return events, nil
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
case "PullRequestReviewEvent", "PullRequestReviewCommentEvent":
summary.Reviews++
summary.DocComments++
case "IssueCommentEvent":
summary.DocComments++
case "IssuesEvent":
summary.Issues++
case "CreateEvent":
summary.NewRepos++
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

func buildState(summary ActivitySummary) PetState {
activityTotal := summary.Commits + summary.MergedPRs + summary.Reviews + summary.DocComments + summary.RefactorCommits + summary.NewRepos + summary.Issues
mood := 5
if activityTotal == 0 {
mood = 2
} else {
mood = min(100, 10+summary.Commits+summary.MergedPRs*5+summary.Reviews+summary.DocComments+summary.Issues)
}
return PetState{
Mood:      mood,
Kindness:  summary.Reviews * 2,
Logic:     summary.Commits + summary.MergedPRs*3,
Evolution: evolutionFor(summary),
Activity:  summary,
}
}

func renderStatus(state PetState, login string) string {
art := renderArt(state)
lines := []string{
"GitPet Status",
fmt.Sprintf("Keeper: %s", login),
fmt.Sprintf("Evolution: %s", state.Evolution),
fmt.Sprintf("Mood: %d | Kindness: %d | Logic Shards: %d", state.Mood, state.Kindness, state.Logic),
fmt.Sprintf("Activity (7d): Commits %d, Merged PRs %d, Reviews %d, Issues %d, Docs/Comments %d", state.Activity.Commits, state.Activity.MergedPRs, state.Activity.Reviews, state.Activity.Issues, state.Activity.DocComments),
activityTone(state.Activity),
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
if summary.Commits+summary.MergedPRs+summary.Reviews+summary.DocComments+summary.RefactorCommits+summary.NewRepos+summary.Issues == 0 {
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

func activityTone(summary ActivitySummary) string {
total := summary.Commits + summary.MergedPRs + summary.Reviews + summary.DocComments + summary.NewRepos + summary.RefactorCommits + summary.Issues
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

func guessLogin(input string) string {
fields := strings.Fields(input)
if len(fields) >= 3 {
return fields[len(fields)-1]
}
return ""
}

func readToken(r *http.Request) string {
if token := strings.TrimSpace(r.Header.Get("X-GitHub-Token")); token != "" {
return token
}
auth := strings.TrimSpace(r.Header.Get("Authorization"))
if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
return strings.TrimSpace(auth[7:])
}
return ""
}

func min(a, b int) int {
if a < b {
return a
}
return b
}

func init() {
rand.Seed(time.Now().UnixNano())
}
