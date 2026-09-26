package hianime

// searchItem contains parsed anime metadata from a HiAnime search result card.
type searchItem struct {
	Slug     string
	Title    string
	JName    string
	Poster   string
	Format   string
	Sub      string
	Dub      string
	Episodes string
}

// episodesResponse models the JSON payload returned by HiAnime's episode list endpoint.
type episodesResponse struct {
	Status     bool   `json:"status"`
	TotalItems int    `json:"totalItems"`
	HTML       string `json:"html"`
}

// serversResponse models the JSON payload returned by HiAnime's episode servers endpoint.
type serversResponse struct {
	Status bool   `json:"status"`
	HTML   string `json:"html"`
}

// episodeEntry is an item from the parsed episode list.
type episodeEntry struct {
	Number string
	ID     string
	Title  string
}

// episodeServer represents an available stream host for an episode.
type episodeServer struct {
	Type       string // "sub" or "dub"
	Name       string // "ZokoAnime", "HD-1", "Vidstream-2", etc.
	Hash       string // raw base64 string from data-hash
	DecodedURL string // decoded URL from Hash
}

// zokoPayload models the deobfuscated JSON config embedded in ZokoAnime player pages.
type zokoPayload struct {
	Src       string         `json:"src"`
	Subtitles []zokoSubtitle `json:"subtitles"`
}

// zokoSubtitle models individual subtitle track entries from ZokoAnime.
type zokoSubtitle struct {
	Lang    string `json:"lang"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
	Src     string `json:"src"`
}
