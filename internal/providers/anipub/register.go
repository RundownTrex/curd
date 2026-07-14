package anipub

import "github.com/wraient/curd/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "anipub",
		DefaultDisabled: true,
		DisableReason: "upstream domain is dead",
		Aliases:  []string{"anipub.xyz"},
		Referrer: "https://megaplay.buzz/",
	}, func() providers.Provider {
		return &Provider{}
	})
}
