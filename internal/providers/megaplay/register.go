package megaplay

import "github.com/wraient/curd/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "megaplay",
		Aliases:  []string{"megaplay.buzz", "anikoto", "anikoto.cz"},
		Referrer: "https://megaplay.buzz/",
	}, func() providers.Provider {
		return &Provider{}
	})
}
