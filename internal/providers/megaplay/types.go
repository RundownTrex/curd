package megaplay

// SearchItem is stored in SelectionOption.ExtraData for mapping hints.
type SearchItem struct {
	MalID    int
	Title    string
	Episodes int
}

// anilistResponse is the top-level AniList GraphQL response.
type anilistResponse struct {
	Data struct {
		Page struct {
			Media []anilistMedia `json:"media"`
		} `json:"Page"`
	} `json:"data"`
}

type anilistMedia struct {
	ID    int `json:"id"`
	Title struct {
		English string `json:"english"`
		Romaji  string `json:"romaji"`
	} `json:"title"`
	IDMal    *int `json:"idMal"`
	Episodes *int `json:"episodes"`
}

// megaplaySourcesResponse models the megaplay.buzz getSources JSON response.
// The "sources" field can be either a JSON array or a JSON object;
// we use sourcesRaw to handle both cases.
type megaplaySourcesResponse struct {
	SourcesRaw interface{} `json:"sources"`
	Tracks     []subTrack  `json:"tracks"`
	Captions   []subTrack  `json:"captions"`
	Subtitles  []subTrack  `json:"subtitles"`
}

// streamFile extracts the HLS stream URL from the polymorphic sources field.
func (m *megaplaySourcesResponse) streamFile() string {
	switch v := m.SourcesRaw.(type) {
	case string:
		return v
	case map[string]interface{}:
		if f, ok := v["file"].(string); ok {
			return f
		}
	case []interface{}:
		if len(v) > 0 {
			if obj, ok := v[0].(map[string]interface{}); ok {
				if f, ok := obj["file"].(string); ok {
					return f
				}
			}
		}
	}
	return ""
}

// allSubTracks merges tracks, captions, and subtitles into a single slice.
func (m *megaplaySourcesResponse) allSubTracks() []subTrack {
	var all []subTrack
	all = append(all, m.Tracks...)
	all = append(all, m.Captions...)
	all = append(all, m.Subtitles...)
	return all
}

type subTrack struct {
	File    string `json:"file"`
	Src     string `json:"src"`
	URL     string `json:"url"`
	Label   string `json:"label"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Type    string `json:"type"`
	Default bool   `json:"default"`
}

// fileURL returns whichever of File/Src/URL is non-empty.
func (t subTrack) fileURL() string {
	for _, candidate := range []string{t.File, t.Src, t.URL} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

// kindOrType returns whichever of Kind/Type is non-empty.
func (t subTrack) kindOrType() string {
	if t.Kind != "" {
		return t.Kind
	}
	return t.Type
}

// labelOrTitle returns whichever of Label/Title is non-empty.
func (t subTrack) labelOrTitle() string {
	if t.Label != "" {
		return t.Label
	}
	return t.Title
}
