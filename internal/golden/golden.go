// Package golden measures what the circuit PRODUCES, over cases whose answer
// is known in advance (DR-005).
//
// The engine already measures what the work costs (state.Ledger) and whether
// the project's own commands pass (package verify). Neither answers whether
// the circuit is any good: a phase that costs little and leaves the build
// green can still have produced a spec that misses the defect it existed to
// catch. A golden case is that defect, planted on purpose, and the check is
// whether the circuit named it.
//
// Every check is interpreted here, deterministically, and never by a model.
// ArcanFlows shipped both designs and kept this one: their first evaluation
// schema carried a natural-language "judge" criterion for an LLM, and the
// second replaced it with declarative checks because skill and model changes
// have to become measured decisions rather than impressions. A model judging
// a model puts the variance being measured inside the instrument: one weight
// change moves the judge and the judged together, and the measurement goes
// blind exactly when it is needed.
package golden

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// CasesDir is where a project keeps its cases.
//
// They live OUTSIDE .ailoop on purpose. That directory is the loop's runtime
// state and every project gitignores it — checked in both this repo and
// curza-sync, where the ignore rule says so in as many words. A case is not
// state: it decides a verdict, so it has to be reviewable in a diff, which a
// file nobody can commit never is. Runs go the other way: those ARE state.
func CasesDir(workspace string) string { return filepath.Join(workspace, "golden") }

// RunsPath is the append-only history of runs, which is runtime state and
// belongs with the rest of it.
func RunsPath(workspace string) string {
	return filepath.Join(workspace, ".ailoop", "golden-runs.jsonl")
}

// A Check is one deterministic assertion over what an agent produced.
// Exactly one field is set; a Check with none is a load error, never a pass.
type Check struct {
	Contains    string `json:"contains,omitempty"`
	NotContains string `json:"not_contains,omitempty"`
	Regex       string `json:"regex,omitempty"`
}

// Describe renders the check for a report.
func (c Check) Describe() string {
	switch {
	case c.Contains != "":
		return fmt.Sprintf("contains %q", c.Contains)
	case c.NotContains != "":
		return fmt.Sprintf("does not contain %q", c.NotContains)
	case c.Regex != "":
		return fmt.Sprintf("matches /%s/", c.Regex)
	}
	return "empty check"
}

// Validate refuses a check that asserts nothing.
//
// It has to be an error rather than a silent pass. A case whose checks all
// assert nothing reports green while measuring nothing, which is worse than
// having no harness: it answers the question wrongly instead of leaving it
// open.
func (c Check) Validate() error {
	n := 0
	for _, s := range []string{c.Contains, c.NotContains, c.Regex} {
		if s != "" {
			n++
		}
	}
	switch {
	case n == 0:
		return fmt.Errorf("check asserts nothing: set one of contains, not_contains, regex")
	case n > 1:
		return fmt.Errorf("check sets %d assertions; exactly one is allowed", n)
	}
	if c.Regex != "" {
		if _, err := regexp.Compile(c.Regex); err != nil {
			return fmt.Errorf("regex %q does not compile: %w", c.Regex, err)
		}
	}
	return nil
}

// A Case is one known task with a known-good shape of answer.
type Case struct {
	Slug   string  `json:"slug"`
	Title  string  `json:"title"`
	Phase  string  `json:"phase"`
	Task   string  `json:"task"`
	Checks []Check `json:"checks"`
	// Disabled parks a case without deleting it, so the reason it was parked
	// stays in the diff.
	Disabled bool `json:"disabled,omitempty"`
}

// Validate refuses a case that cannot produce a verdict.
func (c Case) Validate() error {
	if strings.TrimSpace(c.Slug) == "" {
		return fmt.Errorf("case has no slug")
	}
	if strings.TrimSpace(c.Task) == "" {
		return fmt.Errorf("case %q has no task", c.Slug)
	}
	if len(c.Checks) == 0 {
		return fmt.Errorf("case %q has no checks: it would report green having measured nothing", c.Slug)
	}
	for i, ch := range c.Checks {
		if err := ch.Validate(); err != nil {
			return fmt.Errorf("case %q check %d: %w", c.Slug, i, err)
		}
	}
	return nil
}

