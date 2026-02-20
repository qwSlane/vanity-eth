package cmd

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"vanity-eth/internal/generator"
)

// version is set at build time via -ldflags "-X vanity-eth/cmd.version=vX.Y.Z"
var version = "dev"

var (
	flagPrefix   string
	flagSuffix   string
	flagContains string
	flagRegex    string
	flagWorkers  int
	flagCount    int
	flagCase     bool
	flagTUI      bool
	flagOutput   string
	flagFormat   string
)

var (
	green   = color.New(color.FgGreen, color.Bold)
	yellow  = color.New(color.FgYellow, color.Bold)
	cyan    = color.New(color.FgCyan)
	red     = color.New(color.FgRed)
	bold    = color.New(color.Bold)
	magenta = color.New(color.FgMagenta, color.Bold)
)

const logoASCII = `
██╗   ██╗ █████╗ ███╗  ██╗██╗████████╗██╗   ██╗    ███████╗████████╗██╗  ██╗
██║   ██║██╔══██╗████╗ ██║██║╚══██╔══╝╚██╗ ██╔╝    ██╔════╝╚══██╔══╝██║  ██║
██║   ██║███████║██╔██╗██║██║   ██║    ╚████╔╝     █████╗     ██║   ███████║
╚██╗ ██╔╝██╔══██║██║╚████║██║   ██║     ╚██╔╝      ██╔══╝     ██║   ██╔══██║
 ╚████╔╝ ██║  ██║██║ ╚███║██║   ██║      ██║       ███████╗   ██║   ██║  ██║
  ╚═══╝  ╚═╝  ╚═╝╚═╝  ╚══╝╚═╝   ╚═╝      ╚═╝       ╚══════╝   ╚═╝   ╚═╝  ╚═╝
`

var rootCmd = &cobra.Command{
	Use:     "vanity-eth",
	Version: version,
	Short:   "Vanity Ethereum address generator",
	Long: `vanity-eth generates Ethereum addresses matching desired patterns.
Uses all CPU cores by default for maximum throughput.

Examples:
  vanity-eth --prefix dead
  vanity-eth --suffix beef
  vanity-eth --contains cafe --count 3
  vanity-eth --regex "^0x(dead|cafe)"
  vanity-eth              (launch interactive TUI)`,
	RunE: runRoot,
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&flagPrefix, "prefix", "p", "", "address must start with this hex string (after 0x)")
	rootCmd.Flags().StringVarP(&flagSuffix, "suffix", "s", "", "address must end with this hex string")
	rootCmd.Flags().StringVarP(&flagContains, "contains", "c", "", "address must contain this hex string")
	rootCmd.Flags().StringVarP(&flagRegex, "regex", "r", "", "address must match this regex (applied to full 0x… address)")
	rootCmd.Flags().IntVarP(&flagWorkers, "workers", "w", runtime.NumCPU(), "number of parallel workers")
	rootCmd.Flags().IntVarP(&flagCount, "count", "n", 1, "how many matching addresses to find")
	rootCmd.Flags().BoolVar(&flagCase, "case-sensitive", false, "case-sensitive matching (checksummed address)")
	rootCmd.Flags().BoolVar(&flagTUI, "tui", false, "launch interactive TUI (default when no pattern is given)")
	rootCmd.Flags().StringVarP(&flagOutput, "output", "o", "", "save results to this file")
	rootCmd.Flags().StringVar(&flagFormat, "format", "text", "output format: text or json")
}

func runRoot(cmd *cobra.Command, args []string) error {
	noPattern := flagPrefix == "" && flagSuffix == "" && flagContains == "" && flagRegex == ""
	if flagTUI || noPattern {
		return runTUI()
	}
	return runCLI(cmd)
}

