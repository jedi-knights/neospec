// Package ports defines the consumer-side interfaces that drive neospec's
// pluggable architecture. Each file declares a single, minimal interface.
// Concrete implementations live in internal/adapters/.
package ports

import (
	"context"

	"github.com/jedi-knights/neospec/internal/domain"
)

// NeovimProvider ensures that a Neovim binary of the requested version is
// available locally. Implementations are expected to check a cache before
// downloading, so repeated calls for the same version are cheap.
type NeovimProvider interface {
	// Ensure returns the absolute path to the nvim binary, downloading and
	// caching it if necessary.
	Ensure(ctx context.Context, version domain.Version, platform domain.Platform) (binaryPath string, err error)
}
