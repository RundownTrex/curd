package anidb

import "github.com/wraient/curd/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:            "anidb",
		Aliases:         []string{"anidb.app", "anidb app", "anidb-app"},
		Referrer:        "https://anidb.app/",
		DefaultDisabled: false,
	}, func() providers.Provider {
		return &Provider{}
	})
}
