package api

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"palpanel/internal/saveindex"
)

type saveHistorySourceView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type saveHistorySnapshotView struct {
	ID          string           `json:"id"`
	Fingerprint string           `json:"fingerprint"`
	GeneratedAt string           `json:"generated_at"`
	CapturedAt  string           `json:"captured_at"`
	Parser      string           `json:"parser"`
	Counts      saveindex.Counts `json:"counts"`
	SizeBytes   int64            `json:"size_bytes"`
}

type saveHistoryStateView struct {
	Source        saveHistorySourceView     `json:"source"`
	Retention     int                       `json:"retention"`
	MaxTotalBytes int64                     `json:"max_total_bytes"`
	TotalBytes    int64                     `json:"total_bytes"`
	Items         []saveHistorySnapshotView `json:"items"`
}

type saveHistoryDiffView struct {
	From    saveHistorySnapshotView      `json:"from"`
	To      saveHistorySnapshotView      `json:"to"`
	Summary saveindex.HistoryDiffSummary `json:"summary"`
	Total   int                          `json:"total"`
	Limit   int                          `json:"limit"`
	Offset  int                          `json:"offset"`
	Items   []saveindex.HistoryChange    `json:"items"`
}

func (s Server) listSaveHistory(c *gin.Context) {
	captureErr := s.saveIndex.EnsureHistorySnapshot()
	if captureErr != nil && !errors.Is(captureErr, os.ErrNotExist) {
		if errors.Is(captureErr, saveindex.ErrDisabled) {
			fail(c, http.StatusConflict, "save_index_disabled", "save indexing is disabled")
			return
		}
		if errors.Is(captureErr, saveindex.ErrHistoryCorrupt) {
			fail(c, http.StatusConflict, "save_history_snapshot_corrupt", "save history failed integrity validation")
			return
		}
		fail(c, http.StatusInternalServerError, "save_history_capture_failed", "the current save index could not be added to history")
		return
	}
	state, err := s.saveIndex.History()
	if err != nil {
		if errors.Is(err, saveindex.ErrHistoryCorrupt) {
			fail(c, http.StatusConflict, "save_history_snapshot_corrupt", "save history failed integrity validation")
			return
		}
		fail(c, http.StatusInternalServerError, "save_history_read_failed", "save history could not be read")
		return
	}
	source, err := s.store.ActiveSaveSource(c.Request.Context())
	if err != nil {
		fail(c, http.StatusInternalServerError, "save_source_read_failed", "the active save source could not be read")
		return
	}
	ok(c, saveHistoryStateView{
		Source:    saveHistorySourceView{ID: source.ID, Name: source.Name, Kind: source.Kind},
		Retention: state.Retention, MaxTotalBytes: state.MaxTotalBytes, TotalBytes: state.TotalBytes,
		Items: saveHistorySnapshotViews(state.Items),
	})
}

func (s Server) diffSaveHistory(c *gin.Context) {
	fromID := strings.TrimSpace(c.Query("from"))
	toID := strings.TrimSpace(c.Query("to"))
	if fromID == "" || toID == "" || fromID == toID {
		fail(c, http.StatusBadRequest, "save_history_range_invalid", "from and to must identify two different snapshots")
		return
	}
	query := strings.TrimSpace(c.Query("q"))
	if len([]rune(query)) > 128 {
		fail(c, http.StatusBadRequest, "save_history_query_too_long", "q must contain at most 128 characters")
		return
	}
	category := strings.ToLower(strings.TrimSpace(c.DefaultQuery("category", "all")))
	if !validSaveHistoryCategory(category) {
		fail(c, http.StatusBadRequest, "save_history_category_invalid", "category must be all, players, guilds, bases, pals, containers, or items")
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit < 1 || limit > 500 {
		fail(c, http.StatusBadRequest, "save_history_limit_invalid", "limit must be between 1 and 500")
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		fail(c, http.StatusBadRequest, "save_history_offset_invalid", "offset must be zero or greater")
		return
	}
	diff, err := s.saveIndex.HistoryDiff(fromID, toID, saveindex.HistoryDiffOptions{
		Category: category, Query: query, Limit: limit, Offset: offset,
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail(c, http.StatusNotFound, "save_history_snapshot_not_found", "save history snapshot was not found for the active source")
			return
		}
		if errors.Is(err, saveindex.ErrHistoryCorrupt) {
			fail(c, http.StatusConflict, "save_history_snapshot_corrupt", "save history failed integrity validation")
			return
		}
		fail(c, http.StatusInternalServerError, "save_history_diff_failed", "save history diff could not be calculated")
		return
	}
	ok(c, saveHistoryDiffView{
		From: saveHistorySnapshotViewFor(diff.From), To: saveHistorySnapshotViewFor(diff.To),
		Summary: diff.Summary, Total: diff.Total, Limit: diff.Limit, Offset: diff.Offset, Items: diff.Items,
	})
}

func saveHistorySnapshotViews(items []saveindex.HistorySnapshot) []saveHistorySnapshotView {
	views := make([]saveHistorySnapshotView, 0, len(items))
	for _, item := range items {
		views = append(views, saveHistorySnapshotViewFor(item))
	}
	return views
}

func saveHistorySnapshotViewFor(item saveindex.HistorySnapshot) saveHistorySnapshotView {
	return saveHistorySnapshotView{
		ID: item.ID, Fingerprint: item.Fingerprint, GeneratedAt: item.GeneratedAt,
		CapturedAt: item.CapturedAt, Parser: item.Parser, Counts: item.Counts, SizeBytes: item.SizeBytes,
	}
}

func validSaveHistoryCategory(value string) bool {
	switch value {
	case "all", "players", "guilds", "bases", "pals", "containers", "items":
		return true
	default:
		return false
	}
}
