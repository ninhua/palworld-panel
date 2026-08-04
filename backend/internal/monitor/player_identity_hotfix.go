package monitor

import (
	"time"

	"palpanel/internal/playeridentity"
)

const playerPresenceIdentityNormalizationInterval = 100 * time.Millisecond

func init() {
	go func() {
		ticker := time.NewTicker(playerPresenceIdentityNormalizationInterval)
		defer ticker.Stop()
		for range ticker.C {
			playerPresenceRuntimes.Range(func(_, value any) bool {
				runtime, ok := value.(*playerPresenceRuntime)
				if !ok || runtime == nil {
					return true
				}
				runtime.mu.Lock()
				for index := range runtime.snapshot.Players {
					runtime.snapshot.Players[index].PlayerUID = playeridentity.Normalize(runtime.snapshot.Players[index].PlayerUID)
					runtime.snapshot.Players[index].SteamID = playeridentity.NormalizeSteamID(runtime.snapshot.Players[index].SteamID)
				}
				runtime.mu.Unlock()
				return true
			})
		}
	}()
}
