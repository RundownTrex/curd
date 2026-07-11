package allanime

import "github.com/wraient/curd/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:            "mkissa",
		Aliases:         []string{"isekai2nd", "allmanga", "allanime", "all-anime", "all anime"},
		Referrer:        "https://mkissa.to/",
		DefaultDisabled: true,
		DisableReason:   "disabled by default; set Provider to include mkissa to enable",
	}, func() providers.Provider {
		return &Provider{}
	})
}
