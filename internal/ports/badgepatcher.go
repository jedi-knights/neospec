package ports

import "context"

// BadgePatcher reads a README file, locates an existing coverage badge URL,
// replaces it with a new URL reflecting the current coverage percentage, and
// writes the file back in place. It is a no-op if no badge is found and
// createIfMissing is false.
type BadgePatcher interface {
	Patch(ctx context.Context, readmePath string, pct float64) error
}
