package playeruid

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"regexp"
	"strings"
	"unicode/utf16"
)

type Mode string

const (
	ModeUnknown Mode = "unknown"
	ModeSteam   Mode = "steam"
	ModeNoSteam Mode = "nosteam"
)

type Pair struct {
	Steam   string `json:"steam_uid"`
	NoSteam string `json:"nosteam_uid"`
}

type Identity struct {
	SteamID   string `json:"steam_id"`
	PlayerUID string `json:"player_uid"`
}

type Detection struct {
	Mode    Mode `json:"mode"`
	Matched int  `json:"matched"`
	Total   int  `json:"total"`
}

var steamIDPattern = regexp.MustCompile(`^[0-9]{17}$`)

func Calculate(steamID string) (Pair, error) {
	steamID = strings.TrimSpace(steamID)
	if !steamIDPattern.MatchString(steamID) {
		return Pair{}, errors.New("SteamID64 must contain exactly 17 digits")
	}
	runes := utf16.Encode([]rune(steamID))
	payload := make([]byte, len(runes)*2)
	for index, value := range runes {
		binary.LittleEndian.PutUint16(payload[index*2:], value)
	}
	hash := cityHash64(payload)
	steam := uint32(hash) + uint32(hash>>32)*23
	nosteam := mixNoSteam(steam)
	return Pair{Steam: formatUID(steam), NoSteam: formatUID(nosteam)}, nil
}

func DetectMode(identities []Identity) (Detection, error) {
	result := Detection{Mode: ModeUnknown, Total: len(identities)}
	if len(identities) == 0 {
		return result, nil
	}
	steamMatches := 0
	noSteamMatches := 0
	for _, identity := range identities {
		pair, err := Calculate(identity.SteamID)
		if err != nil {
			return Detection{}, err
		}
		uid := normalizeUID(identity.PlayerUID)
		if uid == pair.Steam {
			steamMatches++
		}
		if uid == pair.NoSteam {
			noSteamMatches++
		}
	}
	if steamMatches == len(identities) && noSteamMatches == 0 {
		result.Mode = ModeSteam
		result.Matched = steamMatches
		return result, nil
	}
	if noSteamMatches == len(identities) && steamMatches == 0 {
		result.Mode = ModeNoSteam
		result.Matched = noSteamMatches
		return result, nil
	}
	if steamMatches > noSteamMatches {
		result.Matched = steamMatches
	} else {
		result.Matched = noSteamMatches
	}
	return result, nil
}

func UIDForMode(pair Pair, mode Mode) (string, error) {
	switch mode {
	case ModeSteam:
		return pair.Steam, nil
	case ModeNoSteam:
		return pair.NoSteam, nil
	default:
		return "", fmt.Errorf("unsupported UID mode %q", mode)
	}
}

func normalizeUID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) == 32 {
		return compact[:8] + "-" + compact[8:12] + "-" + compact[12:16] + "-" + compact[16:20] + "-" + compact[20:]
	}
	return value
}

func formatUID(value uint32) string {
	return fmt.Sprintf("%08x-0000-0000-0000-000000000000", value)
}

func mixNoSteam(steam uint32) uint32 {
	a := (steam << 8) ^ (uint32(2654435769) - steam)
	b := (a >> 13) ^ (uint32(0) - (steam + a))
	c := (b >> 12) ^ (steam - a - b)
	d := (c << 16) ^ (a - c - b)
	e := (d >> 5) ^ (b - d - c)
	f := (e >> 3) ^ (c - d - e)
	mixed := (f << 10) ^ (d - f - e)
	return (mixed >> 15) ^ (e - mixed - f)
}

func cityHash64(payload []byte) uint64 {
	if len(payload) < 33 || len(payload) > 64 {
		panic("playeruid: CityHash input outside the supported SteamID64 length")
	}
	const k2 uint64 = 0x9ae16a3b2f90404f
	mul := k2 + uint64(len(payload))*2
	a := fetch64(payload, 0) * k2
	b := fetch64(payload, 8)
	c := fetch64(payload, len(payload)-24)
	d := fetch64(payload, len(payload)-32)
	e := fetch64(payload, 16) * k2
	f := fetch64(payload, 24) * 9
	g := fetch64(payload, len(payload)-8)
	h := fetch64(payload, len(payload)-16) * mul
	u := rotateRight(a+g, 43) + (rotateRight(b, 30)+c)*9
	v := ((a + g) ^ d) + f + 1
	w := bits.ReverseBytes64((u+v)*mul) + h
	x := rotateRight(e+f, 42) + c
	y := (bits.ReverseBytes64((v+w)*mul) + g) * mul
	z := e + f + c
	a = bits.ReverseBytes64((x+z)*mul+y) + b
	b = shiftMix((z+a)*mul+d+h) * mul
	return b + x
}

func fetch64(payload []byte, offset int) uint64 {
	return binary.LittleEndian.Uint64(payload[offset : offset+8])
}

func rotateRight(value uint64, shift int) uint64 {
	return bits.RotateLeft64(value, -shift)
}

func shiftMix(value uint64) uint64 {
	return value ^ (value >> 47)
}
