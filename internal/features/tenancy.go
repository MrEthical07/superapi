package features

import (
	"github.com/MrEthical07/superapi/internal/core/app"
	"github.com/MrEthical07/superapi/internal/tenancy"
)

func tenancyFeature() app.Feature { return tenancy.New() }
