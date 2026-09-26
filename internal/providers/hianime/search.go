package hianime

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/wraient/curd/internal/curdhost"
	"github.com/wraient/curd/internal/providers"
)

var (
	flwItemMarkerRE = regexp.MustCompile(`<div\b[^>]*class=["'][^"']*flw-item[^"']*["'][^>]*>`)
	filmNameRE      = regexp.MustCompile(`(?s)<h3\b[^>]*class=["'][^"']*film-name[^"']*["'][^>]*>\s*<a\b([^>]*)>(.*?)</a>`)
	hrefRE          = regexp.MustCompile(`href=["']([^"']+)["']`)
	titleAttrRE     = regexp.MustCompile(`title=["']([^"']+)["']`)
	jnameRE         = regexp.MustCompile(`data-jname=["']([^"']+)["']`)
	posterRE        = regexp.MustCompile(`(?s)<img\b[^>]*(?:data-src|src)=["']([^"']+)["'][^>]*class=["'][^"']*film-poster-img`)
	posterAltRE     = regexp.MustCompile(`(?s)<img\b[^>]*class=["'][^"']*film-poster-img[^"']*["'][^>]*(?:data-src|src)=["']([^"']+)["']`)
	formatRE        = regexp.MustCompile(`<span\b[^>]*class=["'][^"']*fdi-item[^"']*["'][^>]*>([^<]+)</span>`)
	subCountRE      = regexp.MustCompile(`(?s)<div\b[^>]*class=["'][^"']*tick-sub[^"']*["'][^>]*>(?:<i[^>]*></i>)?\s*([0-9]+)\s*</div>`)
	dubCountRE      = regexp.MustCompile(`(?s)<div\b[^>]*class=["'][^"']*tick-dub[^"']*["'][^>]*>(?:<i[^>]*></i>)?\s*([0-9]+)\s*</div>`)
	epsCountRE      = regexp.MustCompile(`(?s)<div\b[^>]*class=["'][^"']*tick-eps[^"']*["'][^>]*>\s*([0-9]+)\s*</div>`)
	searchPunctRE   = regexp.MustCompile(`[:;?!/&()#"@\-_]+`)
)

func cleanSearchQuery(query string) string {
	// Rule 2: explicitly strip apostrophes before URL encoding
	query = strings.ReplaceAll(query, "'", "")
	query = strings.ReplaceAll(query, "’", "")
	return strings.Join(strings.Fields(query), " ")
}

func doSearchAnime(query string) ([]providers.SelectionOption, error) {
	searchURL := fmt.Sprintf("%s/search?keyword=%s", baseURL, url.QueryEscape(query))
	pageHTML, err := fetchString(searchURL, baseURL+"/")
	if err != nil {
		return nil, fmt.Errorf("hianime search request: %w", err)
	}

	// Cut off at main-sidebar so we only parse actual search results and not the Top 10 sidebar
	if idx := strings.Index(pageHTML, `id="main-sidebar"`); idx != -1 {
		pageHTML = pageHTML[:idx]
	}

	matches := flwItemMarkerRE.FindAllStringIndex(pageHTML, -1)
	if len(matches) == 0 {
		return nil, nil
	}

	options := make([]providers.SelectionOption, 0, len(matches))
	seenKeys := make(map[string]struct{}, len(matches))

	for i, m := range matches {
		start := m[0]
		end := len(pageHTML)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		itemHTML := pageHTML[start:end]

		nameMatch := filmNameRE.FindStringSubmatch(itemHTML)
		if len(nameMatch) < 3 {
			continue
		}
		attrs := nameMatch[1]
		innerText := nameMatch[2]

		hrefMatch := hrefRE.FindStringSubmatch(attrs)
		if len(hrefMatch) < 2 {
			continue
		}
		link := strings.TrimRight(hrefMatch[1], "/")
		slug := link
		if lastSlash := strings.LastIndex(slug, "/"); lastSlash != -1 {
			slug = slug[lastSlash+1:]
		}
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}

		if _, seen := seenKeys[slug]; seen {
			continue
		}
		seenKeys[slug] = struct{}{}

		var rawTitle string
		if tMatch := titleAttrRE.FindStringSubmatch(attrs); len(tMatch) >= 2 {
			rawTitle = tMatch[1]
		} else {
			rawTitle = innerText
		}
		title := strings.TrimSpace(html.UnescapeString(rawTitle))
		if title == "" {
			title = slug
		}

		var jname string
		if jMatch := jnameRE.FindStringSubmatch(attrs); len(jMatch) >= 2 {
			jname = strings.TrimSpace(html.UnescapeString(jMatch[1]))
		}

		var poster string
		if pMatch := posterRE.FindStringSubmatch(itemHTML); len(pMatch) >= 2 {
			poster = strings.TrimSpace(pMatch[1])
		} else if pMatch := posterAltRE.FindStringSubmatch(itemHTML); len(pMatch) >= 2 {
			poster = strings.TrimSpace(pMatch[1])
		}

		var format string
		if fMatch := formatRE.FindStringSubmatch(itemHTML); len(fMatch) >= 2 {
			format = strings.TrimSpace(fMatch[1])
		}

		var sub string
		if sMatch := subCountRE.FindStringSubmatch(itemHTML); len(sMatch) >= 2 {
			sub = strings.TrimSpace(sMatch[1])
		}

		var dub string
		if dMatch := dubCountRE.FindStringSubmatch(itemHTML); len(dMatch) >= 2 {
			dub = strings.TrimSpace(dMatch[1])
		}

		var eps string
		if eMatch := epsCountRE.FindStringSubmatch(itemHTML); len(eMatch) >= 2 {
			eps = strings.TrimSpace(eMatch[1])
		}

		item := searchItem{
			Slug:     slug,
			Title:    title,
			JName:    jname,
			Poster:   poster,
			Format:   format,
			Sub:      sub,
			Dub:      dub,
			Episodes: eps,
		}

		label := formatSearchLabel(item)

		options = append(options, providers.SelectionOption{
			Key:       slug,
			Label:     label,
			Title:     title, // Rule 3: pure title without metadata tags
			Thumbnail: poster,
			ExtraData: item,
		})
	}

	return options, nil
}

