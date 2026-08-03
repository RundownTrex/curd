package allanime

import "github.com/wraient/curd/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:            "mkissa",
		Aliases:         []string{"isekai2nd", "allmanga", "allanime", "all-anime", "all anime", "anidb", "anidb.app"},
		Referrer:        "https://anidb.app/",
		DefaultDisabled: false,
	}, func() providers.Provider {
		return &Provider{}
	})
}
