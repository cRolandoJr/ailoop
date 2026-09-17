package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cRolandoJr/ailoop/internal/config"
	"github.com/cRolandoJr/ailoop/internal/verify"
)

// ErrNoChecks means the project never declared what "verified" means for it.
// It is an error and not an empty pass: nothing having run is not everything
// having passed.
var ErrNoChecks = errors.New("no verification commands declared")

// VerifyReport is the outcome of running the project's own checks.
type VerifyReport struct {
	Results []verify.Result
	Passed  bool
}

// Verify runs the checks the project declared for itself.
func (l *Loop) Verify(ctx context.Context) (*VerifyReport, error) {
	cfg := l.cfg
	if cfg == nil {
		loaded, err := config.Load(l.workspace)
		if err != nil {
			return nil, err
		}
		cfg = loaded
	}
	if len(cfg.Verify) == 0 {
		return nil, fmt.Errorf("%w in %s", ErrNoChecks, config.Path(l.workspace))
	}

	results := verify.Run(ctx, l.workspace, cfg.Verify,
		time.Duration(cfg.TimeoutSeconds)*time.Second)

	return &VerifyReport{Results: results, Passed: verify.AllPassed(results)}, nil
}

// FailureSummary renders the failing checks as feedback an agent can act on.
func (r *VerifyReport) FailureSummary() string {
	var b strings.Builder
	for _, res := range verify.Failed(r.Results) {
		fmt.Fprintf(&b, "check %q failed (exit %d):\n%s\n", res.Name, res.ExitCode, res.Output)
	}
	return b.String()
}
