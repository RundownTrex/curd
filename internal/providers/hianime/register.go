package hianime

import "github.com/wraient/curd/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:     "hianime",
		Aliases:  []string{"hianime.to", "hianime.at", "hianimetv", "aniwatch"},
		Referrer: "https://zokoanime.video/",
	}, func() providers.Provider {
		return &Provider{}
	})
}
