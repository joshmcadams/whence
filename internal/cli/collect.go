package cli

import (
	"github.com/jmcadams/ports/internal/config"
	"github.com/jmcadams/ports/internal/inventory"
	"github.com/jmcadams/ports/internal/model"
)

// collect returns the full merged inventory (native + Docker), unfiltered.
func collect(cfg config.Config) ([]model.Server, error) {
	return inventory.Collect(cfg)
}
