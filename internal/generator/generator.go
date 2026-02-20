package generator

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"math/big"
	"regexp"
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
// for the combined hex pattern length (prefix + suffix + contains).
// Returns nil if hexLen == 0.
func HexDifficulty(prefix, suffix, contains string) *big.Int {
	hexLen := len(prefix) + len(suffix) + len(contains)
	if hexLen == 0 {
		return nil
	}
	return new(big.Int).Exp(big.NewInt(16), big.NewInt(int64(hexLen)), nil)
}

// IsValidHexPattern returns true if s is a non-empty valid hex string.
func IsValidHexPattern(s string) bool {
	for _, c := range strings.ToLower(s) {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return len(s) > 0
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

	return func(addr string) bool {
		a := normalize(addr)
		bare := strings.TrimPrefix(a, "0x")

		if prefix != "" && !strings.HasPrefix(bare, prefix) {
			return false
		}
		if suffix != "" && !strings.HasSuffix(bare, suffix) {
			return false
		}
		if contains != "" && !strings.Contains(bare, contains) {
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

				addr := addressFromKey(key)
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

func addressFromKey(key *ecdsa.PrivateKey) string {
	addr := crypto.PubkeyToAddress(key.PublicKey)
	return strings.ToLower(addr.Hex())
}

func privateKeyHex(key *ecdsa.PrivateKey) string {
	return hex.EncodeToString(crypto.FromECDSA(key))
}
