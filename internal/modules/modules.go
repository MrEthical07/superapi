package modules

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/modules/health"
	"github.com/MrEthical07/superapi/internal/modules/system"
	// MODULE_IMPORTS
)

// START HERE:
// - This registry controls which modules are loaded at runtime.
// - Module generators update MODULE_IMPORTS and MODULE_LIST markers.
// - Route details live inside each module's routes.go file.

// All returns the complete runtime module list in registration order.
func All() []app.Module {
	return []app.Module{
		health.New(),
		system.New(),
		// MODULE_LIST
	}
}
