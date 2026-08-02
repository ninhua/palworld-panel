package api

func init() {
	patchFeatures = append(patchFeatures,
		"boss-auto-wave-execution-opt-in",
		"boss-auto-wave-delay-scheduling",
		"boss-auto-wave-restart-resume",
		"boss-auto-wave-uncertain-stop",
		"boss-auto-wave-single-step-cycle",
		"boss-auto-wave-database-lease",
		"boss-auto-wave-multi-process-deduplication",
		"boss-auto-wave-crash-lease-expiry",
	)
}
