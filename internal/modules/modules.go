package modules

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/modules/health"
	"github.com/MrEthical07/superapi/internal/modules/system"
	"github.com/MrEthical07/superapi/internal/modules/tenants"
	// MODULE_IMPORTS
)

func All() []app.Module {
	return []app.Module{
		health.New(),
		system.New(),
		tenants.New(),
		// MODULE_LIST
	}
}
