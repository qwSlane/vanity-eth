package main

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	flagPrefix  string
	flagSuffix  string
	flagContains string
	flagRegex   string
	flagWorkers int
	flagCount   int
	flagCase    bool
)

type Result struct {
	Address    string
	PrivateKey string
}

var (
	green  = color.New(color.FgGreen, color.Bold)
	yellow = color.New(color.FgYellow, color.Bold)
	cyan   = color.New(color.FgCyan)
	red    = color.New(color.FgRed)
	bold   = color.New(color.Bold)
)

func main() {
	root := &cobra.Command{
		Use:   "vanity-eth",
		Short: "Vanity Ethereum address generator",
		Long: `vanity-eth generates Ethereum addresses matching desired patterns.
Uses all CPU cores by default for maximum throughput.

Examples:
  vanity-eth --prefix dead
  vanity-eth --suffix beef
  vanity-eth --contains cafe --count 3
  vanity-eth --regex "^0x(dead|cafe)"`,
		RunE: run,
	}

	root.Flags().StringVarP(&flagPrefix, "prefix", "p", "", "address must start with this hex string (after 0x)")
	root.Flags().StringVarP(&flagSuffix, "suffix", "s", "", "address must end with this hex string")
	root.Flags().StringVarP(&flagContains, "contains", "c", "", "address must contain this hex string")
	root.Flags().StringVarP(&flagRegex, "regex", "r", "", "address must match this regex (applied to full 0x... address)")
	root.Flags().IntVarP(&flagWorkers, "workers", "w", runtime.NumCPU(), "number of parallel workers")
	root.Flags().IntVarP(&flagCount, "count", "n", 1, "how many matching addresses to find")
	root.Flags().BoolVar(&flagCase, "case-sensitive", false, "case-sensitive matching (checksummed address)")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	if flagPrefix == "" && flagSuffix == "" && flagContains == "" && flagRegex == "" {
		return fmt.Errorf("provide at least one of: --prefix, --suffix, --contains, --regex")
	}

	// Validate hex inputs (any number of hex chars is valid)
	for flag, val := range map[string]string{"prefix": flagPrefix, "suffix": flagSuffix, "contains": flagContains} {
		if val != "" {
			if !isHex(val) {
				return fmt.Errorf("--%s must be a valid hex string (got %q)", flag, val)
			}
		}
	}

	var re *regexp.Regexp
	if flagRegex != "" {
		var err error
		re, err = regexp.Compile(flagRegex)
		if err != nil {
			return fmt.Errorf("invalid regex: %w", err)
		}
	}

	matcher := buildMatcher(flagPrefix, flagSuffix, flagContains, re, flagCase)

	bold.Printf("\nvanity-eth  •  workers: %d  •  target: %d address(es)\n", flagWorkers, flagCount)
	printPattern()
	fmt.Println()

	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	results := make(chan Result, flagCount)
	var found atomic.Int64
	var total atomic.Int64

	var wg sync.WaitGroup
	for i := 0; i < flagWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if int(found.Load()) >= flagCount {
					return
				}

				key, err := crypto.GenerateKey()
				if err != nil {
					continue
				}
				total.Add(1)

				addr := addressFromKey(key)
				if matcher(addr) {
					n := found.Add(1)
					if int(n) <= flagCount {
						results <- Result{
							Address:    addr,
							PrivateKey: privateKeyHex(key),
						}
					}
					if int(found.Load()) >= flagCount {
						cancel()
						return
					}
				}
			}
		}()
	}

	// Progress ticker
	ticker := time.NewTicker(3 * time.Second)
	start := time.Now()

	go func() {
		wg.Wait()
		close(results)
		ticker.Stop()
	}()

	collected := 0
	var printed []Result

