# crew missions, stage 3: the Jira board and deliverables — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A mission shows on the principal's Jira board as one epic with an issue per piece, each assigned to them and moving from To Do to Done as the piece runs. A piece can end in a deliverable (a brief, research, a plan) instead of, or as well as, a pull request: crew collects it, attaches it to the piece's issue, hands it to later pieces, and waits for the principal to accept it unless the domain's auto-merge switch is on.

**Architecture:** A new `internal/jira` package holds a small Jira Cloud REST client, the per-repo config (`<crewDir>/jira.yaml`) and `Board`, which turns crew's seven tracker operations into REST calls. The engine never queues individual writes. After every tick a pure planner, `TrackerOps`, compares the mission state with what the state says Jira already shows (each piece's issue key, lane, flag, sprint and attached files, plus a cursor into the event log for comments) and returns the writes still to send. The engine sends them in order and records each success in state, so a write that fails is simply planned again on the next tick. Deliverables are files a station leaves in `.crew/deliverables/` inside its worktree, a folder git ignores. The engine copies them into the mission folder, and `Decide` gains one status (`delivered`) and one wait kind (`deliverable`).

**Tech Stack:** Go 1.22 module (toolchain 1.24), `gopkg.in/yaml.v3` only, stdlib `net/http`, `mime/multipart`, `net/http/httptest`, `testing`.

**Spec:** `docs/superpowers/specs/2026-11-18-orchestrator-missions-design.md`. Read "What a piece produces", "When a piece is complete", "Jira", "The auto-merge switch" (the deliverables bullet) and "Reaching the principal" before any task. This plan covers **stage 3 of its Build order**.

**Two places where this plan departs from the spec's wording, on purpose:**

1. *"Writes are queued in the mission state and retried."* The state holds what Jira already shows, and the writes still to send are computed from it each tick. The effect is the same (nothing is lost while Jira is down, and nothing is sent twice in normal running), and a crash between an effect and its queue entry can't lose a write.
2. *"crew gives each run a deliverables folder inside the mission folder, outside the worktree."* A station writes to `.crew/deliverables/` inside its worktree, because a headless station may only write under its working directory. `.crew/` ignores itself, so the files never reach git or a pull request. crew copies them to `<mission folder>/deliverables/<piece>/` when the run finishes.

**Not in this plan:** sub-pieces and how they sit under the epic (stage 5; until then every piece is a plain issue under the epic); Jira Data Center or any site that isn't `*.atlassian.net`; starting a sprint when the board has none active; reading anything back from Jira except a deliverable's issue being moved to Done; parallel pieces (stage 4).

## Global Constraints

- The only dependency is `gopkg.in/yaml.v3`. No new modules.
- Before writing Go, load the `use-modern-go` skill and follow it for Go 1.22.
- Config decodes strictly: unknown keys are errors.
- State is immutable: every state-machine function takes a `State` and returns a new one. `Decide` and `TrackerOps` perform no I/O.
- **crew writes to Jira; agents don't.** No agent prompt asks an agent to touch Jira.
- **Credentials come only from the environment:** `CREW_JIRA_EMAIL` and `CREW_JIRA_TOKEN`. They never appear in config, state, events, notes, logs, notifications or error text, and they are removed from the environment of every station and orchestrator process crew starts.
- **crew sends credentials only to `https://<name>.atlassian.net`**, and never follows a redirect with them.
- **Jira never stops a piece.** A Jira failure is a note; the tick goes on and the write is planned again next tick. No Jira call happens inside `Decide`.
- **crew reads one thing back from Jira:** whether the issue of a posted, unaccepted deliverable is in the Done status. Nothing else a person does on the board changes a mission.
- Every issue crew creates is assigned, by default to the account the token belongs to.
- A deliverable is regular files only. crew never follows a symlink when it collects, copies or attaches one.
- State holds file names, issue keys and short messages only: never file contents.
- `runs_at_once` stays 1.
- Tests never call a real Jira site, the real `gh`, a git remote, `osascript` or a harness CLI. Jira is `internal/jira/jiratest`'s local server or a fake `Tracker`.
- The repo is public: examples use `acme/app`, project `ACME` and `https://example.atlassian.net`. No real site, account or email appears in code, tests or docs.
- Files stay under 400 lines and functions under 50 lines.
- Every commit message uses the conventional `type: description` form and ends with the `Co-Authored-By` line naming the model that wrote the commit, and the `Claude-Session` line. The controller passes both to each implementer.

## Review Focus

1. **The token reaches a station, or something crew writes.** A station's environment has no `CREW_JIRA_*` variable; an HTTP error, a note and a formatted `Client` never contain the token; a redirect is refused. → Task 1 `TestClient_TokenNeverPrinted`, `TestClient_RefusesRedirects`; Task 2 `TestHeadlessRunner_DropsJiraCredentials`.
2. **A station leaves a symlink in its deliverables folder that points at a private file.** crew skips it: it is not copied to the mission folder and not attached to an issue. → Task 6 `TestListDelivered_SkipsSymlinks`.
3. **Jira is down for the whole mission.** Every piece still runs to complete; each tick adds one note, not one per write; when Jira is back the board catches up, and a mission that ended meanwhile says how many writes it couldn't send. → Task 5 `TestEngine_JiraDownNeverStopsAPiece`, `TestLoop_DrainsThenSaysWhatIsLeft`.
4. **The engine dies after Jira created an issue and before the key was saved.** The next engine finds the issue by its label and doesn't create a second one. → Task 3 `TestBoard_CreateFindsItsOwnIssue`.
5. **The principal moves an issue to Done by hand at the wrong time.** A code piece's issue moved to Done changes nothing; a deliverable's issue moved to Done is the acceptance, once and only once it is posted. → Task 7 `TestDecide_DoneOnTheBoardAcceptsOnlyAPostedDeliverable`.

---

## Phase A: talking to Jira

### Task 1: The Jira client and its test server

**Files:** Create `internal/jira/client.go`, `internal/jira/issue.go` (create, find, status, move, comment), `internal/jira/extras.go` (attach, flag, sprint, boards, statuses), `internal/jira/client_test.go`, `internal/jira/jiratest/server.go`, `internal/jira/jiratest/server_test.go`.

**Interfaces (Produces):**
```go
package jira

// Client talks to one Jira Cloud site. Its HTTP client times out after 15s and refuses every redirect.
type Client struct {
    Base, Email string        // Base has no trailing slash
    HTTP        *http.Client
    // the token is unexported and of a type whose String and GoString return "[redacted]"
}
func New(base, email, token string) *Client

// Error is a reply outside 200-299. Body is the first 300 bytes of the reply, cut on a rune boundary.
type Error struct{ Method, Path string; Status int; Body string }
func (e *Error) Error() string // "jira <METHOD> <path>: HTTP <status>: <body>"
var ErrNoTransition = errors.New("the issue has no transition to that status from where it is")
// Permanent: retrying can't help. True for ErrNoTransition and for an *Error whose
// status is 400-499 except 401, 403, 408 and 429. False for everything else, nil included.
func Permanent(err error) bool

type Issue struct{ Project, Type, Summary, Description, Assignee, Parent string; Labels []string }

func (c *Client) Myself(ctx context.Context) (accountID, name string, err error)            // GET  /rest/api/3/myself
func (c *Client) Create(ctx context.Context, in Issue) (key string, err error)              // POST /rest/api/3/issue
func (c *Client) FindByLabel(ctx context.Context, project, label string) (key string, found bool, err error) // POST /rest/api/3/search/jql
func (c *Client) Status(ctx context.Context, key string) (string, error)                    // GET  /rest/api/3/issue/{key}?fields=status
func (c *Client) MoveTo(ctx context.Context, key, status string) error                      // GET then POST /rest/api/3/issue/{key}/transitions
func (c *Client) Comment(ctx context.Context, key, text string) error                       // POST /rest/api/3/issue/{key}/comment
func (c *Client) Attach(ctx context.Context, key, name string, body io.Reader) error        // POST /rest/api/3/issue/{key}/attachments
func (c *Client) FlagField(ctx context.Context) (id string, found bool, err error)          // GET  /rest/api/3/field
func (c *Client) SetFlag(ctx context.Context, key, field string, on bool) error             // PUT  /rest/api/3/issue/{key}
func (c *Client) ActiveSprint(ctx context.Context, board int) (id int, name string, found bool, err error) // GET /rest/agile/1.0/board/{board}/sprint?state=active
func (c *Client) AddToSprint(ctx context.Context, sprint int, keys []string) error          // POST /rest/agile/1.0/sprint/{sprint}/issue
func (c *Client) Boards(ctx context.Context, project string) ([]BoardInfo, error)           // GET  /rest/agile/1.0/board?projectKeyOrId=<project>
func (c *Client) Statuses(ctx context.Context, project string) (map[string][]string, error) // GET  /rest/api/3/project/{project}/statuses: issue type name -> status names
type BoardInfo struct{ ID int; Name string }
```

