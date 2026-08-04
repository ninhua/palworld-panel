package boss

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	bossLifecycleMaxLogFiles  = 4
	bossLifecycleMaxLineBytes = 1 << 20
)

type bossLifecycleLogCursor struct {
	Offset     int64  `json:"offset"`
	FileSize   int64  `json:"file_size"`
	PrefixHash string `json:"prefix_hash,omitempty"`
	PrefixSize int64  `json:"prefix_size,omitempty"`
	Generation int64  `json:"generation"`
}

type bossLifecycleDeathEvent struct {
	Hash       string
	Path       string
	Offset     int64
	TargetName string
	TargetID   string
	RawLine    string
	DetectedAt string
}

var bossLifecycleKillPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(.+?)\s+has killed\s+(?:Pal\s+)?'([^']+)'(?:\s+\(([^)]+)\))?`),
	regexp.MustCompile(`(?i)'([^']+)'\s+was (?:attacked|killed) by\s+'([^']+)'\s+and died`),
}

func captureBossLifecycleLogCursors(logDirectory string) (map[string]bossLifecycleLogCursor, error) {
	files, err := bossLifecycleLogFiles(logDirectory)
	if err != nil {
		return nil, err
	}
	cursors := make(map[string]bossLifecycleLogCursor, len(files))
	for _, path := range files {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		prefixSize := minInt64(info.Size(), 4096)
		prefix, _ := bossLifecycleFilePrefixHash(path, prefixSize)
		cursors[path] = bossLifecycleLogCursor{Offset: info.Size(), FileSize: info.Size(), PrefixHash: prefix, PrefixSize: prefixSize}
	}
	return cursors, nil
}

func scanBossLifecycleLogs(logDirectory string, cursors map[string]bossLifecycleLogCursor, aliases []string, maxReadBytes int64) ([]bossLifecycleDeathEvent, map[string]bossLifecycleLogCursor, int64, error) {
	if maxReadBytes <= 0 {
		maxReadBytes = 2 << 20
	}
	files, err := bossLifecycleLogFiles(logDirectory)
	if err != nil {
		return nil, cursors, 0, err
	}
	if cursors == nil {
		cursors = map[string]bossLifecycleLogCursor{}
	}
	wanted := map[string]bool{}
	for _, alias := range aliases {
		if normalized := normalizeBossLifecyclePal(alias); normalized != "" {
			wanted[normalized] = true
		}
	}
	if len(wanted) == 0 {
		return nil, cursors, 0, errors.New("boss lifecycle target PalID is unavailable")
	}

	updated := make(map[string]bossLifecycleLogCursor, len(cursors)+len(files))
	for path, cursor := range cursors {
		updated[path] = cursor
	}
	var events []bossLifecycleDeathEvent
	var totalRead int64
	for _, path := range files {
		if totalRead >= maxReadBytes {
			break
		}
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		cursor := updated[path]
		prefixSize := cursor.PrefixSize
		if prefixSize <= 0 {
			prefixSize = minInt64(info.Size(), 4096)
		}
		prefix, _ := bossLifecycleFilePrefixHash(path, prefixSize)
		rotated := info.Size() < cursor.Offset || cursor.PrefixHash != "" && prefix != "" && cursor.PrefixHash != prefix
		if rotated {
			cursor.Offset = 0
			cursor.Generation++
			prefixSize = minInt64(info.Size(), 4096)
			prefix, _ = bossLifecycleFilePrefixHash(path, prefixSize)
		}
		cursor.FileSize = info.Size()
		cursor.PrefixHash = prefix
		cursor.PrefixSize = prefixSize
		remaining := maxReadBytes - totalRead
		found, nextOffset, readBytes, readErr := scanBossLifecycleFile(path, cursor, wanted, remaining)
		if readErr != nil {
			return events, updated, totalRead, readErr
		}
		cursor.Offset = nextOffset
		cursor.FileSize = info.Size()
		updated[path] = cursor
		events = append(events, found...)
		totalRead += readBytes
	}
	return events, updated, totalRead, nil
}