func formatSearchLabel(item searchItem) string {
	title := item.Title
	var tags []string

	if item.Format != "" {
		tags = append(tags, item.Format)
	}

	if item.Episodes != "" {
		tags = append(tags, fmt.Sprintf("%s episodes", item.Episodes))
	} else if item.Sub != "" || item.Dub != "" {
		var subDub []string
		if item.Sub != "" {
			subDub = append(subDub, "Sub: "+item.Sub)
		}
		if item.Dub != "" {
			subDub = append(subDub, "Dub: "+item.Dub)
		}
		tags = append(tags, strings.Join(subDub, " | "))
	}

	if len(tags) > 0 {
		return fmt.Sprintf("%s (%s)", title, strings.Join(tags, ", "))
	}
	return title
}

func searchAnime(query, mode string) ([]providers.SelectionOption, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil, fmt.Errorf("empty search query")
	}

	clean := cleanSearchQuery(trimmed)
	if clean != "" {
		if opts, err := doSearchAnime(clean); err == nil && len(opts) > 0 {
			return opts, nil
		}
	}

	// If clean differed from raw query, try raw query
	if clean != trimmed {
		if opts, err := doSearchAnime(trimmed); err == nil && len(opts) > 0 {
			return opts, nil
		}
	}

	// Try query before separator (e.g. "Bleach: Thousand-Year Blood War" -> "Bleach")
	if idx := strings.IndexAny(trimmed, ":-"); idx > 0 {
		prefix := cleanSearchQuery(trimmed[:idx])
		if prefix != "" && prefix != clean {
			if opts, err := doSearchAnime(prefix); err == nil && len(opts) > 0 {
				return opts, nil
			}
		}
	}

	// Try stripping all punctuation
	punctStripped := strings.TrimSpace(searchPunctRE.ReplaceAllString(trimmed, " "))
	if punctStripped != "" && punctStripped != clean && punctStripped != trimmed {
		if opts, err := doSearchAnime(punctStripped); err == nil && len(opts) > 0 {
			return opts, nil
		}
	}

	// Fallback to cross-looking up AniList titles (English / Romaji)
	if curdhost.SearchAniListTitles != nil {
		if en, ro, err := curdhost.SearchAniListTitles(trimmed); err == nil {
			if en != "" {
				cleanEn := cleanSearchQuery(en)
				if cleanEn != clean {
					if opts, err := doSearchAnime(cleanEn); err == nil && len(opts) > 0 {
						return opts, nil
					}
				}
			}
			if ro != "" {
				cleanRo := cleanSearchQuery(ro)
				if cleanRo != clean {
					if opts, err := doSearchAnime(cleanRo); err == nil && len(opts) > 0 {
						return opts, nil
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("hianime search: no results found for %q", query)
}
