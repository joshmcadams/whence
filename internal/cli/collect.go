package cli

import (
	"github.com/joshmcadams/ports/internal/config"
	"github.com/joshmcadams/ports/internal/inventory"
	"github.com/joshmcadams/ports/internal/model"
)

// collect returns the full merged inventory (native + Docker), unfiltered.
func collect(cfg config.Config) ([]model.Server, error) {
	return inventory.Collect(cfg)
}
