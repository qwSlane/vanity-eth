package generator

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/crypto"
)

// Config holds all search parameters.
type Config struct {
	Prefix        string
	Suffix        string
	Contains      string
	Regex         string
	Workers       int
	Count         int
	CaseSensitive bool
}

// Result holds a found address and its private key.
type Result struct {
	Address    string
	PrivateKey string
}

// Stats holds live counters updated atomically during a search.
type Stats struct {
	Total atomic.Int64
	Found atomic.Int64
}

// HexDifficulty returns the expected number of attempts to find a single match
// for the combined hex pattern complexity (prefix + suffix + contains).
// When caseSensitive is true, letter case in a-f is treated as fixed.
// Returns nil if all patterns are empty.
func HexDifficulty(prefix, suffix, contains string, caseSensitive bool) *big.Int {
	var active bool
	totalP := big.NewRat(1, 1)

	if p := edgePatternProbability(prefix, true, caseSensitive); p != nil {
		totalP.Mul(totalP, p)
		active = true
	}
	if p := edgePatternProbability(suffix, false, caseSensitive); p != nil {
		totalP.Mul(totalP, p)
		active = true
	}
	if p := containsPatternProbabilityApprox(contains, caseSensitive); p != nil {
		totalP.Mul(totalP, p)
		active = true
	}

	if !active || totalP.Sign() == 0 {
		return nil
	}

	// expected attempts ~= 1 / probability
	num := new(big.Int).Set(totalP.Num())
	den := new(big.Int).Set(totalP.Denom())
	if num.Sign() == 0 {
		return nil
	}
	d := new(big.Int).Quo(den, num)
	if d.Sign() == 0 {
		return big.NewInt(1)
	}
	return d
}

// IsValidHexPattern returns true if s is a valid hex pattern,
// optionally with | for alternation (e.g. "dead|cafe").
func IsValidHexPattern(s string) bool {
	_, err := compileHexPattern(s)
	return err == nil
}

// ValidateHexPattern validates prefix/suffix/contains pattern syntax.
func ValidateHexPattern(s string) error {
	_, err := compileHexPattern(s)
	return err
}

// MinHexPatternLen returns the shortest effective hex length in pattern.
// Returns 0 for empty or invalid patterns.
func MinHexPatternLen(pattern string) int {
	minLen, _ := minPatternLenAndLetters(pattern)
	return minLen
}

// matchAlt returns true if check(haystack, alt) is true for any alternative.
func matchAlt(haystack string, alts []string, check func(string, string) bool) bool {
	for _, alt := range alts {
		if check(haystack, alt) {
			return true
		}
	}
	return false
}

// BuildMatcher returns a match function for the given criteria.
func BuildMatcher(prefix, suffix, contains string, re *regexp.Regexp, caseSensitive bool) func(string) bool {
	normalize := func(s string) string {
		if caseSensitive {
			return s
		}
		return strings.ToLower(s)
	}
	prefix = normalize(prefix)
	suffix = normalize(suffix)
	contains = normalize(contains)
	prefixAlts, _ := compileHexPattern(prefix)
	suffixAlts, _ := compileHexPattern(suffix)
	containsAlts, _ := compileHexPattern(contains)

	return func(addr string) bool {
		a := normalize(addr)
		bare := strings.TrimPrefix(a, "0x")

		if len(prefixAlts) > 0 && !matchAlt(bare, prefixAlts, strings.HasPrefix) {
			return false
		}
		if len(suffixAlts) > 0 && !matchAlt(bare, suffixAlts, strings.HasSuffix) {
			return false
		}
		if len(containsAlts) > 0 && !matchAlt(bare, containsAlts, strings.Contains) {
			return false
		}
		if re != nil && !re.MatchString(addr) {
			return false
		}
		return true
	}
}

// Run starts a worker pool that searches for addresses matching cfg.
// Results are sent to resultCh (buffered with cfg.Count capacity).
// Stats are updated atomically throughout. resultCh is closed when all
// workers exit (either context cancelled or count reached).
func Run(ctx context.Context, cfg Config, resultCh chan<- Result, stats *Stats) {
	var re *regexp.Regexp
	if cfg.Regex != "" {
		re, _ = regexp.Compile(cfg.Regex)
	}
	matcher := BuildMatcher(cfg.Prefix, cfg.Suffix, cfg.Contains, re, cfg.CaseSensitive)

	var wg sync.WaitGroup
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if int(stats.Found.Load()) >= cfg.Count {
					return
				}

				key, err := crypto.GenerateKey()
				if err != nil {
					continue
				}
				stats.Total.Add(1)

				addr := addressFromKey(key, cfg.CaseSensitive)
				if matcher(addr) {
					n := stats.Found.Add(1)
					if int(n) <= cfg.Count {
						select {
						case resultCh <- Result{
							Address:    addr,
							PrivateKey: privateKeyHex(key),
						}:
						case <-ctx.Done():
							return
						}
					}
				}
			}
		}()
	}

	wg.Wait()
	close(resultCh)
}

func addressFromKey(key *ecdsa.PrivateKey, caseSensitive bool) string {
	addr := crypto.PubkeyToAddress(key.PublicKey)
	if caseSensitive {
		return addr.Hex()
	}
	return strings.ToLower(addr.Hex())
}

func privateKeyHex(key *ecdsa.PrivateKey) string {
	return hex.EncodeToString(crypto.FromECDSA(key))
}

