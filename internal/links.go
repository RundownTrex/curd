package internal

import "strings"

// LinkPriorities defines the order of priority for link domains
// Higher index = higher priority
var LinkPriorities = []string{
	"mp4upload",       // Low priority - embed page
	"streamtape",      // Low priority
	"dood",            // Low priority
	"ok.ru",           // Low priority - embed page
	"allanime.uns.bio", // Low priority - embed page
	"strmup.cc",       // Low priority - embed page
	"streamwish",      // Low priority - embed page
	"vidplay",         // Medium priority - HLS
	"filemoon",        // Medium priority - HLS
	"gogocdn",         // Medium priority
	"gogoanime.com",   // Medium priority
	"dropbox.com",     // High priority - direct
	"wetransfer.com",  // High priority - direct
	"fast4speed.rsvp", // Very high priority - direct video URL (Youtube)
	"youtu-chan.com",  // Very high priority - direct video URL
	"sharepoint",      // Very high priority - direct MP4
	"wixmp.com",       // Very high priority - M3U8 list
	"repackager.wixmp.com", // Very high priority - M3U8 list
}

// GetProviderForLink returns the provider name for a given link URL
func GetProviderForLink(link string) string {
	providerMap := map[string]string{
		"repackager.wixmp.com":    "wixmp (m3u8)",
		"wixmp.com":               "wixmp (m3u8)",
		"fast4speed.rsvp":         "Youtube (mp4)",
		"youtu-chan.com":          "Youtube (mp4)",
		"sharepoint":              "Sharepoint (mp4)",
		"mp4upload":               "Mp4Upload (mp4)",
		"filemoon":                "Filemoon (HLS)",
		"vidplay":                 "Vidplay (HLS)",
		"gogocdn":                 "GogoAnime (m3u8)",
		"dood":                    "Dood (mp4)",
		"streamtape":              "StreamTape (mp4)",
	}

	for domain, providerName := range providerMap {
		if strings.Contains(link, domain) {
			return providerName
		}
	}

	// Extract domain from URL for unknown providers
	if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		if parts := strings.Split(link, "://"); len(parts) > 1 {
			if domainParts := strings.Split(parts[1], "/"); len(domainParts) > 0 {
				return domainParts[0]
			}
		}
	}

	return "Unknown Provider"
}

// PrioritizeLink takes an array of links and returns a single link based on priority
func PrioritizeLink(links []string) string {
	if len(links) == 0 {
		return ""
	}

	// Create a map for quick lookup of priorities
	priorityMap := make(map[string]int)
	for i, p := range LinkPriorities {
		priorityMap[p] = len(LinkPriorities) - i // Higher index means higher priority
	}

	highestPriority := -1
	var bestLink string

	for _, link := range links {
		for domain, priority := range priorityMap {
			if strings.Contains(link, domain) {
				if priority > highestPriority {
					highestPriority = priority
					bestLink = link
				}
				break
			}
		}
	}

	// If no priority link found, return the first link
	if bestLink == "" {
		return links[0]
	}

	return bestLink
}