// LoadCases reads every case in the workspace, in slug order so a run is
// reproducible. A directory that does not exist returns nothing and no error;
// an invalid case is an error, because a case silently skipped is a case that
// stops catching what it was written to catch.
func LoadCases(workspace string) ([]Case, error) {
	paths, err := filepath.Glob(filepath.Join(CasesDir(workspace), "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	var cases []Case
	seen := map[string]string{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		var c Case
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		if other, dup := seen[c.Slug]; dup {
			return nil, fmt.Errorf("%s: slug %q already used by %s", filepath.Base(p), c.Slug, other)
		}
		seen[c.Slug] = filepath.Base(p)
		cases = append(cases, c)
	}
	return cases, nil
}

// CheckResult is one assertion's outcome.
type CheckResult struct {
	Describe string `json:"check"`
	Passed   bool   `json:"passed"`
}

// CaseResult is what one case produced.
//
// Slug, Title and Output are copied rather than referenced: a history that
// becomes unreadable once someone edits a case cannot be used to spot a
// regression, which is the only reason it is kept.
type CaseResult struct {
	Slug      string        `json:"slug"`
	Title     string        `json:"title,omitempty"`
	Output    string        `json:"output,omitempty"`
	Passed    bool          `json:"passed"`
	Checks    []CheckResult `json:"checks,omitempty"`
	LatencyMS int64         `json:"latency_ms"`
	Err       string        `json:"error,omitempty"`
}

// Run is one execution of the suite.
//
// Provider and Model are recorded because a regression has two causes that
// look identical in the output — the prompt changed, or the weights did — and
// phase routing guarantees they change independently.
type Run struct {
	Time     time.Time    `json:"time"`
	Provider string       `json:"provider,omitempty"`
	Model    string       `json:"model,omitempty"`
	Trigger  string       `json:"trigger,omitempty"`
	Note     string       `json:"note,omitempty"`
	Total    int          `json:"total"`
	Passed   int          `json:"passed"`
	Results  []CaseResult `json:"results,omitempty"`
}

// Evaluate applies the checks to one output. It is pure: same text, same
// checks, same verdict, no network and no clock.
func Evaluate(output string, checks []Check) (bool, []CheckResult) {
	results := make([]CheckResult, 0, len(checks))
	all := true
	for _, c := range checks {
		ok := false
		switch {
		case c.Contains != "":
			ok = strings.Contains(output, c.Contains)
		case c.NotContains != "":
			ok = !strings.Contains(output, c.NotContains)
		case c.Regex != "":
			// Validate compiled it already; a case that got here without
			// validation fails its check rather than panicking the run.
			re, err := regexp.Compile(c.Regex)
			ok = err == nil && re.MatchString(output)
		}
		if !ok {
			all = false
		}
		results = append(results, CheckResult{Describe: c.Describe(), Passed: ok})
	}
	return all, results
}

// Producer is what turns a case into the text the circuit produced. It is an
// argument rather than a dependency so the harness can be tested end to end
// without a model: the seam sits at the network, and nowhere closer.
type Producer func(ctx context.Context, c Case) (string, error)

// Execute runs every enabled case and returns the record.
func Execute(ctx context.Context, cases []Case, produce Producer) Run {
	run := Run{Time: time.Now()}
	for _, c := range cases {
		if c.Disabled {
			continue
		}
		started := time.Now()
		out, err := produce(ctx, c)
		res := CaseResult{
			Slug:      c.Slug,
			Title:     c.Title,
			Output:    out,
			LatencyMS: time.Since(started).Milliseconds(),
		}
		if err != nil {
			// A case that could not run is not a case that failed its checks,
			// and it is certainly not one that passed. It counts against the
			// total and reports why.
			res.Err = err.Error()
		} else {
			res.Passed, res.Checks = Evaluate(out, c.Checks)
		}
		run.Total++
		if res.Passed {
			run.Passed++
		}
		run.Results = append(run.Results, res)
	}
	return run
}

// AllPassed reports whether the suite is green.
//
// A run with no cases is NOT green. "Nothing ran" must never read as
// "everything is fine" — the same rule the project's own verification follows,
// and the one that makes an empty suite loud instead of reassuring.
func (r Run) AllPassed() bool {
	return r.Total > 0 && r.Passed == r.Total
}

// AppendRun records the run. Like the turn journal it is best-effort: a lost
// line costs a gap in the history, a returned error would cost the run.
func AppendRun(workspace string, r Run) {
	if workspace == "" {
		return
	}
	if r.Time.IsZero() {
		r.Time = time.Now()
	}
	path := RunsPath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	line, err := json.Marshal(r)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// ReadRuns returns past runs, newest last. A malformed line is skipped: the
// file is appended to by a process that can be killed mid-write.
func ReadRuns(workspace string) ([]Run, error) {
	data, err := os.ReadFile(RunsPath(workspace))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var runs []Run
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r Run
		if json.Unmarshal([]byte(line), &r) != nil {
			continue
		}
		runs = append(runs, r)
	}
	return runs, nil
}