loop:
	for {
		select {
		case r, ok := <-results:
			if !ok {
				break loop
			}
			collected++
			printed = append(printed, r)
			printResult(collected, r, total.Load(), time.Since(start))
		case <-ticker.C:
			printProgress(total.Load(), int(found.Load()), time.Since(start))
		case <-ctx.Done():
			// drain remaining results
			ticker.Stop()
			for r := range results {
				collected++
				printed = append(printed, r)
				printResult(collected, r, total.Load(), time.Since(start))
			}
			break loop
		}
	}

	elapsed := time.Since(start)
	rate := float64(total.Load()) / elapsed.Seconds()
	fmt.Printf("\n%s  found %d/%d  •  %s tried  •  %.0f addr/s  •  %s\n",
		bold.Sprint("done"),
		collected, flagCount,
		formatBig(total.Load()),
		rate,
		elapsed.Round(time.Millisecond),
	)

	return nil
}

// buildMatcher returns a function that checks whether an address string matches all criteria.
func isHex(s string) bool {
	for _, c := range strings.ToLower(s) {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return len(s) > 0
}

func buildMatcher(prefix, suffix, contains string, re *regexp.Regexp, caseSensitive bool) func(string) bool {
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
		// strip 0x for prefix/suffix/contains checks
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

func addressFromKey(key *ecdsa.PrivateKey) string {
	// go-ethereum crypto.PubkeyToAddress returns checksummed address
	addr := crypto.PubkeyToAddress(key.PublicKey)
	return strings.ToLower(addr.Hex())
}

func privateKeyHex(key *ecdsa.PrivateKey) string {
	return hex.EncodeToString(crypto.FromECDSA(key))
}

func printPattern() {
	parts := []string{}
	if flagPrefix != "" {
		parts = append(parts, fmt.Sprintf("prefix=%q", flagPrefix))
	}
	if flagSuffix != "" {
		parts = append(parts, fmt.Sprintf("suffix=%q", flagSuffix))
	}
	if flagContains != "" {
		parts = append(parts, fmt.Sprintf("contains=%q", flagContains))
	}
	if flagRegex != "" {
		parts = append(parts, fmt.Sprintf("regex=%q", flagRegex))
	}
	yellow.Printf("pattern: %s\n", strings.Join(parts, "  "))

	// estimate difficulty
	hexLen := len(flagPrefix) + len(flagSuffix) + len(flagContains)
	if hexLen > 0 {
		difficulty := new(big.Int).Exp(big.NewInt(16), big.NewInt(int64(hexLen)), nil)
		cyan.Printf("~1 in %s addresses match\n", formatBig(difficulty.Int64()))
	}
}

func printProgress(total int64, found int, elapsed time.Duration) {
	rate := float64(total) / elapsed.Seconds()
	fmt.Printf("\r\033[K%s tried  •  %d found  •  %.0f addr/s  •  %s",
		formatBig(total), found, rate, elapsed.Round(time.Second))
}

func printResult(n int, r Result, total int64, elapsed time.Duration) {
	rate := float64(total) / elapsed.Seconds()
	fmt.Printf("\r\033[K") // clear progress line

	fmt.Printf("\n%s  #%d found after %s (%.0f addr/s)\n",
		green.Sprint("✓"),
		n,
		formatBig(total),
		rate,
	)
	bold.Printf("  Address:     ")
	highlightAddress(r.Address)
	fmt.Println()
	bold.Printf("  Private key: ")
	red.Printf("0x%s\n", r.PrivateKey)
	fmt.Println()
}

func highlightAddress(addr string) {
	// addr is lowercase, e.g. 0xdeadbeef...
	bare := addr[2:] // strip 0x
	fmt.Print("0x")

	pattern := strings.ToLower(flagPrefix + flagSuffix + flagContains)
	_ = pattern

	prefixLen := len(flagPrefix)
	suffixLen := len(flagSuffix)
	addrLen := len(bare)

	for i, ch := range bare {
		inPrefix := prefixLen > 0 && i < prefixLen
		inSuffix := suffixLen > 0 && i >= addrLen-suffixLen

		if inPrefix || inSuffix {
			green.Printf("%c", ch)
		} else {
			fmt.Printf("%c", ch)
		}
	}
}

func formatBig(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	if n < 1_000_000_000 {
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	}
	return fmt.Sprintf("%.3fB", float64(n)/1e9)
}