func runCLI(cmd *cobra.Command) error {
	// Validate hex inputs.
	for flag, val := range map[string]string{"prefix": flagPrefix, "suffix": flagSuffix, "contains": flagContains} {
		if val != "" {
			if err := generator.ValidateHexPattern(val); err != nil {
				return fmt.Errorf("--%s: %v", flag, err)
			}
		}
	}

	if flagRegex != "" {
		if _, err := regexp.Compile(flagRegex); err != nil {
			return fmt.Errorf("invalid regex: %w", err)
		}
	}

	if flagFormat != "text" && flagFormat != "json" {
		return fmt.Errorf("--format must be text or json")
	}

	cfg := generator.Config{
		Prefix:        flagPrefix,
		Suffix:        flagSuffix,
		Contains:      flagContains,
		Regex:         flagRegex,
		Workers:       flagWorkers,
		Count:         flagCount,
		CaseSensitive: flagCase,
	}

	magenta.Print(logoASCII)
	bold.Printf("vanity-eth  •  workers: %d  •  target: %d address(es)\n", flagWorkers, flagCount)
	printPattern(flagPrefix, flagSuffix, flagContains, flagRegex, flagCase)
	fmt.Println()

	ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	stats := &generator.Stats{}
	resultCh := make(chan generator.Result, flagCount)

	go generator.Run(ctx, cfg, resultCh, stats)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	start := time.Now()

	var collected []generator.Result

loop:
	for {
		select {
		case r, ok := <-resultCh:
			if !ok {
				break loop
			}
			collected = append(collected, r)
			if flagFormat == "text" {
				printResult(len(collected), r, stats.Total.Load(), time.Since(start))
			}
		case <-ticker.C:
			if flagFormat == "text" {
				printProgress(stats.Total.Load(), int(stats.Found.Load()), flagCount, time.Since(start), cfg)
			}
		case <-ctx.Done():
			ticker.Stop()
			for r := range resultCh {
				collected = append(collected, r)
				if flagFormat == "text" {
					printResult(len(collected), r, stats.Total.Load(), time.Since(start))
				}
			}
			break loop
		}
	}

	elapsed := time.Since(start)
	total := stats.Total.Load()
	rate := float64(total) / elapsed.Seconds()

	if flagFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		type jsonResult struct {
			Address    string `json:"address"`
			PrivateKey string `json:"privateKey"`
		}
		out := make([]jsonResult, len(collected))
		for i, r := range collected {
			out[i] = jsonResult{Address: r.Address, PrivateKey: "0x" + r.PrivateKey}
		}
		_ = enc.Encode(out)
	} else {
		fmt.Printf("\n%s  found %d/%d  •  %s tried  •  %.0f addr/s  •  %s\n",
			bold.Sprint("done"),
			len(collected), flagCount,
			formatBig(total),
			rate,
			elapsed.Round(time.Millisecond),
		)
	}

	if flagOutput != "" {
		if err := saveToFile(flagOutput, collected); err != nil {
			fmt.Fprintf(os.Stderr, "error saving file: %v\n", err)
		} else {
			green.Printf("saved to %s\n", flagOutput)
		}
	}

	return nil
}

func saveToFile(path string, results []generator.Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for i, r := range results {
		fmt.Fprintf(f, "#%d\n", i+1)
		fmt.Fprintf(f, "Address:     %s\n", r.Address)
		fmt.Fprintf(f, "Private Key: 0x%s\n\n", r.PrivateKey)
	}
	return nil
}

func printPattern(prefix, suffix, contains, regex string, caseSensitive bool) {
	var parts []string
	if prefix != "" {
		parts = append(parts, fmt.Sprintf("prefix=%q", prefix))
	}
	if suffix != "" {
		parts = append(parts, fmt.Sprintf("suffix=%q", suffix))
	}
	if contains != "" {
		parts = append(parts, fmt.Sprintf("contains=%q", contains))
	}
	if regex != "" {
		parts = append(parts, fmt.Sprintf("regex=%q", regex))
	}
	yellow.Printf("pattern: %s\n", strings.Join(parts, "  "))

	if d := generator.HexDifficulty(prefix, suffix, contains, caseSensitive); d != nil {
		cyan.Printf("~1 in %s addresses match\n", d.String())
		cyan.Printf("ETA will appear once the search starts\n")
	}
}

func printProgress(total int64, found, count int, elapsed time.Duration, cfg generator.Config) {
	rate := float64(total) / elapsed.Seconds()
	eta := computeETA(cfg, found, count, rate)
	etaStr := ""
	if eta > 0 {
		etaStr = "  •  ETA " + fmtDuration(eta)
	}
	fmt.Printf("\r\033[K%s tried  •  %d/%d found  •  %.0f addr/s  •  %s%s",
		formatBig(total), found, count, rate, elapsed.Round(time.Second), etaStr)
}

// computeETA estimates remaining time using the current live rate and difficulty.
func computeETA(cfg generator.Config, found, count int, ratePerSec float64) time.Duration {
	if ratePerSec <= 0 {
		return 0
	}
	d := generator.HexDifficulty(cfg.Prefix, cfg.Suffix, cfg.Contains, cfg.CaseSensitive)
	if d == nil {
		return 0 // regex patterns: can't estimate
	}
	remaining := count - found
	if remaining <= 0 {
		return 0
	}
	// expected_attempts = remaining * difficulty
	expected := new(big.Float).SetInt(d)
	expected.Mul(expected, big.NewFloat(float64(remaining)))
	secs, _ := new(big.Float).Quo(expected, big.NewFloat(ratePerSec)).Float64()
	return time.Duration(secs * float64(time.Second))
}

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	days := h / 24
	h = h % 24
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %02d:%02d:%02d", days, h, m, s)
	}
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func printResult(n int, r generator.Result, total int64, elapsed time.Duration) {
	rate := float64(total) / elapsed.Seconds()
	fmt.Printf("\r\033[K")
	fmt.Printf("\n%s  #%d found after %s (%.0f addr/s)\n",
		green.Sprint("✓"), n, formatBig(total), rate)
	bold.Printf("  Address:     ")
	highlightAddress(r.Address)
	fmt.Println()
	bold.Printf("  Private key: ")
	red.Printf("0x%s\n", r.PrivateKey)
	fmt.Println()
}

func highlightAddress(addr string) {
	bare := addr[2:] // strip 0x
	fmt.Print("0x")
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
	if n < 1_000 {
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
