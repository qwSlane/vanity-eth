package generator

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestValidateHexPattern_GroupedAlternation(t *testing.T) {
	p := "x(a|b|c)(10|20|30|40|50)"
	if err := ValidateHexPattern(p); err != nil {
		t.Fatalf("expected pattern to be valid, got error: %v", err)
	}
}

func TestBuildMatcher_GroupedPrefix(t *testing.T) {
	matcher := BuildMatcher("x(a|b|c)(10|20|30|40|50)", "", "", nil, false)

	if !matcher("0xa10aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatalf("expected grouped prefix pattern to match")
	}
	if matcher("0xabbaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatalf("expected grouped prefix pattern not to match")
	}
}

func TestBuildMatcher_LegacyAlternationStillWorks(t *testing.T) {
	matcher := BuildMatcher("e|f|ff", "", "", nil, false)

	if !matcher("0xffaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatalf("expected legacy alternation to match")
	}
	if matcher("0x0aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatalf("expected legacy alternation not to match")
	}
}

func TestMinHexPatternLen(t *testing.T) {
	if got := MinHexPatternLen("x(a|b|c)(10|20|30|40|50)"); got != 3 {
		t.Fatalf("expected min length 3, got %d", got)
	}
}

func TestHexDifficulty_CaseSensitiveIsHarder(t *testing.T) {
	ci := HexDifficulty("eee", "", "", false)
	cs := HexDifficulty("eee", "", "", true)
	if ci == nil || cs == nil {
		t.Fatalf("difficulty should not be nil")
	}
	if cs.Cmp(ci) <= 0 {
		t.Fatalf("expected case-sensitive difficulty to be greater: ci=%s cs=%s", ci.String(), cs.String())
	}
}

func TestHexDifficulty_GroupedPrefixAndSuffix(t *testing.T) {
	prefix := "(a|b|c)(10|20|30|40|50)"
	suffix := "c0ffee"

	ci := HexDifficulty(prefix, suffix, "", false)
	cs := HexDifficulty(prefix, suffix, "", true)
	if ci == nil || cs == nil {
		t.Fatalf("difficulty should not be nil")
	}

	if got, want := ci.String(), "4581298449"; got != want {
		t.Fatalf("case-insensitive difficulty mismatch: got %s want %s", got, want)
	}
	if got, want := cs.String(), "293203100740"; got != want {
		t.Fatalf("case-sensitive difficulty mismatch: got %s want %s", got, want)
	}
}

func TestAddressFromKey_RespectsCaseMode(t *testing.T) {
	key, err := crypto.HexToECDSA("4c0883a69102937d6231471b5dbb6204fe5129617082799f7ed2a5abf85f7f4f")
	if err != nil {
		t.Fatalf("failed to parse key: %v", err)
	}

	cs := addressFromKey(key, true)
	ci := addressFromKey(key, false)
	wantCS := crypto.PubkeyToAddress(key.PublicKey).Hex()

	if cs != wantCS {
		t.Fatalf("case-sensitive address mismatch: got %q want %q", cs, wantCS)
	}
	if ci != strings.ToLower(wantCS) {
		t.Fatalf("case-insensitive address mismatch: got %q want %q", ci, strings.ToLower(wantCS))
	}
}
