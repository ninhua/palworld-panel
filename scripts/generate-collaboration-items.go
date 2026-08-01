package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type localizedItem struct {
	Description string `json:"description"`
}

type collaborationItem struct {
	ID            string `json:"id"`
	Collaboration string `json:"collaboration"`
}

func main() {
	inputPath := flag.String("input", filepath.FromSlash("third_party/palworld-save-pal/data/json/l10n/zh-Hans/items.json"), "localized item catalog path")
	outputPath := flag.String("output", filepath.FromSlash("backend/internal/pallocalize/item-collaborations.zh-CN.json"), "generated collaboration catalog path")
	flag.Parse()

	payload, err := os.ReadFile(*inputPath)
	check(err)
	items := map[string]localizedItem{}
	check(json.Unmarshal(payload, &items))

	entries := make([]collaborationItem, 0, 135)
	counts := map[string]int{}
	for id, item := range items {
		collaboration := ""
		switch {
		case strings.Contains(item.Description, "泰拉瑞亚联动"):
			collaboration = "terraria"
		case strings.Contains(item.Description, "ULTRAKILL 联动"):
			collaboration = "ultrakill"
		}
		if collaboration == "" {
			continue
		}
		entries = append(entries, collaborationItem{ID: id, Collaboration: collaboration})
		counts[collaboration]++
	}
	if counts["terraria"] != 111 || counts["ultrakill"] != 24 {
		check(fmt.Errorf("unexpected collaboration counts: Terraria=%d ULTRAKILL=%d", counts["terraria"], counts["ultrakill"]))
	}
	sort.Slice(entries, func(i, j int) bool { return strings.ToLower(entries[i].ID) < strings.ToLower(entries[j].ID) })

	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	check(encoder.Encode(entries))
	temporary := *outputPath + ".tmp"
	check(os.WriteFile(temporary, buffer.Bytes(), 0o644))
	check(os.Rename(temporary, *outputPath))
	fmt.Printf("wrote %d collaboration items to %s\n", len(entries), *outputPath)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
