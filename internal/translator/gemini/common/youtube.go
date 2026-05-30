package common

import "regexp"

// youTubeIDRe matches YouTube video IDs from common URL shapes (watch, youtu.be,
// shorts, embed, live). Video IDs are 11 chars of [A-Za-z0-9_-].
var youTubeIDRe = regexp.MustCompile(`(?:youtube\.com/(?:watch\?(?:[^\s"]*&)?v=|shorts/|embed/|live/)|youtu\.be/)([A-Za-z0-9_-]{11})`)

// ExtractYouTubeURLs returns canonical, de-duplicated YouTube watch URLs found in text.
// Gemini accepts these directly as a fileData part for native video understanding.
func ExtractYouTubeURLs(text string) []string {
	matches := youTubeIDRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		id := m[1]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, "https://www.youtube.com/watch?v="+id)
	}
	return out
}
