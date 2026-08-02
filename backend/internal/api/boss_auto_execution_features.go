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
		"boss-auto-wave-pause-control",
		"boss-auto-wave-resume-control",
		"boss-auto-wave-skip-current-control",
		"boss-auto-wave-control-audit",
		"boss-auto-wave-confirmed-failure-retry",
		"boss-auto-wave-retry-delay",
		"boss-auto-wave-retry-state-restore",
		"boss-auto-wave-uncertain-retry-block",
	)
}
