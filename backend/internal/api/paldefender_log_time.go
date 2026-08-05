package api

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	palDefenderBracketTimePattern = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?)\]`)
	palDefenderUnrealTimePattern  = regexp.MustCompile(`^\[(\d{4}\.\d{2}\.\d{2}-\d{2}\.\d{2}\.\d{2})(?::(\d{1,9}))?\]`)
)

// palDefenderLogOccurredAt extracts the timestamp written by PalDefender or
// Unreal. Prefixes without an explicit zone are interpreted in the panel
// process location. The ingestion time remains the safe fallback.
func palDefenderLogOccurredAt(line string, fallback time.Time) time.Time {
	line = strings.TrimSpace(line)
	if line == "" {
		return fallback
	}
	if strings.HasPrefix(line, "[") {
		if end := strings.IndexByte(line, ']'); end > 1 {
			candidate := strings.TrimSpace(line[1:end])
			if parsed, err := time.Parse(time.RFC3339Nano, candidate); err == nil {
				return parsed
			}
		}
	}
	if end := strings.IndexByte(line, ' '); end > 0 {
		candidate := strings.TrimSpace(line[:end])
		if parsed, err := time.Parse(time.RFC3339Nano, candidate); err == nil {
			return parsed
		}
	}
	location := fallback.Location()
	if location == nil {
		location = time.Local
	}
	if match := palDefenderBracketTimePattern.FindStringSubmatch(line); len(match) > 1 {
		value := strings.Replace(match[1], "T", " ", 1)
		if parsed, err := time.ParseInLocation("2006-01-02 15:04:05.999999999", value, location); err == nil {
			return parsed
		}
	}
	if match := palDefenderUnrealTimePattern.FindStringSubmatch(line); len(match) > 1 {
		parsed, err := time.ParseInLocation("2006.01.02-15.04.05", match[1], location)
		if err == nil {
			if len(match) > 2 && match[2] != "" {
				fraction := match[2]
				if len(fraction) < 9 {
					fraction += strings.Repeat("0", 9-len(fraction))
				} else if len(fraction) > 9 {
					fraction = fraction[:9]
				}
				if nanoseconds, conversionErr := strconv.Atoi(fraction); conversionErr == nil {
					parsed = parsed.Add(time.Duration(nanoseconds))
				}
			}
			return parsed
		}
	}
	return fallback
}

func init() {
	patchFeatures = append(patchFeatures, "paldefender-log-original-timestamps")
}
