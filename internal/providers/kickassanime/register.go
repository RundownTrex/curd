package kickassanime

import "github.com/wraient/curd/internal/providers"

func init() {
	providers.Register(providers.Meta{
		Name:            "kickassanime",
		Aliases:         []string{"kickass-anime", "kickass", "kaa", "kaa.lt", "kaa-lt"},
		Referrer:        "https://kaa.lt/",
		DefaultDisabled: false,
		OptOutToken:     "no-kaa",
		FallbackPrompt:  "Watch on KickAssAnime",
	}, func() providers.Provider {
		return &Provider{}
	})
}
