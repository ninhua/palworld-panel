package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypeForTypedAdditionalProperties(t *testing.T) {
	value := schema{
		Type: "object",
		AdditionalProperties: map[string]any{
			"$ref": "#/components/schemas/PalDefenderInventorySlot",
		},
	}
	want := `Record<string, components["schemas"]["PalDefenderInventorySlot"]>`
	if got := typeFor(value, 2); got != want {
		t.Fatalf("typeFor() = %q, want %q", got, want)
	}
}

func TestTypeForAnyOfIncludingNull(t *testing.T) {
	value := schema{AnyOf: []schema{{Type: "string"}, {Type: "null"}}}
	if got, want := typeFor(value, 2), "string | null"; got != want {
		t.Fatalf("typeFor() = %q, want %q", got, want)
	}
}

func TestLiteralNumericTypes(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "int", value: int(1), want: "1"},
		{name: "int8", value: int8(-2), want: "-2"},
		{name: "int16", value: int16(-3), want: "-3"},
		{name: "int32", value: int32(-4), want: "-4"},
		{name: "int64", value: int64(-5), want: "-5"},
		{name: "uint", value: uint(6), want: "6"},
		{name: "uint8", value: uint8(7), want: "7"},
		{name: "uint16", value: uint16(8), want: "8"},
		{name: "uint32", value: uint32(9), want: "9"},
		{name: "uint64", value: uint64(10), want: "10"},
		{name: "float32", value: float32(1.25), want: "1.25"},
		{name: "float64", value: float64(2.5), want: "2.5"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := literal(test.value); got != test.want {
				t.Fatalf("literal(%T(%v)) = %q, want %q", test.value, test.value, got, test.want)
			}
		})
	}
}

func TestOpenAPIGeneratesMonitorDiagnosticContracts(t *testing.T) {
	output := filepath.Join(t.TempDir(), "contracts.ts")
	if err := run(filepath.Join("..", "..", "..", "docs", "openapi.yaml"), output); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	contract := string(body)
	for _, want := range []string{
		`"MonitorRiskReason":`,
		`"MonitorSample":`,
		`"host_memory_total_bytes": number`,
		`"workload_memory_usage_bytes": number`,
		`"oom_killed": boolean`,
		`"lifecycle_available": boolean`,
		`"risk_reasons": Array<components["schemas"]["MonitorRiskReason"]>`,
		`"MonitorSnapshot":`,
		`"SupportBundleStatus":`,
		`"schema_version": 1;`,
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("generated monitor contract does not contain %q", want)
		}
	}
}

func TestOpenAPIGeneratesPlayerAndSaveIndexContracts(t *testing.T) {
	output := filepath.Join(t.TempDir(), "contracts.ts")
	if err := run(filepath.Join("..", "..", "..", "docs", "openapi.yaml"), output); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	contract := string(body)
	for _, want := range []string{
		`"Player":`,
		`"online_source": "none" | "rest" | "paldefender" | "rest+paldefender"`,
		`"online_stale": boolean`,
		`"gm_user_id"?: string`,
		`"SaveIndexStatus":`,
		`"oodle_available"?: boolean`,
		`"error_detail"?: string`,
		`"PlayerListEnvelope":`,
		`"PlayerDetailEnvelope":`,
		`"PlayerInventoryEnvelope":`,
		`"PlayerDataView":`,
		`"online_overlay": boolean`,
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("generated player/save-index contract does not contain %q", want)
		}
	}
}

func TestOpenAPIGeneratesIncidentContracts(t *testing.T) {
	output := filepath.Join(t.TempDir(), "contracts.ts")
	if err := run(filepath.Join("..", "..", "..", "docs", "openapi.yaml"), output); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	contract := string(body)
	for _, want := range []string{
		`"Incident":`,
		`"IncidentEvent":`,
		`"IncidentDelivery":`,
		`"IncidentWebhookStatus":`,
		`"IncidentStatus":`,
		`"IncidentSeverity":`,
		`"IncidentListEnvelope":`,
		`"IncidentDetailEnvelope":`,
		`"status": components["schemas"]["IncidentStatus"]`,
		`"severity": components["schemas"]["IncidentSeverity"]`,
		`"IncidentStatus": "open" | "acknowledged" | "resolved"`,
		`"IncidentSeverity": "info" | "warning" | "error" | "critical"`,
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("generated incident contract does not contain %q", want)
		}
	}
}
