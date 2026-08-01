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

type palDatabase struct {
	Version string `json:"Version"`
	Pals    []struct {
		InternalName   string            `json:"InternalName"`
		LocalizedNames map[string]string `json:"LocalizedNames"`
	} `json:"Pals"`
}

type palEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func main() {
	databasePath := flag.String("db", filepath.FromSlash("third_party/palcalc/PalCalc.Model/db.json"), "PalCalc db.json path")
	outputPath := flag.String("output", filepath.FromSlash("backend/internal/pallocalize/pals.palcalc-v26.zh-CN.json"), "generated Pal catalog path")
	flag.Parse()

	payload, err := os.ReadFile(*databasePath)
	check(err)
	var database palDatabase
	check(json.Unmarshal(payload, &database))
	if database.Version != "v26" {
		check(fmt.Errorf("expected PalCalc v26, got %q", database.Version))
	}
	entries := make([]palEntry, 0, len(database.Pals))
	seen := map[string]bool{}
	for _, pal := range database.Pals {
		id := strings.TrimSpace(pal.InternalName)
		name := strings.TrimSpace(pal.LocalizedNames["zh-Hans"])
		if id == "" || name == "" {
			check(fmt.Errorf("Pal is missing InternalName or zh-Hans name: %q", id))
		}
		key := strings.ToLower(id)
		if seen[key] {
			check(fmt.Errorf("duplicate Pal ID %q", id))
		}
		seen[key] = true
		entries = append(entries, palEntry{ID: id, Name: name, Kind: "standard"})
	}
	if len(entries) != 299 {
		check(fmt.Errorf("expected 299 PalCalc Pals, got %d", len(entries)))
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
	fmt.Printf("wrote %d PalCalc %s Pals to %s\n", len(entries), database.Version, *outputPath)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