func scanBossLifecycleFile(path string, cursor bossLifecycleLogCursor, wanted map[string]bool, limit int64) ([]bossLifecycleDeathEvent, int64, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, cursor.Offset, 0, fmt.Errorf("open PalDefender log: %w", err)
	}
	defer file.Close()
	if _, err := file.Seek(cursor.Offset, io.SeekStart); err != nil {
		return nil, cursor.Offset, 0, fmt.Errorf("seek PalDefender log: %w", err)
	}
	reader := bufio.NewReaderSize(io.LimitReader(file, limit), 64<<10)
	offset := cursor.Offset
	var consumed int64
	var events []bossLifecycleDeathEvent
	for consumed < limit {
		lineStart := offset
		line, readErr := reader.ReadString('\n')
		if len(line) > bossLifecycleMaxLineBytes {
			return events, offset, consumed, errors.New("PalDefender log line exceeds lifecycle limit")
		}
		if len(line) > 0 {
			read := int64(len(line))
			consumed += read
			if readErr == io.EOF && !strings.HasSuffix(line, "\n") {
				// Keep a partial final line for the next scan.
				return events, lineStart, consumed - read, nil
			}
			offset += read
			if targetName, targetID, matched := parseBossLifecycleDeath(line, wanted); matched {
				hashInput := fmt.Sprintf("%s|%d|%d|%s", path, cursor.Generation, lineStart, strings.TrimSpace(line))
				digest := sha256.Sum256([]byte(hashInput))
				events = append(events, bossLifecycleDeathEvent{
					Hash: hex.EncodeToString(digest[:]), Path: path, Offset: lineStart,
					TargetName: targetName, TargetID: targetID, RawLine: strings.TrimSpace(line),
					DetectedAt: time.Now().UTC().Format(time.RFC3339Nano),
				})
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return events, offset, consumed, nil
			}
			return events, offset, consumed, fmt.Errorf("read PalDefender log: %w", readErr)
		}
	}
	return events, offset, consumed, nil
}

func parseBossLifecycleDeath(line string, wanted map[string]bool) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	if match := bossLifecycleKillPatterns[0].FindStringSubmatch(line); len(match) > 0 {
		name := strings.TrimSpace(match[2])
		id := ""
		if len(match) > 3 {
			id = strings.TrimSpace(match[3])
		}
		if wanted[normalizeBossLifecyclePal(id)] || wanted[normalizeBossLifecyclePal(name)] {
			return name, id, true
		}
	}
	if match := bossLifecycleKillPatterns[1].FindStringSubmatch(line); len(match) > 0 {
		target := strings.TrimSpace(match[1])
		if wanted[normalizeBossLifecyclePal(target)] {
			return target, target, true
		}
	}
	return "", "", false
}

func bossLifecycleLogFiles(logDirectory string) ([]string, error) {
	logDirectory = strings.TrimSpace(logDirectory)
	if logDirectory == "" {
		return nil, errors.New("PalDefender log directory is unavailable")
	}
	entries, err := os.ReadDir(logDirectory)
	if err != nil {
		return nil, fmt.Errorf("read PalDefender logs: %w", err)
	}
	type candidate struct {
		path string
		at   time.Time
	}
	items := make([]candidate, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".log") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		items = append(items, candidate{path: filepath.Join(logDirectory, entry.Name()), at: info.ModTime()})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].at.After(items[j].at) })
	if len(items) > bossLifecycleMaxLogFiles {
		items = items[:bossLifecycleMaxLogFiles]
	}
	sort.Slice(items, func(i, j int) bool { return items[i].at.Before(items[j].at) })
	paths := make([]string, 0, len(items))
	for _, item := range items {
		paths = append(paths, item.path)
	}
	return paths, nil
}

func bossLifecycleFilePrefixHash(path string, size int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if size <= 0 {
		return "", nil
	}
	if size > 4096 {
		size = 4096
	}
	buffer := make([]byte, int(size))
	read, err := io.ReadFull(file, buffer)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", err
	}
	digest := sha256.Sum256(buffer[:read])
	return hex.EncodeToString(digest[:]), nil
}

func normalizeBossLifecyclePal(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, "'\"")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
