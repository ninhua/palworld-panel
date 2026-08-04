package api

import "regexp"

func init() {
	// Match the platform identity only. PalDefender writes the remote address in
	// parentheses after the ID; it must never become a nickname candidate.
	loginLogPattern = regexp.MustCompile(`(?i)(?:^|\])\s*((?:steam|gdk|ps5|xbox|eos)_[A-Za-z0-9_-]+)(?:\s+\([^)]*\))?\s+connected to the server\.?$`)

	// Keep platform matching aligned with the stricter login parser.
	userIDPattern = regexp.MustCompile(`(?i)\b(?:steam|gdk|ps5|xbox|eos)_[A-Za-z0-9_-]+\b`)

	// Current PalDefender descriptors use UID= while older builds used
	// PlayerUID=. Accept both so replayed dead letters preserve the canonical
	// player identity when the platform catalog is temporarily unavailable.
	playerUIDPattern = regexp.MustCompile(`(?i)\b(?:PlayerUID|UID)\s*[:=]\s*([A-Za-z0-9-]{4,128})`)

	patchFeatures = append(patchFeatures,
		"paldefender-login-identity-parser-v2",
		"paldefender-uid-descriptor-parser-v2",
		"paldefender-bridge-deadletter-self-heal-v2",
		"paldefender-player-death-noise-filter",
	)
}
