package playeridentity

import (
	"database/sql/driver"
	"fmt"
	"strings"

	"modernc.org/sqlite"
)

const (
	// SQLiteCollation compares PlayerUID values after canonicalization.
	SQLiteCollation = "PALPLAYERUID"
	// SQLiteFunction exposes canonicalization to migrations and maintenance SQL.
	SQLiteFunction = "pal_uid_canonical"
)

func init() {
	sqlite.MustRegisterCollationUtf8(SQLiteCollation, Compare)
	if err := sqlite.RegisterDeterministicScalarFunction(SQLiteFunction, 1, func(_ *sqlite.FunctionContext, values []driver.Value) (driver.Value, error) {
		if len(values) == 0 || values[0] == nil {
			return "", nil
		}
		text, ok := values[0].(string)
		if !ok {
			return Normalize(strings.TrimSpace(toString(values[0]))), nil
		}
		return Normalize(text), nil
	}); err != nil {
		panic(err)
	}
}

// Normalize returns one stable identity key while preserving unknown identifier
// formats. Palworld GUID-like values are stored as 32 uppercase hexadecimal
// characters; known platform IDs are stored in lowercase.
func Normalize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	compact := strings.NewReplacer("-", "", "{", "", "}", "").Replace(value)
	if len(compact) == 32 && isHex(compact) {
		return strings.ToUpper(compact)
	}

	lower := strings.ToLower(value)
	for _, prefix := range []string{"steam_", "gdk_", "ps5_", "xbox_", "eos_"} {
		if strings.HasPrefix(lower, prefix) {
			return lower
		}
	}
	return value
}

// NormalizeSteamID returns the Palworld/PalDefender Steam identifier format
// used by log lines. Numeric Steam64 values are prefixed with "steam_".
func NormalizeSteamID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "steam_") {
		return value
	}
	if len(value) >= 15 && len(value) <= 20 && isDecimal(value) {
		return "steam_" + value
	}
	return value
}

// Compare implements SQLite UTF-8 collation semantics.
func Compare(left, right string) int {
	left = Normalize(left)
	right = Normalize(right)
	return strings.Compare(left, right)
}

func Equivalent(left, right string) bool {
	left = Normalize(left)
	return left != "" && left == Normalize(right)
}

func isHex(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func isDecimal(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return value != ""
}

func toString(value driver.Value) string {
	if raw, ok := value.([]byte); ok {
		return string(raw)
	}
	return fmt.Sprint(value)
}
