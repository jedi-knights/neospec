package ports

import (
	"context"
	"io"

	"github.com/jedi-knights/neospec/internal/domain"
)

// Reporter writes test and coverage results to an io.Writer in a specific
// format. Each output format (LCOV, Cobertura, Coveralls, JUnit, console) is a
// separate implementation. Reporters are stateless; they may be called multiple
// times with different writers.
type Reporter interface {
	Write(ctx context.Context, w io.Writer, suite *domain.SuiteResult, cov *domain.CoverageData) error
}
