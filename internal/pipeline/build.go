package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"pricing-catalog/pkg/catalog"
	"pricing-catalog/pkg/connector"
)

var (
	ErrNilConnector = errors.New("connector is nil")
	ErrEmptyCatalog = errors.New("connector returned no pricing records")
)

// BuildOptions controls creation of a deterministic catalog snapshot.
//
// Version and GeneratedAt are deliberately explicit. The eventual refresh
// workflow should provide these values rather than having connectors invent
// release metadata.
type BuildOptions struct {
	Version     string
	GeneratedAt time.Time
}

func (o BuildOptions) validate() error {
	if strings.TrimSpace(o.Version) == "" {
		return errors.New("snapshot version is required")
	}
	if o.GeneratedAt.IsZero() {
		return errors.New("snapshot generated_at is required")
	}
	return nil
}

// BuildSnapshot is the supported path from a connector to a catalog snapshot.
//
// Connector.Fetch is the provider-specific fetch + normalization boundary.
// This package owns the generic validation, deterministic ordering, and
// snapshot construction that follows it.
func BuildSnapshot(
	ctx context.Context,
	c connector.Connector,
	options BuildOptions,
) (catalog.Snapshot, error) {
	if c == nil {
		return catalog.Snapshot{}, ErrNilConnector
	}

	if strings.TrimSpace(c.Name()) == "" {
		return catalog.Snapshot{}, errors.New("connector name is required")
	}

	if err := options.validate(); err != nil {
		return catalog.Snapshot{}, err
	}

	records, err := c.Fetch(ctx)
	if err != nil {
		return catalog.Snapshot{}, fmt.Errorf("fetch connector %q: %w", c.Name(), err)
	}

	if len(records) == 0 {
		return catalog.Snapshot{}, fmt.Errorf(
			"connector %q: %w",
			c.Name(),
			ErrEmptyCatalog,
		)
	}

	catalogView, err := catalog.New(
		options.Version,
		records,
		options.GeneratedAt.UTC(),
	)
	if err != nil {
		return catalog.Snapshot{}, fmt.Errorf(
			"build snapshot from connector %q: %w",
			c.Name(),
			err,
		)
	}

	return catalogView.Snapshot(), nil
}

// BuildAndWriteJSON runs the supported build pipeline and writes the
// resulting validated snapshot.
func BuildAndWriteJSON(
	ctx context.Context,
	c connector.Connector,
	options BuildOptions,
	w io.Writer,
) error {
	snapshot, err := BuildSnapshot(ctx, c, options)
	if err != nil {
		return err
	}

	return WriteSnapshot(w, snapshot)
}
