package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"

	"palpanel/internal/boss"
)

func (s Server) resolveRaidBaseLocation(ctx context.Context, requestedID string) (boss.RaidBaseLocation, error) {
	requestedID = strings.TrimSpace(requestedID)
	if requestedID == "" {
		return boss.RaidBaseLocation{}, errors.New("raid base id is empty")
	}
	index, status, err := s.serverSaveIndex.Current(ctx)
	if err != nil {
		state := strings.TrimSpace(status.State)
		if state != "" {
			return boss.RaidBaseLocation{}, fmt.Errorf("save index %s: %w", state, err)
		}
		return boss.RaidBaseLocation{}, fmt.Errorf("read save index: %w", err)
	}
	items := raidBaseCandidates(reflect.ValueOf(index))
	for _, item := range items {
		base, ok := raidBaseFromValue(item)
		if !ok || !raidBaseIDEqual(base.ID, requestedID) {
			continue
		}
		if !finiteRaidBaseValue(base.X) || !finiteRaidBaseValue(base.Y) || !finiteRaidBaseValue(base.Z) {
			return boss.RaidBaseLocation{}, fmt.Errorf("base %s has invalid coordinates", requestedID)
		}
		return base, nil
	}
	return boss.RaidBaseLocation{}, fmt.Errorf("base %s was not found in the current save index", requestedID)
}

func raidBaseCandidates(value reflect.Value) []reflect.Value {
	value = raidReflectValue(value)
	if !value.IsValid() {
		return nil
	}
	for _, name := range []string{"bases", "base_camps", "basecamps", "camps"} {
		if field, ok := raidReflectField(value, name); ok {
			if result := raidReflectSlice(field); len(result) > 0 {
				return result
			}
		}
	}
	// Keep the resolver compatible with save-index type renames by accepting the
	// first top-level slice whose elements expose a stable id and coordinates.
	switch value.Kind() {
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if result := raidReflectSlice(value.Field(index)); len(result) > 0 {
				if _, ok := raidBaseFromValue(result[0]); ok {
					return result
				}
			}
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if result := raidReflectSlice(iterator.Value()); len(result) > 0 {
				if _, ok := raidBaseFromValue(result[0]); ok {
					return result
				}
			}
		}
	}
	return nil
}

func raidReflectSlice(value reflect.Value) []reflect.Value {
	value = raidReflectValue(value)
	if !value.IsValid() || (value.Kind() != reflect.Slice && value.Kind() != reflect.Array) {
		return nil
	}
	result := make([]reflect.Value, 0, value.Len())
	for index := 0; index < value.Len(); index++ {
		result = append(result, value.Index(index))
	}
	return result
}

func raidBaseFromValue(value reflect.Value) (boss.RaidBaseLocation, bool) {
	value = raidReflectValue(value)
	if !value.IsValid() {
		return boss.RaidBaseLocation{}, false
	}
	id := raidReflectString(value, "id", "base_id", "basecamp_id", "camp_id")
	if id == "" {
		return boss.RaidBaseLocation{}, false
	}
	x, xOK := raidReflectNumber(value, "x")
	y, yOK := raidReflectNumber(value, "y")
	z, zOK := raidReflectNumber(value, "z")
	if !xOK || !yOK || !zOK {
		if nested, ok := raidReflectField(value, "location", "world_location", "position"); ok {
			x, xOK = raidReflectNumber(nested, "x")
			y, yOK = raidReflectNumber(nested, "y")
			z, zOK = raidReflectNumber(nested, "z")
		}
	}
	if !xOK || !yOK || !zOK {
		return boss.RaidBaseLocation{}, false
	}
	name := raidReflectString(value, "custom_name", "name", "base_name", "raw_name")
	return boss.RaidBaseLocation{
		ID: id, Name: name,
		GuildID:   raidReflectString(value, "guild_id", "group_id"),
		GuildName: raidReflectString(value, "guild_name", "group_name", "guild"),
		X:         x, Y: y, Z: z,
	}, true
}

func raidReflectField(value reflect.Value, aliases ...string) (reflect.Value, bool) {
	value = raidReflectValue(value)
	if !value.IsValid() {
		return reflect.Value{}, false
	}
	wanted := map[string]bool{}
	for _, alias := range aliases {
		wanted[raidReflectName(alias)] = true
	}
	switch value.Kind() {
	case reflect.Struct:
		typeOf := value.Type()
		for index := 0; index < value.NumField(); index++ {
			fieldType := typeOf.Field(index)
			jsonName := strings.Split(fieldType.Tag.Get("json"), ",")[0]
			if wanted[raidReflectName(fieldType.Name)] || wanted[raidReflectName(jsonName)] {
				return value.Field(index), true
			}
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			key := raidReflectValue(iterator.Key())
			if key.IsValid() && key.Kind() == reflect.String && wanted[raidReflectName(key.String())] {
				return iterator.Value(), true
			}
		}
	}
	return reflect.Value{}, false
}

func raidReflectString(value reflect.Value, aliases ...string) string {
	field, ok := raidReflectField(value, aliases...)
	if !ok {
		return ""
	}
	field = raidReflectValue(field)
	if !field.IsValid() {
		return ""
	}
	if field.Kind() == reflect.String {
		return strings.TrimSpace(field.String())
	}
	if field.CanInterface() {
		return strings.TrimSpace(fmt.Sprint(field.Interface()))
	}
	return ""
}

func raidReflectNumber(value reflect.Value, aliases ...string) (float64, bool) {
	field, ok := raidReflectField(value, aliases...)
	if !ok {
		return 0, false
	}
	field = raidReflectValue(field)
	if !field.IsValid() {
		return 0, false
	}
	switch field.Kind() {
	case reflect.Float32, reflect.Float64:
		return field.Float(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(field.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(field.Uint()), true
	}
	return 0, false
}

func raidReflectValue(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

func raidReflectName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	return value
}

func raidBaseIDEqual(left, right string) bool {
	normalize := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.Trim(value, "{}")
		return strings.ReplaceAll(value, "-", "")
	}
	return normalize(left) != "" && normalize(left) == normalize(right)
}

func finiteRaidBaseValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
