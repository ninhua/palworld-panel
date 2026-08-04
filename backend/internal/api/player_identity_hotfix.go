package api

import (
	"regexp"
	"time"

	"palpanel/internal/playeridentity"
)

const playerIdentityNormalizationInterval = 100 * time.Millisecond

func init() {
	// PalDefender login lines carry a platform ID followed by an IP address in
	// parentheses. Capture only the platform identity; never expose the IP as a
	// nickname candidate.
	loginLogPattern = regexp.MustCompile(`(?i)(?:^|\])\s*((?:steam|gdk|ps5)_[A-Za-z0-9_-]+)(?:\s+\([^)]*\))?\s+connected to the server\.?$`)
	patchFeatures = append(patchFeatures,
		"player-uid-canonicalization",
		"player-identity-account-merge",
		"paldefender-login-identity-parser",
		"paldefender-outbound-reply-ignore",
	)

	go func() {
		ticker := time.NewTicker(playerIdentityNormalizationInterval)
		defer ticker.Stop()
		for range ticker.C {
			gameEventBridges.Range(func(_, value any) bool {
				runtime, ok := value.(*gameEventBridgeRuntime)
				if !ok || runtime == nil {
					return true
				}
				runtime.mu.Lock()
				for index := range runtime.identities {
					runtime.identities[index].PlayerUID = playeridentity.Normalize(runtime.identities[index].PlayerUID)
					runtime.identities[index].SteamID = playeridentity.NormalizeSteamID(runtime.identities[index].SteamID)
				}
				runtime.mu.Unlock()
				return true
			})
		}
	}()
}
