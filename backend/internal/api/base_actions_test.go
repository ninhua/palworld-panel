package api

import (
	"testing"

	"palpanel/internal/saveindex"
)

func TestBaseProbeMatchesExpectedGuild(t *testing.T) {
	base := saveindex.Base{GuildID: "guild-42", GuildName: "Dawn Guild"}
	tests := map[string]struct {
		output string
		want   bool
	}{
		"guild id":        {output: "Nearest base belongs to guild guild-42", want: true},
		"guild name":      {output: "Nearest base: Dawn Guild", want: true},
		"different guild": {output: "Nearest base: Other Guild", want: false},
		"missing base":    {output: "No base found", want: false},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := baseProbeMatches(test.output, base); got != test.want {
				t.Fatalf("baseProbeMatches(%q) = %v, want %v", test.output, got, test.want)
			}
		})
	}
}

func TestFindBaseDoesNotReturnUnknownBase(t *testing.T) {
	bases := []saveindex.Base{{ID: "base-1", Name: "Main Base"}}
	if _, found := findBase(bases, "missing"); found {
		t.Fatal("findBase returned an unknown base")
	}
}