**Request bodies:**

| Call | Body |
|---|---|
| `Create` | `{"fields":{"project":{"key":P},"issuetype":{"name":T},"summary":S,"description":<ADF>,"labels":[…],"assignee":{"accountId":A},"parent":{"key":K}}}`. `assignee` and `parent` are left out when empty. The reply's `key` is returned. |
| `FindByLabel` | `{"jql":"project = \"P\" AND labels = \"L\" ORDER BY created ASC","fields":["summary"],"maxResults":1}`; the first of the reply's `issues[].key`. |
| `MoveTo` | Reads `Status` first and returns nil when it already equals `status` (case-insensitive). Otherwise picks the transition whose `to.name` equals `status` (case-insensitive) and posts `{"transition":{"id":ID}}`. None found: `ErrNoTransition`. |
| `Comment`, `Create`'s description | ADF: `{"type":"doc","version":1,"content":[…]}` with one `paragraph` per line of text; an empty line is a paragraph with no content. |
| `Attach` | `multipart/form-data` with one part named `file` and the header `X-Atlassian-Token: no-check`. |
| `FlagField` | The `id` of the field whose `name` is `Flagged`. |
| `SetFlag` | `{"fields":{"<field>":[{"value":"Impediment"}]}}` to set, `{"fields":{"<field>":null}}` to clear. |
| `AddToSprint` | `{"issues":[…keys]}` |

Every request uses basic auth and `Accept: application/json`. A reply body is read up to 1 MiB.

**`jiratest.Server`** is an in-memory Jira behind `httptest.NewServer`, serving exactly the endpoints above. Later tasks and the end-to-end test use it.
```go
package jiratest
type Issue struct {
    Key, Type, Summary, Description, Assignee, Parent, Status string
    Labels, Comments, Attachments []string
    History []string // every status the issue has been in, in order, starting with To Do
    Flagged bool
    Flags   int      // how many times the flag was set
    Sprint  int
}
func New(t *testing.T) *Server            // project ACME; statuses To Do, In Progress, In Review, Done for every type; a Flagged field; no board
func (s *Server) URL() string
func (s *Server) Issue(key string) (Issue, bool)
func (s *Server) Issues() []Issue          // in creation order; keys are ACME-1, ACME-2, …
func (s *Server) SetStatus(key, status string) // what a person does on the board
func (s *Server) SetActiveSprint(board, sprint int) // sprint 0: the board has sprints and none is active
func (s *Server) DropFlagField()
func (s *Server) Fail(status, times int)   // the next `times` requests get this HTTP status
func (s *Server) Requests() []string       // "METHOD path", in order
func (s *Server) SawAuth() []string        // the Authorization header of each request
```
Any transition is allowed from any status. A request for an issue that doesn't exist gets 404. `Description` and `Comments` hold the ADF flattened back to text, one line per paragraph.

- [ ] **Step 1: Write the failing tests** against `jiratest.New(t)`:
  - `TestCreate_SendsFields`: an issue with every field set → the server's issue has the same type, summary, description, labels, assignee and parent, and status `To Do`.
  - `TestCreate_LeavesOutEmptyAssigneeAndParent`: the recorded request body has no `assignee` and no `parent` key.
  - `TestFindByLabel`: two issues, one labelled `crew-m1-P1` → its key, true; label `nope` → false, nil error.
  - `TestMoveTo_PicksTransitionByTargetStatus`: `MoveTo(key, "in review")` → server status `In Review`.
  - `TestMoveTo_AlreadyThereSendsNothing`: no `POST …/transitions` in `Requests()`.
  - `TestMoveTo_NoSuchStatus`: `MoveTo(key, "Blocked")` → `errors.Is(err, ErrNoTransition)` and `Permanent(err)`.
  - `TestComment_OneParagraphPerLine`: `"a\n\nb"` → the server's comment reads `"a\n\nb"`.
  - `TestAttach_SendsTheFile`: name `brief.md`, body `hello` → the issue's attachments are `["brief.md"]` and the request carried `X-Atlassian-Token: no-check`.
  - `TestFlag_SetAndClear`; `TestFlagField_Missing`: after `DropFlagField()` → `found == false`, nil error.
  - `TestSprint_ActiveAndNone`: `SetActiveSprint(1, 7)` → `7, true`; `SetActiveSprint(1, 0)` → `found == false`, nil error; then `AddToSprint(7, keys)` sets each issue's `Sprint`.
  - `TestPermanent`: table of 400, 404, 422 → true; 401, 403, 408, 429, 500, 503, a network error, nil → false.
  - `TestClient_TokenNeverPrinted`: token `s3cr3t-token`; `Fail(500, 1)` then any call → the error text, `fmt.Sprintf("%+v", client)` and `fmt.Sprintf("%#v", client)` don't contain `s3cr3t-token`.
  - `TestClient_RefusesRedirects`: a second `httptest` server that records requests; the first replies `302` to it → the call returns an error and the second server saw no request.
  - `TestClient_SendsBasicAuth`: every `SawAuth()` entry is `Basic ` + base64 of `email:token`.
