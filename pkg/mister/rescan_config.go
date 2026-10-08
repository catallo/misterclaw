package mister

import "sort"

// mergeRefreshedConfig preserves formats on unscanned locations while accepting
// freshly detected core/start information. Unknown systems use the same CD
// heuristic as full discovery, applied to the combined extension set.
func mergeRefreshedConfig(system string, previous, refreshed SystemConfig) SystemConfig {
	extensions := make(map[string]bool)
	for _, ext := range previous.Extensions {
		extensions[ext] = true
	}
	for _, ext := range refreshed.Extensions {
		extensions[ext] = true
	}
	var combined []string
	isCD := false
	for ext := range extensions {
		combined = append(combined, ext)
		isCD = isCD || cdExtensions[ext]
	}
	sort.Strings(combined)
	if _, known := getDefaultConfig(system); known {
		refreshed.Extensions = combined
		return refreshed
	}
	result := previous
	result.Extensions = combined
	if refreshed.Core != "" {
		result.Core = refreshed.Core
		result.SetName = refreshed.SetName
	}
	if isCD {
		result.Type, result.Index, result.Delay = "s", 0, 1
	} else if refreshed.Type != "" {
		result.Type, result.Index, result.Delay = refreshed.Type, refreshed.Index, refreshed.Delay
	}
	return result
}
