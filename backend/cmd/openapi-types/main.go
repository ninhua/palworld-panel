package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

type document struct {
	Components struct {
		Schemas map[string]schema `yaml:"schemas"`
	} `yaml:"components"`
}

type schema struct {
	Ref                  string            `yaml:"$ref"`
	Type                 string            `yaml:"type"`
	Const                any               `yaml:"const"`
	Enum                 []any             `yaml:"enum"`
	Required             []string          `yaml:"required"`
	Properties           map[string]schema `yaml:"properties"`
	AllOf                []schema          `yaml:"allOf"`
	AnyOf                []schema          `yaml:"anyOf"`
	Items                *schema           `yaml:"items"`
	AdditionalProperties any               `yaml:"additionalProperties"`
}

func main() {
	specPath := flag.String("spec", "../docs/openapi.yaml", "OpenAPI document")
	outputPath := flag.String("output", "../frontend/src/api/generated/contracts.ts", "generated TypeScript output")
	flag.Parse()
	if err := run(*specPath, *outputPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(specPath, outputPath string) error {
	body, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read OpenAPI document: %w", err)
	}
	var spec document
	if err := yaml.Unmarshal(body, &spec); err != nil {
		return fmt.Errorf("parse OpenAPI document: %w", err)
	}
	if len(spec.Components.Schemas) == 0 {
		return fmt.Errorf("OpenAPI document has no component schemas")
	}
	var out bytes.Buffer
	out.WriteString("// Generated from docs/openapi.yaml. Do not edit.\n")
	out.WriteString("export interface components {\n  schemas: {\n")
	names := sortedKeys(spec.Components.Schemas)
	for _, name := range names {
		out.WriteString("    ")
		out.WriteString(strconv.Quote(name))
		out.WriteString(": ")
		out.WriteString(typeFor(spec.Components.Schemas[name], 2))
		out.WriteString(";\n")
	}
	out.WriteString("  };\n}\n")
	if err := os.MkdirAll(filepathDir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, out.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write generated types: %w", err)
	}
	return nil
}

func typeFor(value schema, indent int) string {
	if value.Ref != "" {
		parts := strings.Split(value.Ref, "/")
		return "components[\"schemas\"][" + strconv.Quote(parts[len(parts)-1]) + "]"
	}
	if len(value.AllOf) > 0 {
		parts := make([]string, 0, len(value.AllOf))
		for _, item := range value.AllOf {
			parts = append(parts, typeFor(item, indent))
		}
		return strings.Join(parts, " & ")
	}
	if len(value.AnyOf) > 0 {
		parts := make([]string, 0, len(value.AnyOf))
		for _, item := range value.AnyOf {
			parts = append(parts, typeFor(item, indent))
		}
		return strings.Join(parts, " | ")
	}
	if value.Const != nil {
		return literal(value.Const)
	}
	if len(value.Enum) > 0 {
		parts := make([]string, 0, len(value.Enum))
		for _, item := range value.Enum {
			parts = append(parts, literal(item))
		}
		return strings.Join(parts, " | ")
	}
	switch value.Type {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "null":
		return "null"
	case "array":
		if value.Items == nil {
			return "unknown[]"
		}
		return "Array<" + typeFor(*value.Items, indent) + ">"
	case "object":
		if len(value.Properties) == 0 {
			if enabled, ok := value.AdditionalProperties.(bool); ok && enabled {
				return "Record<string, unknown>"
			}
			if item, ok := additionalPropertiesSchema(value.AdditionalProperties); ok {
				return "Record<string, " + typeFor(item, indent) + ">"
			}
			return "Record<string, never>"
		}
		required := make(map[string]bool, len(value.Required))
		for _, name := range value.Required {
			required[name] = true
		}
		var out strings.Builder
		out.WriteString("{\n")
		for _, name := range sortedKeys(value.Properties) {
			out.WriteString(strings.Repeat("  ", indent+1))
			out.WriteString(strconv.Quote(name))
			if !required[name] {
				out.WriteByte('?')
			}
			out.WriteString(": ")
			out.WriteString(typeFor(value.Properties[name], indent+1))
			out.WriteString(";\n")
		}
		out.WriteString(strings.Repeat("  ", indent))
		out.WriteByte('}')
		return out.String()
	default:
		return "unknown"
	}
}

func additionalPropertiesSchema(value any) (schema, bool) {
	if value == nil {
		return schema{}, false
	}
	body, err := yaml.Marshal(value)
	if err != nil {
		return schema{}, false
	}
	var result schema
	if err := yaml.Unmarshal(body, &result); err != nil {
		return schema{}, false
	}
	if result.Ref == "" && result.Type == "" && len(result.AllOf) == 0 && len(result.AnyOf) == 0 {
		return schema{}, false
	}
	return result, true
}

func literal(value any) string {
	switch typed := value.(type) {
	case string:
		return strconv.Quote(typed)
	case bool:
		return strconv.FormatBool(typed)
	case int:
		return strconv.Itoa(typed)
	case int8:
		return strconv.FormatInt(int64(typed), 10)
	case int16:
		return strconv.FormatInt(int64(typed), 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint:
		return strconv.FormatUint(uint64(typed), 10)
	case uint8:
		return strconv.FormatUint(uint64(typed), 10)
	case uint16:
		return strconv.FormatUint(uint64(typed), 10)
	case uint32:
		return strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return "unknown"
	}
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func filepathDir(path string) string {
	index := strings.LastIndexAny(path, `/\\`)
	if index < 0 {
		return "."
	}
	return path[:index]
}