- [ ] **Step 2: Run** `go test ./internal/jira/...` — Expected: FAIL (package doesn't exist).
- [ ] **Step 3: Implement** the client and the test server. Keep each REST call in a function of its own over one private `do(ctx, method, path, payload) ([]byte, error)`.
- [ ] **Step 4: Run** `go test -race ./internal/jira/...` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: a Jira Cloud client and a local Jira for tests`

### Task 2: Jira config, credentials, and keeping them from stations

**Files:** Create `internal/config/secrets.go`, `internal/config/secrets_test.go`, `internal/jira/config.go`, `internal/jira/config_test.go`. Modify `internal/run/runner.go` (`exec`), `internal/run/runner_test.go`.

**Interfaces (Produces):**
```go
package config
const EnvJiraEmail, EnvJiraToken = "CREW_JIRA_EMAIL", "CREW_JIRA_TOKEN"
// WithoutSecrets returns environ without the entries of the variables above. It never changes environ.
func WithoutSecrets(environ []string) []string

package jira
const ConfigFile = "jira.yaml"
type Statuses struct {
    ToDo       string `yaml:"to_do"`
    InProgress string `yaml:"in_progress"`
    InReview   string `yaml:"in_review"`
    Done       string `yaml:"done"`
}
type Repo struct {
    Site      string   `yaml:"site"`
    Project   string   `yaml:"project"`
    Board     int      `yaml:"board"`      // 0: issues are not put in a sprint
    Assignee  string   `yaml:"assignee"`   // an account id; "" means the account the token belongs to
    IssueType string   `yaml:"issue_type"` // default Task
    EpicType  string   `yaml:"epic_type"`  // default Epic
    Statuses  Statuses `yaml:"statuses"`   // defaults: To Do, In Progress, In Review, Done
}
type Config struct {
    Version int             `yaml:"version"`
    Repos   map[string]Repo `yaml:"repos"` // "owner/name"
}
// LoadConfig reads <crewDir>/jira.yaml. A missing file is an empty Config and no error.
// Every Repo it returns has its defaults filled in and has passed Validate.
func LoadConfig(crewDir string) (Config, error)
func (c Config) For(repo string) (Repo, bool)
func (c Config) With(repo string, r Repo) Config   // a copy; c is not changed
func SaveConfig(crewDir string, c Config) error    // temp file + rename, mode 0600, with the header comment below
func (r Repo) Validate() error
// Credentials reads the two variables through getenv. When one is empty the error is
// "<NAME> is not set"; when both are, "CREW_JIRA_EMAIL and CREW_JIRA_TOKEN are not set".
func Credentials(getenv func(string) string) (email, token string, err error)
```

**Validation** (each error starts `jira.yaml: repos.<repo>: `, or with no prefix from `Validate`):
- `site` must parse as a URL with scheme `https`, a host ending in `.atlassian.net`, no user info, and a path of `""` or `/`. Error: `site must be https://<name>.atlassian.net, got "<value>"`. A trailing slash is removed on load.
- `project` must match `^[A-Z][A-Z0-9_]{1,9}$`. Error: `project must be a Jira project key like ACME, got "<value>"`.
- `board` must be 0 or more.

**The header `SaveConfig` writes:**
```yaml
# jira.yaml — where crew shows missions in Jira, one entry per repo.
# `crew jira setup` writes this file; you can edit it. Credentials are never
# stored here: crew reads CREW_JIRA_EMAIL and CREW_JIRA_TOKEN from the environment.
```

- [ ] **Step 1: Write the failing tests**
  - `TestWithoutSecrets`: `["A=1","CREW_JIRA_TOKEN=x","CREW_JIRA_EMAIL=y","CREW_JIRA_TOKENS=z"]` → `["A=1","CREW_JIRA_TOKENS=z"]`, and the input slice is unchanged.
  - `TestHeadlessRunner_DropsJiraCredentials`: with `t.Setenv` for both variables and a `fakeharness.sh` mode that prints its environment, the reply contains neither `CREW_JIRA_TOKEN` nor `CREW_JIRA_EMAIL`, and still contains `PATH=`.
  - `TestLoadConfig_MissingFileIsEmpty`; `TestLoadConfig_FillsDefaults`: an entry with only `site` and `project` → `IssueType == "Task"`, `EpicType == "Epic"`, the four default status names.
  - `TestLoadConfig_Refuses`: table: `http://example.atlassian.net`, `https://example.com`, `https://evil.example/x.atlassian.net`, `https://user:pw@example.atlassian.net`, `https://example.atlassian.net/wiki`, project `acme`, project `A`, `board: -1`, an unknown key → each an error naming the repo.
  - `TestSaveConfig_RoundTrips`: save then load gives the same `Config`; the file mode is 0600 and starts with the header.
  - `TestCredentials`: both set → values, nil; token missing → error contains `CREW_JIRA_TOKEN`, not `CREW_JIRA_EMAIL`, and not the email's value.
- [ ] **Step 2: Run** `go test ./internal/config/ ./internal/jira/ ./internal/run/` — Expected: FAIL.
- [ ] **Step 3: Implement.** In `HeadlessRunner.exec` set `cmd.Env = config.WithoutSecrets(os.Environ())`.
- [ ] **Step 4: Run** `go test ./internal/config/ ./internal/jira/ ./internal/run/ ./internal/mission/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: Jira config per repo, credentials from the environment only`

### Task 3: `Board`: crew's tracker operations on one Jira project

**Files:** Create `internal/jira/board.go`, `internal/jira/board_test.go`.

**Interfaces:**
- Consumes: `Client`, `Repo`, `Permanent`, `jiratest.Server`.
- Produces (the method set is `mission.Tracker`, defined in Task 5; `jira` does not import `mission`):
```go
// Board is one repo's Jira project as crew uses it. It looks up the token's account
// and the Flagged field once each and remembers them.
type Board struct{ Client *Client; Repo Repo }

// Create makes one issue, or returns the one already carrying label. kind is "epic" or "piece".
// The issue's type is Repo.EpicType or Repo.IssueType, its labels are ["crew", label], its
// assignee is Repo.Assignee or the token's account, and a piece's parent is the epic's key.
func (b *Board) Create(ctx context.Context, kind, label, summary, body, parent string) (key string, err error)
// Move puts the issue in a lane: "to-do" | "in-progress" | "in-review" | "done" -> Repo.Statuses.
func (b *Board) Move(ctx context.Context, key, lane string) error
func (b *Board) Comment(ctx context.Context, key, text string) error
// Flag sets or clears the issue's flag. ErrNoFlag when the site has no Flagged field.
func (b *Board) Flag(ctx context.Context, key string, on bool) error
// Attach uploads the regular file at path under its base name. A path that is not a regular file is an error.
func (b *Board) Attach(ctx context.Context, key, path string) error
// Done reports whether the issue is in Repo.Statuses.Done (case-insensitive).
func (b *Board) Done(ctx context.Context, key string) (bool, error)
// Sprint puts the issues in the board's active sprint. With Repo.Board == 0 it does nothing
// and reports true. With no active sprint it does nothing and reports false, nil.
func (b *Board) Sprint(ctx context.Context, keys []string) (placed bool, err error)

var ErrNoFlag = errors.New("this Jira site has no Flagged field")
// IsPermanent is Permanent plus ErrNoFlag and an unknown lane or kind.
func IsPermanent(err error) bool

// Unavailable is a tracker that can't be reached: every method returns an error whose text is Reason.
type Unavailable struct{ Reason string }
```

- [ ] **Step 1: Write the failing tests**
  - `TestBoard_CreateEpicAndPiece`: the epic has type `Epic`, labels `crew` and `crew-m1-epic`, no parent, and the token's account as assignee; the piece has type `Task`, the epic as parent and the same assignee.
  - `TestBoard_CreateUsesConfiguredAssignee`: `Repo.Assignee = "acc-9"` → the issue's assignee is `acc-9` and no `GET /rest/api/3/myself` was sent.
  - `TestBoard_CreateFindsItsOwnIssue`: `Create` twice with the same label → the same key both times and the server holds one issue.
  - `TestBoard_MoveMapsLanes`: each of the four lanes → the configured status; lane `"nope"` → an error for which `IsPermanent` is true and no request was sent.
  - `TestBoard_FlagWithoutAField`: after `DropFlagField()` → `errors.Is(err, ErrNoFlag)`, `IsPermanent(err)`, and a second `Flag` call sends no request (the lookup is remembered).
  - `TestBoard_AttachRefusesASymlink`: a symlink to a real file → an error, and the issue has no attachment.
  - `TestBoard_Done`: status `Done` → true; `In Review` → false; `done` lower-case in config → still true.
  - `TestBoard_SprintCases`: board 0 → `true, nil`, no request; board 1 with sprint 7 → `true`, issues in sprint 7; board 1 with none active → `false, nil`.
  - `TestUnavailable`: every method returns an error whose text is the reason, and `IsPermanent` is false for it.
- [ ] **Step 2: Run** `go test ./internal/jira/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test -race ./internal/jira/...` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: Board maps crew's tracker operations to one Jira project`

---

## Phase B: the board follows the mission

### Task 4: State, and the planner that says which Jira writes are still owed

**Files:** Modify `internal/mission/state.go`, `internal/mission/events.go`. Create `internal/mission/tracker_plan.go`, `internal/mission/tracker_text.go`, `internal/mission/tracker_plan_test.go`, `internal/mission/reduce_tracker.go`, `internal/mission/reduce_tracker_test.go`.

**Interfaces (Produces):**
```go
// state.go
const PieceDelivered PieceStatus = "delivered" // the run is done and any pull request is merged; its deliverable waits to be accepted
const WaitDeliverable = "deliverable"

// Deliverable is the work a piece produced that isn't a change to the repo.
type Deliverable struct {
    Files    []string  `json:"files"`              // paths relative to the piece's deliverables folder
    Print    string    `json:"print"`              // fingerprint of the files as collected
    Posted   time.Time `json:"posted"`
    Accepted bool      `json:"accepted,omitempty"`
    By       string    `json:"by,omitempty"`       // who accepted: "engine" or "principal"
    Attached []string  `json:"attached,omitempty"` // the Files crew is done with on the Jira issue
}
// Tracked is what Jira already shows for a piece.
type Tracked struct {
    Issue   string `json:"issue,omitempty"`
    Lane    string `json:"lane,omitempty"`
    Flagged bool   `json:"flagged,omitempty"`
    Sprint  bool   `json:"sprint,omitempty"`
}
// TrackerState is what Jira already shows for the mission.
type TrackerState struct {
    Epic     string `json:"epic,omitempty"`
    EpicLane string `json:"epic_lane,omitempty"`
    Seq      int    `json:"seq,omitempty"` // events up to this one have had their comment sent
}
// new fields
PieceState.Deliverable *Deliverable `json:"deliverable,omitempty"`
PieceState.Jira        Tracked      `json:"jira"`
State.Jira             TrackerState `json:"jira"`

// events.go
const EvDeliverablePosted, EvDeliverableAccepted = "deliverable-posted", "deliverable-accepted"

// tracker_plan.go
const LaneToDo, LaneInProgress, LaneInReview, LaneDone = "to-do", "in-progress", "in-review", "done"
func Lane(p PieceState) string
// TrackerOp is one write Jira still needs. Kind: "create-epic" | "create" | "sprint" |
// "move" | "flag" | "attach" | "move-epic" | "comment".
type TrackerOp struct {
    Kind, Piece string // Piece is "" for the epic
    Lane, File  string
    Text        string
    On          bool
    Seq         int      // comment: the event it comes from
    Pieces      []string // sprint: the pieces to place
}
// TrackerOps returns the writes still owed, in the order to send them. evs are the events
// after s.Jira.Seq. sprints says whether the repo's board uses sprints. It performs no I/O.
func TrackerOps(m Mission, s State, evs []Event, sprints bool) []TrackerOp

// tracker_text.go
func TrackerLabel(missionID, piece string) string             // "crew-<mission>-<piece>"; piece "" gives "crew-<mission>-epic"
func EpicText(missionID string, m Mission) (summary, body string)
func IssueText(missionID string, m Mission, p Piece) (summary, body string)
// TrackerComment is the comment an event puts on an issue. piece "" means the epic.
func TrackerComment(ev Event, s State) (piece, text string, ok bool)
func DeliverablesRel(missionID, piece string) string          // ".crew/missions/<mission>/deliverables/<piece>"

// reduce_tracker.go: each returns a new State
func EpicCreated(s State, key string) State            // also records the epic's lane as to-do: Jira creates an issue there
func IssueCreated(s State, piece, key string) State    // also records the piece's lane as to-do
func LaneSet(s State, piece, lane string) State        // piece "" sets the epic's lane
func FlagSet(s State, piece string, on bool) State
func SprintSet(s State, pieces []string) State
func FileAttached(s State, piece, file string) State
func CommentedThrough(s State, seq int) State           // never moves the cursor back
```
`PieceState.clone` must deep-copy `Deliverable` (its two slices included).

**Lanes:**

| Piece | Lane |
|---|---|
| `queued`; `waiting` with `Wait.Back == queued` | `to-do` |
| `running`; any other `waiting` | `in-progress` |
| `pr-open`, `delivered` | `in-review` |
| `complete`, `cancelled` | `done` |

The epic's lane is `in-progress` while the mission is `running` or `stopped`, `done` when it is `done`, and `to-do` otherwise.

**Order of `TrackerOps`:**
1. `create-epic` when `s.Jira.Epic == ""`. With no epic, nothing else is planned.
2. `create` for each piece without an issue, in state order.
3. One `sprint` op naming every piece that has an issue and `Sprint == false`, when `sprints` is true and there is at least one.
4. For each piece that has an issue, in state order: `move` when `Lane(p) != p.Jira.Lane`; `flag` when `(p.Wait != nil) != p.Jira.Flagged`; one `attach` per entry of `Deliverable.Files` that isn't in `Deliverable.Attached`.
5. `move-epic` when the epic's lane differs from `s.Jira.EpicLane`.
6. One `comment` per event in `evs` for which `TrackerComment` reports ok, in event order.

**Texts.** Summaries are cut to 120 runes, comments to 1000.
- Epic summary: the first non-empty line of the request. Body: `Mission <id>\n\n<the request>`.
- Issue summary: `<piece id>: <first non-empty line of the brief>`. Body: `Mission <id>, piece <piece id>\nDomain: <domain>\nFactory: <factory>\nWaits for: <ids, comma-separated, or "nothing">\n\n<the brief>`.

| Event kind | On | Comment |
|---|---|---|
| `pr-opened` | the piece | `Pull request opened: <the piece's PRURL>` |
| `decision` | the piece | `Decision (<by>): <msg>` |
| `escalated` | the piece | `Escalated to you: <msg>` |
| `waiting-on-principal` | the piece | `Waiting on you: <msg>` |
| `rebrief` | the piece | `Brief changed by <by>.` |
| `piece-cancelled` | the piece | `Cancelled: <msg>` |
| `deliverable-posted` | the piece | `Deliverable posted: <msg>. A copy is in <DeliverablesRel>.` |
| `deliverable-accepted` | the piece | `Deliverable accepted by <by>: <msg>` |
| `amendment` | the epic | `Amendment by the principal: <msg>` |
| anything else | | no comment |

- [ ] **Step 1: Write the failing tests**
  - `TestLane`: one row per line of the lanes table, plus `waiting` with `Back == ""`.
  - `TestTrackerOps_FreshMission`: two queued pieces, no epic → exactly `[create-epic]`. With `Jira.Epic` set → `[create P1, create P2, move-epic in-progress]` for a running mission.
  - `TestTrackerOps_NothingOwed`: every piece has an issue and its lane, flag and sprint match, the epic lane matches, no events → empty.
  - `TestTrackerOps_Order`: `sprints` true; a running mission whose epic lane matches; P1 complete with lane recorded `in-review`; P2 `delivered` with lane `in-review`, a wait, `Flagged == false` and a deliverable with one of two files attached; neither piece in a sprint; two commentable events → `[sprint{P1,P2}, move P1 done, flag P2 on, attach P2 <second file>, comment, comment]`.
  - `TestTrackerOps_NoSprintOpWithoutSprints`.
  - `TestTrackerOps_FlagClears`: `Wait == nil` and `Flagged == true` → `flag` with `On == false`.
  - `TestTrackerOps_EpicDone`: mission `done`, epic lane `in-progress` → `move-epic done`.
  - `TestTrackerComment`: one row per line of the comments table, and `pr-merged`, `note`, `notified`, `piece-start` → not ok.
  - `TestTrackerText`: a request and a brief whose first lines are 200 runes → summaries of 120 runes; the issue body lists `Waits for: P1, P2`.
  - `TestCreated_StartsInToDo`: after `EpicCreated` and `IssueCreated`, a queued piece and a planned mission owe no `move` and no `move-epic`.
  - `TestReducers_NeverChangeTheirInput`: each reducer leaves its argument equal to a `Clone` taken before; `CommentedThrough(s, 3)` on a state at 5 stays at 5.
  - `TestClone_CopiesDeliverable`: appending to the clone's `Files` and `Attached` leaves the original alone.
- [ ] **Step 2: Run** `go test ./internal/mission/` — Expected: FAIL to compile.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go test ./internal/mission/ ./cmd/crew/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: mission state knows what Jira shows, and a planner lists the writes owed`

### Task 5: The engine sends the planned writes

**Files:** Modify `internal/mission/ops.go` (`Tracker`), `internal/mission/engine.go` (fields, `run`, `Loop`). Create `internal/mission/effects_tracker.go`, `internal/mission/effects_tracker_test.go`, `internal/mission/fakes_tracker_test.go`.

**Interfaces:**
- Consumes: Task 4's planner, texts and reducers; `Store.EventsSince`; `Store.Dir`.
- Produces:
```go
// Tracker is where the principal watches a mission. jira.Board satisfies it.
type Tracker interface {
    Create(ctx context.Context, kind, label, summary, body, parent string) (key string, err error)
    Move(ctx context.Context, key, lane string) error
    Comment(ctx context.Context, key, text string) error
    Flag(ctx context.Context, key string, on bool) error
    Attach(ctx context.Context, key, path string) error
    Done(ctx context.Context, key string) (bool, error)
    Sprint(ctx context.Context, keys []string) (placed bool, err error)
}
// new Engine fields
Tracker          Tracker          // nil: the repo has no Jira; nothing is planned or sent
Sprints          bool             // the repo's board uses sprints
TrackerPermanent func(error) bool // says an error can never succeed; nil means none can

// SyncTracker sends the writes Jira is owed, once, and returns how many are still owed.
// The caller holds the mission lock. It never returns a Jira failure as its error.
func (e *Engine) SyncTracker(ctx context.Context) (owed int, err error)

const (
    maxAttachBytes   = 10 << 20
    trackerDrainRuns = 10 // extra sync passes after a mission ends while writes are still owed
)
```

**The sync.** `tick.run` calls it last, after the state is saved, and only when `Tracker != nil`. It plans with `TrackerOps` and carries the ops out in order:

| Op | Call | On success |
|---|---|---|
| `create-epic` | `Create("epic", TrackerLabel(id, ""), EpicText…, "")` | `EpicCreated`, **save at once** |
| `create` | `Create("piece", TrackerLabel(id, piece), IssueText…, epic key)` | `IssueCreated`, **save at once** |
| `sprint` | `Sprint(keys)` | placed: `SprintSet`. Not placed: the note below, and go on |
| `move`, `move-epic` | `Move(key, lane)` | `LaneSet` |
| `flag` | `Flag(key, on)` | `FlagSet` |
| `attach` | `Attach(key, <store>/deliverables/<piece>/<file>)` | `FileAttached` |
| `comment` | `Comment(key, text)` | `CommentedThrough(op.Seq)` |

- After a `create-epic` succeeds the sync plans again once, so the pieces' issues are created in the same pass.
- A file larger than `maxAttachBytes` is not sent: the note `Jira: <file> is over 10 MB and stays in the mission folder`, then `FileAttached`.
- **An error that is not permanent** ends the sync for this tick with one note, `Jira: can't <what> <issue or piece>: <error>`, on the piece ("" for the epic).
- **A permanent error** is a note with the same text, and then: a `create` leaves that piece's other ops out of this pass and the sync goes on; `move`, `move-epic`, `flag`, `attach` and `comment` are recorded as done (their reducer runs), so they are not tried again.
- A `comment` whose piece has no issue yet ends the comments for this pass.
- When every `comment` op of the pass was carried out, the cursor moves to the last event read, so events with no comment are not read again.
- No active sprint: the note `Jira: the board has no active sprint; <n> issues are in the backlog and won't show on the board until a sprint starts`.
- A cancelled `ctx` ends the sync with no note.
- The state is saved once more at the end when anything changed.

Notes go through `tick.note`, so a note repeated from the previous tick is not written again and a failure that keeps repeating is told to the principal once (`trackFailures`).

**When the mission ends.** In `Loop`, when a tick returns a status other than `running` and `SyncTracker` reports writes owed, the loop runs up to `trackerDrainRuns` more sync passes, one per `every`, stopping early when nothing is owed or `ctx` ends. If writes are still owed after that it emits a `note` event: `Jira is behind: <n> writes were not sent; run crew jira sync <mission id>`.

**Test fake:** `fakeTracker` in `fakes_tracker_test.go` records calls in the shared `callLog` as `create:<kind>:<label>`, `move:<key>:<lane>`, `comment:<key>`, `flag:<key>:<on>`, `attach:<key>:<base name>`, `sprint:<n>`, `done:<key>`; hands out keys `J-1`, `J-2`, …; has `failures` for `failNext` and a `permanent map[string]bool`, both keyed by op name (`create-epic`, `create`, `move`, `comment`, `flag`, `attach`, `sprint`, `done`), `noSprint bool`, and `doneKeys map[string]bool`. `newHarness` leaves `Tracker` nil; `h.withTracker()` sets it and `TrackerPermanent`.

- [ ] **Step 1: Write the failing tests**
  - `TestEngine_NoTrackerSendsNothing`: a whole mission with `Tracker == nil` → state has no issue keys and no test fails for a nil call.
  - `TestEngine_FirstTickCreatesEpicAndIssues`: two pieces → calls start `create:epic:crew-<id>-epic`, `create:piece:crew-<id>-P1`, `create:piece:crew-<id>-P2`; the state holds the three keys; P1 (started this tick) was moved to `in-progress`, no `move` was sent for P2, and the epic was moved to `in-progress`.
  - `TestEngine_LanesFollowThePiece`: drive P1 to `pr-open`, then merged → `move:<key>:in-review` then `move:<key>:done`, each sent once across several ticks.
  - `TestEngine_WaitFlagsAndUnflags`: a piece gets a merge wait → `flag:<key>:true` and a `comment`; the wait is cleared → `flag:<key>:false`.
  - `TestEngine_CommentsOncePerEvent`: three ticks after a `pr-opened` event → one `comment` call; the text recorded by the fake is `Pull request opened: <url>`.
  - `TestEngine_JiraDownNeverStopsAPiece`: `failNext("create-epic", 1000)` → the mission still reaches `done`; each tick's events hold at most one `Jira:` note; then failures cleared and one `SyncTracker` → owed is 0 and every piece's lane is `done`.
  - `TestEngine_PermanentMoveIsGivenUp`: `permanent["move"] = true` → one `move` call across three ticks, one note, and the lane recorded.
  - `TestEngine_PermanentCreateSkipsOnlyThatPiece`: P1's create is permanent, P2's works → P2 has an issue and a lane; P1 has none; the sync is tried for P1 again next tick.
  - `TestEngine_SavesEachKeyAtOnce`: `failNext("create", 1000)`, so the epic is created and no piece is → the state on disk after that tick holds the epic key, and three more ticks send no second `create:epic`.
  - `TestEngine_NoActiveSprintIsANote`: `Sprints = true`, `noSprint = true` → one note with `no active sprint`, the other ops still run, and the `sprint` op is planned again next tick.
  - `TestEngine_BigFileIsNotAttached`: an 11 MiB sparse file in the piece's deliverables folder → no `attach` call for it, one note, and it is not planned again.
  - `TestLoop_DrainsThenSaysWhatIsLeft`: the mission ends with Jira failing → the loop makes `trackerDrainRuns` more passes, then one `note` event containing `crew jira sync`; with Jira recovering on the third pass the loop returns after it and emits no such note.
- [ ] **Step 2: Run** `go test ./internal/mission/` — Expected: FAIL.
- [ ] **Step 3: Implement.** Keep the sync in `effects_tracker.go`; `engine.go` gains only the fields, the call in `run` and the drain in `Loop`.
- [ ] **Step 4: Run** `go test -race ./internal/mission/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: the engine keeps the Jira board in step with the mission`

---

## Phase C: deliverables

### Task 6: A station leaves a deliverable; crew collects it and hands it on

**Files:** Create `internal/mission/deliverable.go`, `internal/mission/deliverable_test.go`, `internal/mission/effects_deliverable.go`, `internal/mission/effects_deliverable_test.go`. Modify `internal/mission/machine.go` (`Obs`, `done`, the action), `internal/mission/engine.go` (`observeRun`), `internal/mission/effects.go` (`perform`, `startPiece`), `internal/mission/ops.go` (`PieceInput`), `internal/mission/store.go`, `internal/mission/reduce.go`, `internal/gitx/gitx_test.go`.

**Interfaces:**
- Consumes: `Deliverable`, `EvDeliverablePosted`, `run.EnsureCrewDir`.
- Produces:
```go
// deliverable.go
const (
    scratchOut          = "deliverables" // <worktree>/.crew/deliverables: what the station leaves
    scratchIn           = "inputs"       // <worktree>/.crew/inputs/<piece>: earlier pieces' deliverables
    maxDeliverableFiles = 20
)
// Found is what a finished run left in its deliverables folder.
type Found struct{ Files []string; Print string; Extra int } // Extra: files beyond maxDeliverableFiles, not taken
// listDelivered lists the regular files under dir, sorted, as slash-separated paths relative
// to dir. It uses Lstat: symlinks and anything else that isn't a regular file or a directory
// are skipped, and a symlinked directory is not entered. A missing dir is an empty Found.
// Print is the hex SHA-256 of each taken file's path, size and modification time.
func listDelivered(dir string) (Found, error)
// copyFiles copies the named regular files from one folder to another, creating folders
// with mode 0700 and files with mode 0600. It refuses a name that is absolute or holds "..".
func copyFiles(from, to string, files []string) error
// Earlier is a deliverable an earlier piece produced, as this piece's input.
type Earlier struct{ Piece string; Files []string }

func (s Store) DeliverablesDir(piece string) string // <mission folder>/deliverables/<piece>

// machine.go
type CollectDeliverable struct{ Piece string }  // an Action
Obs.Delivered map[string]Found                   // present for pieces whose run is done

// reduce.go
func DeliverableCollected(s State, piece string, files []string, print string, now time.Time) State // a fresh, unaccepted Deliverable

// ops.go: PieceInput gains the earlier deliverables
func PieceInput(id string, m Mission, p Piece, d source.Domain, earlier []Earlier) string
```

**`PieceInput` adds two sections, after the domain briefing and before `## Commit`:**
```
## Deliverables from earlier pieces

- P5: .crew/inputs/P5/brief.md, .crew/inputs/P5/plan.md

## Deliverables

If your piece produces work that isn't a change to the repo (research, a brief, a plan), write it as files in .crew/deliverables/ in this directory. Git ignores that folder: don't commit those files and don't move them. crew collects them when you finish.
```
The first section appears only when `earlier` is not empty.

**`startPiece`**, after the worktree is added and before the run starts: `run.EnsureCrewDir(worktree)`; create `<worktree>/.crew/deliverables`; for each piece in `p.WaitsFor` whose state has a `Deliverable`, copy its `Files` from `Store.DeliverablesDir(dep)` to `<worktree>/.crew/inputs/<dep>/`. A failure is the note `can't start: prepare the deliverables folders: <error>` and the worktree is removed, as for a failed run start.

**`observeRun`**, for a run that is done: `Obs.Delivered[id] = listDelivered(<worktree>/.crew/deliverables)`. A failure is the note `can't read the deliverables folder: <error>` and leaves the piece out of `Obs.Git` too, so nothing is decided on half a picture.

**`Decide`, in `done`,** in this order (the first two cases are today's):
1. No git observation: nothing.
2. Uncommitted files: as today.
3. `Delivered` has files and the piece has no `Deliverable`, or its `Print` differs: `CollectDeliverable`, and nothing else for this piece this tick.
4. `Delivered` has no files and the piece has an unaccepted `Deliverable`: the deliverable is dropped (`Deliverable = nil`), then go on.
5. Commits ahead: `OpenPR`, as today.
6. Otherwise: complete and clean up, as today (Task 7 changes this case).

**The `CollectDeliverable` effect:** list the folder again; empty the piece's `Store.DeliverablesDir` and copy the files into it; `DeliverableCollected`; save; emit `deliverable-posted` with the file names as `Msg` (through the existing `fileList`). When `Found.Extra > 0`, the note `<n> more files in .crew/deliverables were left out: a deliverable is at most 20 files`. A failure is the note `can't collect the deliverable: <error>` and changes nothing.

- [ ] **Step 1: Write the failing tests**
  - `TestListDelivered_SortedRelativePaths`: `b.md`, `a/c.md` → `["a/c.md", "b.md"]`.
  - `TestListDelivered_SkipsSymlinks`: a symlink to a file outside the folder, and a symlinked directory holding a file → neither is listed.
  - `TestListDelivered_MissingFolder`: no error, no files.
  - `TestListDelivered_CapsAtTwenty`: 23 files → 20 files, `Extra == 3`.
  - `TestListDelivered_PrintChangesWithContent`: rewriting a file with a different size changes `Print`; listing twice without a change doesn't.
  - `TestCopyFiles_RefusesEscapes`: names `../x` and `/etc/passwd` → an error and nothing written.
  - `TestCopyFiles_Modes`: copied files are 0600.
  - `TestPieceInput_Deliverables`: with one `Earlier` → both sections, in that order, before `## Commit`; with none → only `## Deliverables`.
  - `TestDecide_CollectsBeforeOpeningAPR`: run done, clean, 2 commits ahead, one delivered file, no `Deliverable` → exactly `[CollectDeliverable]`; with a `Deliverable` whose `Print` matches → `[OpenPR]`.
  - `TestDecide_ChangedDeliverableIsCollectedAgain`: `Print` differs → `[CollectDeliverable]`.
  - `TestDecide_DeliverableTakenBackIsDropped`: no files found, an unaccepted `Deliverable` → the new state has none.
  - `TestEngine_CollectCopiesAndRecords`: real files under the harness's worktree path → after one tick the files are in `Store.DeliverablesDir`, the state lists them, and a `deliverable-posted` event names them.
  - `TestEngine_StartCopiesEarlierDeliverables`: P2 waits for P1, P1 complete with a collected deliverable → P2's worktree has `.crew/inputs/P1/<file>`, `.crew/.gitignore` and an empty `.crew/deliverables`, and the input passed to `Runs.Start` names `.crew/inputs/P1/<file>`.
  - In `internal/gitx/gitx_test.go`, `TestWorktree_ScratchIsIgnored`: in a real temp repo, a worktree holding `.crew/.gitignore` (`*`) and `.crew/deliverables/x.md` → `Dirty` is empty and `RemoveWorktree(path, false)` succeeds.
- [ ] **Step 2: Run** `go test ./internal/mission/ ./internal/gitx/` — Expected: FAIL.
- [ ] **Step 3: Implement.** If `TestWorktree_ScratchIsIgnored` shows git refusing the removal, stop and report it; don't add `--force`.
- [ ] **Step 4: Run** `go test -race ./internal/mission/ ./internal/gitx/ ./cmd/crew/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: stations hand over deliverables and later pieces receive them`

### Task 7: Accepting a deliverable

**Files:** Create `internal/mission/machine_deliverable.go`, `internal/mission/machine_deliverable_test.go`. Modify `internal/mission/machine.go` (`Decide`, `done`, `Obs`), `internal/mission/machine_pr.go` (`ended`), `internal/mission/reduce.go` (`Merged`), `internal/mission/effects_pr.go` (`merge`), `internal/mission/answer.go`, `internal/mission/engine.go` (`observe`), and their tests.

**Interfaces:**
- Consumes: `PieceDelivered`, `WaitDeliverable`, `Deliverable`, `Tracker.Done`, `modeFor`, `tick.readSwitches`.
- Produces:
```go
// AcceptObs is what the engine saw about a posted, unaccepted deliverable.
type AcceptObs struct {
    Mode      principal.AutoMode // the switch of the piece's own domain; off when it can't be read
    MovedDone bool               // the piece's Jira issue is in the Done status
}
Obs.Accept map[string]AcceptObs // one entry per piece with an unaccepted Deliverable

const answerAccept = "accept"
```

**A piece's work is finished** where today it completes: in `done` case 6, in `ended` when its pull request is merged, and in the `Merged` reducer. From now on, at each of those three places:
- no `Deliverable`, or it is accepted → complete, and clean up as today;
- otherwise → the status becomes `delivered`, any wait is cleared, and the worktree stays.

`merge` removes the worktree only when `Merged` left the piece complete.

**`Decide` gets one more pass,** after the running and pr-open pieces and before the queued ones, over every piece with an unaccepted `Deliverable`, whatever its status:
1. `Accept[id].Mode` is `on` or `without-checks` → accepted by `engine`; event `deliverable-accepted` with `By: "engine"` and `Msg: "auto-merge is <mode> for <domain>"`.
2. Else `Accept[id].MovedDone` → accepted by `principal`; event with `By: "principal"` and `Msg: "its Jira issue was moved to Done"`.
3. Then, only for a piece whose status is `delivered`: accepted → complete, and `Cleanup{Force: true}`; not accepted and no wait → `wait(id, WaitDeliverable, question, "")`.

A piece absent from `Obs.Accept` is not accepted by rule 1 or 2. The wait is opened with no back status, so a `delivered` piece does not count toward `runs_at_once`.

**The wait's question:**
- the piece has a Jira issue: `deliverable to accept (<files>): move <issue> to Done on the board, or answer accept`
- it has none: `deliverable to accept (<files>) in <DeliverablesRel>: answer accept`

`<files>` is the first three names, comma-separated, then ` and <n> more` when there are more.

**Answers to a `deliverable` wait** (`answerChoices` now takes the piece too):

| Piece | Choices |
|---|---|
| no pull request (`PR == 0`) | `accept`, `goto`, `cancel` |
| has a pull request | `accept`, `cancel` |

- `accept`: the deliverable is accepted by `principal` (event `deliverable-accepted`, `Msg: "answered"`), the piece completes, `Cleanup{Force: true}`. No run step.
- `goto`: `Deliverable = nil`, status `running`, and the usual `goto` run step with the note. The piece runs again even while another piece runs, like a piece sent back from its pull request.
- `cancel`: as for any piece.

**`observe`** fills `Obs.Accept` for every piece with an unaccepted `Deliverable`: `Mode` from the switch snapshot read once per tick (an unreadable switch is `off`, with the existing switch note); and, only when the mode is `off`, the piece has an issue and `Tracker != nil`, `MovedDone` from `Tracker.Done`. A failure there is the note `Jira: can't read <issue>: <error>` and leaves `MovedDone` false.

- [ ] **Step 1: Write the failing tests**
  - `TestDecide_DeliverableOnlyPieceWaitsWhenSwitchOff`: run done, clean, 0 ahead, a posted deliverable, `Accept` mode off → status `delivered`, a `deliverable` wait whose question names the files and `answer accept`, no `Cleanup`, a `waiting-on-principal` record and a notification.
  - `TestDecide_SwitchOnAcceptsAtOnce`: same, mode `on` → in one `Decide`: `deliverable-accepted` by `engine`, status `complete`, `Cleanup{Force: true}`, no wait. Same for `without-checks`.
  - `TestDecide_DoneOnTheBoardAcceptsOnlyAPostedDeliverable`: (a) `delivered` piece, `MovedDone` → accepted by `principal`, complete; (b) a pr-open piece with no `Deliverable` and an `Accept` entry saying `MovedDone` → nothing changes; (c) a running piece whose run isn't done, `MovedDone` → nothing changes.
  - `TestDecide_ChangeAndDeliverable`: a pr-open piece with an unaccepted deliverable, PR observed `MERGED` → status `delivered` and a `deliverable` wait, not `complete`; with the deliverable already accepted (moved to Done while the PR was open) → `complete`.
  - `TestDecide_DeliveredDoesNotHoldARun`: P1 `delivered` with its wait, P2 queued and independent → `StartPiece{P2}`.
  - `TestDecide_DependentWaitsForAcceptance`: P2 waits for P1, P1 `delivered` → no `StartPiece`.
  - `TestMerged_GoesToDeliveredWhenUnaccepted`; `TestEngine_MergeKeepsWorktreeOfDeliveredPiece`.
  - `TestAnswer_AcceptCompletes`: → `complete`, event by `principal`, `Cleanup{Force: true}`, no run step.
  - `TestAnswer_GotoReopensTheWork`: `PR == 0` → status `running`, `Deliverable == nil`, a `goto` step carrying the note.
  - `TestAnswer_GotoRefusedAfterAMerge`: `PR != 0`, `--do goto` → the error lists `accept, cancel`.
  - `TestAnswer_AcceptRefusedOnOtherWaits`: a `merge` wait answered `accept` → an error.
  - `TestEngine_ObservesAcceptance`: switch off and `doneKeys[<key>] = true` → the piece completes within one tick; switch `on` → no `done:<key>` call at all; `Tracker == nil` → no panic and the wait stays.
  - `TestEngine_TamperedSwitchNeverAccepts`: the switch read fails → the deliverable waits and the switch note is written.
- [ ] **Step 2: Run** `go test ./internal/mission/` — Expected: FAIL.
- [ ] **Step 3: Implement.** Put the pass and the "work is finished" helper in `machine_deliverable.go`.
- [ ] **Step 4: Run** `go test -race ./internal/mission/ ./cmd/crew/` — Expected: PASS, the stage 1 and stage 2 end-to-end tests included.
- [ ] **Step 5: Commit** `feat: a deliverable is accepted by the switch, on the board, or with an answer`

---

## Phase D: commands, status, the guide

### Task 8: `crew jira setup|check|sync`, and wiring the engine

**Files:** Create `cmd/crew/jira.go`, `cmd/crew/jira_report.go`, `cmd/crew/jira_test.go`. Modify `cmd/crew/main.go` (the command table and usage), `cmd/crew/missionrun.go` (`newMissionEngine`), `cmd/crew/mission.go` (`startChecks`, `missionFlagSet`, usage), a new `cmd/crew/mission_jira.go` (the Jira line), `internal/scaffold/template/harnesses.yaml` (one deny rule), `internal/scaffold/scaffold_test.go` or the nearest existing template test.

**Interfaces:**
- Consumes: `jira.LoadConfig`, `SaveConfig`, `Credentials`, `New`, `Board`, `Unavailable`, `IsPermanent`; `principal.Confirm`, `principal.OpenTTY`; `Engine.SyncTracker`; `repoInfo`.
- Produces:
```go
// seam: tests point it at jiratest's server
var newJiraClient = func(r jira.Repo, email, token string) *jira.Client { return jira.New(r.Site, email, token) }
var _ mission.Tracker = (*jira.Board)(nil)
var _ mission.Tracker = jira.Unavailable{}
// trackerFor returns the tracker for a repo: nil when jira.yaml has no entry for it,
// jira.Unavailable when credentials are missing, else a *jira.Board.
func trackerFor(crewDir, slug string, getenv func(string) string) (t mission.Tracker, sprints bool, err error)
```

**Usage text:**
```
usage:
  crew jira setup --site URL --project KEY [--board N] [--assignee ACCOUNT_ID]   show this repo's missions on a Jira board (asks you to confirm in the terminal)
  crew jira check                                                               test this repo's Jira settings against the site
  crew jira sync <mission>                                                      send the Jira writes a finished or stopped mission still owes
common flags: --dir D (crew directory), --project P (default: current directory)
```

**`crew jira setup`**
1. Needs both credentials; without them: `set CREW_JIRA_EMAIL and CREW_JIRA_TOKEN first (an API token from id.atlassian.com); crew never stores them`.
2. Validates the flags through `jira.Repo.Validate`. With no `--board`, asks `Boards`: exactly one → that board; none or several → board 0 and a line saying which (`no board found for ACME: issues are not put in a sprint` / `ACME has 3 boards (1 "Team", 4 "Ops", …): pass --board N to use sprints`).
3. Runs the report below. Any `problem:` line stops the command with nothing written.
4. Opens the terminal with `principal.OpenTTY` and asks `principal.Confirm` for the phrase `<owner/name> <PROJECT>`. With no terminal: the same refusal `crew automerge` gives. There is no flag, environment variable or stdin path around it.
5. Saves the entry with `SaveConfig` and prints `Jira is set up for <owner/name>. New missions in this repo will appear in <PROJECT>.`

**The report** (`crew jira check` prints the same, for the entry already in `jira.yaml`; with no entry: `Jira is not set up for <owner/name>; run crew jira setup`):
```
Jira for acme/app: project ACME on https://example.atlassian.net
  account: <display name>
  issue types: Task, Epic
  statuses: To Do, In Progress, In Review, Done
  board: 1, active sprint "Sprint 4"
  flag: the Flagged field is there
```
Variants: `board: 1, no active sprint: new issues go to the backlog and won't show on the board until you start a sprint`; `board: none: issues are not put in a sprint`; `flag: no Flagged field: a waiting piece gets a comment but no flag`.
Problems, each a line starting `  problem: `: the site rejects the credentials; the project isn't found; the issue type or the epic type doesn't exist in the project; a status name is missing for the issue type (`status "In Review" is not in ACME's workflow for Task; add it in Jira or set statuses.in_review in jira.yaml`). The epic type needs only `to_do`, `in_progress` and `done`.

**`crew jira sync <mission>`:** refuses while an engine holds the mission lock (`mission <id> has a live engine, which sends Jira writes itself`); otherwise builds the engine with `newMissionEngine`, takes the lock, calls `SyncTracker` once and prints `Jira is up to date for mission <id>` or `<n> writes still not sent; see the notes in crew mission status <id>`.

**Wiring.** `newMissionEngine` sets `Tracker`, `Sprints` (the repo's `board != 0`) and `TrackerPermanent: jira.IsPermanent` from `trackerFor`. Missing credentials there become `jira.Unavailable{Reason: "CREW_JIRA_EMAIL and CREW_JIRA_TOKEN are not set where this engine was started"}`, never an error that stops the engine.

**`startChecks`** (so `start`, `go` and `resume`): when `jira.yaml` has an entry for the repo and credentials are missing, refuse with `Jira is set up for <owner/name>, but <the Credentials error> here; set it, or remove the repo from jira.yaml`. It makes no network call.

**The plan and start output** gain one line after the auto-merge line: `Jira: project ACME on https://example.atlassian.net` or `Jira: not set up for acme/app; deliverables stay in the mission folder (crew jira setup)`.

**The deny list.** Add `"Bash(crew jira setup:*)"` to `deny.all` in the harness template, next to `crew automerge`. `UpgradeHarnesses` is not changed: a crew directory that already has a `deny` block keeps it as it is, and there the terminal confirmation is what stops a session. The `--do` flag's help text becomes `answer: retry, goto, cancel, approve or accept`, and `missionUsage` lists `accept`.

- [ ] **Step 1: Write the failing tests** (credentials through an injected `getenv`; the site through `newJiraClient` and `jiratest`; the terminal through the seam `crew automerge`'s tests use):
  - `TestJiraSetup_WritesTheEntry`: confirmation typed correctly → `jira.yaml` holds the repo with board 0 and the defaults; the output ends with the set-up line.
  - `TestJiraSetup_PicksTheOnlyBoard`; `TestJiraSetup_SeveralBoardsNeedAFlag`.
  - `TestJiraSetup_NoTerminalWritesNothing`; `TestJiraSetup_WrongPhraseWritesNothing`.
  - `TestJiraSetup_RefusesOtherHosts`: `--site https://example.com` → the `Validate` error, no request sent.
  - `TestJiraSetup_ProblemStops`: the test server lacks `In Review` → the `problem:` line, nothing written, and the terminal was never opened.
  - `TestJiraSetup_NeedsCredentials`: the message above, and its text holds no value of either variable.
  - `TestJiraCheck_Report`: the six lines above for a server with board 1 and sprint 4; then the no-sprint and no-flag variants.
  - `TestJiraCheck_NotSetUp`.
  - `TestTrackerFor`: no entry → nil; entry and credentials → a `*jira.Board`, `sprints` true only with a board; entry without credentials → `jira.Unavailable`.
  - `TestStartChecks_JiraWithoutCredentials`: the refusal names the missing variable; with no entry for the repo → no refusal.
  - `TestMissionPlan_PrintsJiraLine`: both forms.
  - `TestJiraSync_SendsWhatIsOwed`: a mission marked `done` with nothing in Jira → after the command the server has the epic and each piece's issue in `Done`; with a live lock → the refusal.
  - `TestTemplate_DeniesJiraSetup`: the template's `deny.all` for `claude-code` contains `Bash(crew jira setup:*)`, and the stage 2 upgrade tests still pass.
- [ ] **Step 2: Run** `go test ./cmd/crew/ ./internal/scaffold/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** `go build ./... && go vet ./... && go test -count=1 ./...` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: crew jira setup, check and sync; missions use the repo's Jira project`

### Task 9: Status, the orchestrator's instructions, the guide, end to end

**Files:** Modify `cmd/crew/missionstatus.go` (or a new `cmd/crew/missionstatus_jira.go`), `cmd/crew/missionstatus_pr_test.go` or a new test file, `cmd/crew/mission_e2e_test.go` or a new `cmd/crew/mission_e2e_jira_test.go`, `internal/build/orchestrator.go` (`OrchestratorBody`), `internal/build/orchestrator_test.go`, `internal/scaffold/template/missions.yaml` (no new keys; only if a comment needs it), `docs/missions.md`, `README.md`.

**`crew mission status <id>`**
- The first line is unchanged. When the mission has an epic, a second line: `Jira: <epic key>  <site>/browse/<epic key>`, and when writes are owed (`len(mission.TrackerOps(…)) > 0`, computed from the state and the events after the cursor), ` (<n> writes not sent yet)` is appended.
- **Waiting on you:** a `deliverable` wait's line is `<wait>  <piece>  deliverable  <question>  waiting <time>`.
- A new section after "Open pull requests":
```
Deliverables:
  P5  brief.md, plan.md  waiting on you  .crew/missions/<id>/deliverables/P5
  P2  research.md  accepted by engine  .crew/missions/<id>/deliverables/P2
```
- `delivered` pieces are listed nowhere else: not under Running, Queued or Complete.
- A piece's Jira key, when it has one, follows its id in the Open pull requests, Deliverables and Complete lines as ` (ACME-12)`.

**`OrchestratorBody`** gains, in its own words and in the existing plain style:
- a piece may end in a deliverable instead of, or as well as, a pull request: say so in the brief of a piece whose outcome is research, a brief or a plan, and name the piece that needs it in `waits_for` so it receives the files;
- the principal accepts a deliverable by moving its Jira issue to Done or by answering the wait with `--do accept`; with auto-merge on for the domain it is accepted at once;
- the orchestrator never writes to Jira and never runs `crew jira setup`.

**`docs/missions.md`**
- Rewrite the sentence in "What this stage does and doesn't do" that says crew does not write to Jira.
- New section **"Jira"** after "Auto-merge": what appears on the board (the epic, an issue per piece, assignment, the four lanes, the flag, the comments table in words); setting it up (`crew jira setup`, the two environment variables, where an API token comes from, `crew jira check`); the `jira.yaml` example; sprints, including what happens with no active sprint; what crew reads back (one thing); what happens while Jira is down, and `crew jira sync`; that only `*.atlassian.net` sites are supported; that a shell which exports the token makes it readable by any session started from that shell, and that crew removes it from the stations it starts.
- New section **"Deliverables"**: where a station writes them, where crew keeps them, the 20-file and 10 MB limits, how later pieces get them, the three ways one is accepted, sending one back with `--do goto`, and that a deliverable that changes after a rework must be accepted again.
- "The commands" and "What 'waiting on you' means" gain the new command, the `accept` answer and the `deliverable` wait.
- `README.md`: the command list gains the three `crew jira` commands.

**End to end** (the fake harness, the fake `gh`, and `jiratest` through `newJiraClient`):
- `TestMission_EndToEnd_JiraBoard`: two code pieces, auto-merge off, the principal "merges" each PR through the fake `gh`. Assert on the test server: one epic and two issues, each assigned and labelled; P1's issue `History` is `To Do, In Progress, In Review, Done`; each issue has a comment `Pull request opened: …`; each issue's `Flags` is at least 1 and it ends unflagged; the epic ends in `Done`.
- `TestMission_EndToEnd_Deliverable`: P1 is a deliverable-only piece (the fake station writes `.crew/deliverables/brief.md` and commits nothing), P2 waits for P1. Switch off: P1's issue has the attachment `brief.md` and sits in `In Review`; `crew mission status` shows the `deliverable` wait; the test calls `SetStatus(<P1's key>, "Done")`; P1 completes; P2's station finds `.crew/inputs/P1/brief.md` readable in its working directory. Then the same mission with the switch `on` for the domain: no wait is ever opened for P1.
- `TestMission_EndToEnd_JiraDown`: the server fails every request for the whole mission → the mission reaches `done`, `crew mission status` shows the not-sent count, and `crew jira sync <id>` after the server recovers leaves every issue in `Done`.

- [ ] **Step 1: Write the failing tests:** the three end-to-end tests; `TestMissionStatus_Deliverables` (both lines of the section, the Jira line with and without the not-sent count, a `delivered` piece absent from Running, Queued and Complete); `TestOrchestratorBody_MentionsDeliverables` (contains `--do accept`, `deliverable` and `crew jira setup`).
- [ ] **Step 2: Run** `go test ./cmd/crew/ ./internal/build/` — Expected: FAIL.
- [ ] **Step 3: Implement,** then write the guide and README changes.
- [ ] **Step 4: Run** `go build ./... && go vet ./... && go test -count=1 ./... && go test -race -count=1 ./internal/mission/ ./internal/jira/... ./internal/run/` — Expected: PASS. `TestDocs_ExamplePlansParse` still passes.
- [ ] **Step 5: Commit** `feat: status shows Jira and deliverables; the guide covers both`
