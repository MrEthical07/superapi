package modules

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/modules/health"
)

func All() []app.Module {
	return []app.Module{
		health.New(),
	}
}
