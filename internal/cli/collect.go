package cli

import (
	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/inventory"
	"github.com/joshmcadams/whence/internal/model"
)

// collect returns the full merged inventory (native + Docker), unfiltered.
func collect(cfg config.Config) ([]model.Server, error) {
	return inventory.Collect(cfg)
}