func compileHexPattern(pattern string) ([]string, error) {
	s := strings.TrimSpace(pattern)
	if s == "" {
		return nil, nil
	}
	if len(s) >= 2 && (s[0] == '0') && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	} else if len(s) >= 1 && (s[0] == 'x' || s[0] == 'X') {
		s = s[1:]
	}
	if s == "" {
		return nil, fmt.Errorf("pattern is empty")
	}

	branches, err := splitTopLevel(s)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(branches))
	var all []string
	for _, branch := range branches {
		expanded, err := expandBranch(branch)
		if err != nil {
			return nil, err
		}
		for _, alt := range expanded {
			if _, ok := seen[alt]; ok {
				continue
			}
			seen[alt] = struct{}{}
			all = append(all, alt)
		}
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("pattern is empty")
	}
	return all, nil
}

func splitTopLevel(s string) ([]string, error) {
	var parts []string
	start := 0
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unexpected ')'")
			}
		case '|':
			if depth == 0 {
				part := s[start:i]
				if part == "" {
					return nil, fmt.Errorf("empty alternative near '|'")
				}
				parts = append(parts, part)
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("unclosed '('")
	}
	last := s[start:]
	if last == "" {
		return nil, fmt.Errorf("empty alternative near '|'")
	}
	parts = append(parts, last)
	return parts, nil
}

func expandBranch(branch string) ([]string, error) {
	alts := []string{""}
	for i := 0; i < len(branch); {
		switch c := branch[i]; {
		case isHex(c):
			j := i + 1
			for j < len(branch) && isHex(branch[j]) {
				j++
			}
			alts = appendSegment(alts, []string{branch[i:j]})
			i = j
		case c == '(':
			end, err := findGroupEnd(branch, i)
			if err != nil {
				return nil, err
			}
			inner := branch[i+1 : end]
			if inner == "" {
				return nil, fmt.Errorf("empty group '()'")
			}
			groupAlts, err := splitTopLevel(inner)
			if err != nil {
				return nil, err
			}
			for _, ga := range groupAlts {
				for j := 0; j < len(ga); j++ {
					if !isHex(ga[j]) {
						return nil, fmt.Errorf("invalid character %q in group", ga[j])
					}
				}
			}
			alts = appendSegment(alts, groupAlts)
			i = end + 1
		case c == ')':
			return nil, fmt.Errorf("unexpected ')'")
		case c == '|':
			return nil, fmt.Errorf("unexpected '|'")
		default:
			return nil, fmt.Errorf("invalid character %q (allowed: 0-9, a-f, |, (, ), optional x/0x prefix)", c)
		}
	}
	return alts, nil
}

func findGroupEnd(s string, start int) (int, error) {
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return -1, fmt.Errorf("unclosed '('")
}

func appendSegment(prefixes []string, segment []string) []string {
	out := make([]string, 0, len(prefixes)*len(segment))
	for _, p := range prefixes {
		for _, s := range segment {
			out = append(out, p+s)
		}
	}
	return out
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func minPatternLenAndLetters(pattern string) (int, int) {
	alts, err := compileHexPattern(pattern)
	if err != nil || len(alts) == 0 {
		return 0, 0
	}
	minLen := len(alts[0])
	minLetters := countHexLetters(alts[0])
	for _, alt := range alts[1:] {
		l := len(alt)
		letters := countHexLetters(alt)
		if l < minLen || (l == minLen && letters < minLetters) {
			minLen = l
			minLetters = letters
		}
	}
	return minLen, minLetters
}

func countHexLetters(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			n++
		}
	}
	return n
}

func edgePatternProbability(pattern string, isPrefix, caseSensitive bool) *big.Rat {
	if strings.TrimSpace(pattern) == "" {
		return nil
	}
	p := pattern
	if !caseSensitive {
		p = strings.ToLower(p)
	}
	alts, err := compileHexPattern(p)
	if err != nil || len(alts) == 0 {
		return nil
	}

	reduced := make([]string, 0, len(alts))
	for _, a := range alts {
		redundant := false
		for _, b := range alts {
			if a == b || len(b) >= len(a) {
				continue
			}
			if isPrefix {
				if strings.HasPrefix(a, b) {
					redundant = true
					break
				}
			} else {
				if strings.HasSuffix(a, b) {
					redundant = true
					break
				}
			}
		}
		if !redundant {
			reduced = append(reduced, a)
		}
	}
	slices.Sort(reduced)

	sum := new(big.Rat)
	for _, alt := range reduced {
		den := patternDenominator(len(alt), countHexLetters(alt), caseSensitive)
		sum.Add(sum, new(big.Rat).SetFrac(big.NewInt(1), den))
	}
	return sum
}

func containsPatternProbabilityApprox(pattern string, caseSensitive bool) *big.Rat {
	if strings.TrimSpace(pattern) == "" {
		return nil
	}
	p := pattern
	if !caseSensitive {
		p = strings.ToLower(p)
	}
	minLen, minLetters := minPatternLenAndLetters(p)
	if minLen == 0 {
		return nil
	}
	den := patternDenominator(minLen, minLetters, caseSensitive)
	return new(big.Rat).SetFrac(big.NewInt(1), den)
}

func patternDenominator(hexLen, letters int, caseSensitive bool) *big.Int {
	den := new(big.Int).Exp(big.NewInt(16), big.NewInt(int64(hexLen)), nil)
	if caseSensitive && letters > 0 {
		den.Mul(den, new(big.Int).Exp(big.NewInt(2), big.NewInt(int64(letters)), nil))
	}
	return den
}
