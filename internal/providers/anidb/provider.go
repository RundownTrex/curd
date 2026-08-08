package anidb

import (
	"github.com/wraient/curd/internal/providers"
)

// Provider implements AniDB (anidb.app) catalog search and HLS stream resolution.
type Provider struct{}

func (p *Provider) Name() string {
	return "anidb"
}

func (p *Provider) SearchAnime(query, mode string) ([]providers.SelectionOption, error) {
	return searchAniDB(query, mode)
}

func (p *Provider) EpisodesList(showID, mode string) ([]string, error) {
	return getAniDBEpisodesList(showID, mode)
}

func (p *Provider) GetEpisodeURL(config providers.PlaybackConfig, id string, epNo int) ([]string, error) {
	return p.GetEpisodeURLForMode(config, id, epNo, config.SubOrDub)
}

func (p *Provider) GetEpisodeURLForMode(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, error) {
	links, _, err := p.GetEpisodeURLForModeWithHints(config, id, epNo, mode)
	return links, err
}

func (p *Provider) GetEpisodeURLForModeWithHints(config providers.PlaybackConfig, id string, epNo int, mode string) ([]string, map[string]providers.StreamPlaybackHint, error) {
	return getAniDBEpisodeStreamsForMode(id, mode, epNo)
}

func (p *Provider) ResolveProviderID(providerID, query string) (string, error) {
	if providerID != "" {
		return providerID, nil
	}
	options, err := searchAniDB(query, "")
	if err != nil || len(options) == 0 {
		return "", err
	}
	return options[0].Key, nil
}
