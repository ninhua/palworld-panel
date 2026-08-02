package api

func init() {
	patchFeatures = append(patchFeatures,
		"boss-auto-activity-global-guard",
		"boss-auto-activity-restart-ownership",
		"boss-auto-activity-paused-ownership",
		"boss-auto-activity-plan-serialization",
		"boss-auto-activity-terminal-release",
	)
}
