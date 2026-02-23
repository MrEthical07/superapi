package modules

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/modules/health"
	"github.com/MrEthical07/superapi/internal/modules/system"
)

func All() []app.Module {
	return []app.Module{
		health.New(),
		system.New(),
	}
}
