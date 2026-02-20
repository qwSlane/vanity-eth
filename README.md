# vanity-eth

```
██╗   ██╗ █████╗ ███╗  ██╗██╗████████╗██╗   ██╗    ███████╗████████╗██╗  ██╗
██║   ██║██╔══██╗████╗ ██║██║╚══██╔══╝╚██╗ ██╔╝    ██╔════╝╚══██╔══╝██║  ██║
██║   ██║███████║██╔██╗██║██║   ██║    ╚████╔╝     █████╗     ██║   ███████║
╚██╗ ██╔╝██╔══██║██║╚████║██║   ██║     ╚██╔╝      ██╔══╝     ██║   ██╔══██║
 ╚████╔╝ ██║  ██║██║ ╚███║██║   ██║      ██║       ███████╗   ██║   ██║  ██║
  ╚═══╝  ╚═╝  ╚═╝╚═╝  ╚══╝╚═╝   ╚═╝      ╚═╝       ╚══════╝   ╚═╝   ╚═╝  ╚═╝
```

```ansi
[35m╭──────────────────────────────────────────────────────────╮[0m
[35m│[0m                                                          [35m│[0m
[35m│[0m   [1;35mvanity-eth[0m                                             [35m│[0m
[35m│[0m   [90mGenerate Ethereum vanity addresses[0m                     [35m│[0m
[35m│[0m                                                          [35m│[0m
[35m│[0m   [90mPrefix  [0m     [96m>[0m e|f|ff                                  [35m│[0m
[35m│[0m   [90mSuffix  [0m     [96m>[0m cafe                                    [35m│[0m
[35m│[0m   [90mContains[0m     [96m>[0m beef                                    [35m│[0m
[35m│[0m                                                          [35m│[0m
[35m│[0m   [90mCount   [0m     [96m>[0m 1                                       [35m│[0m
[35m│[0m   [90mWorkers [0m     [96m>[0m 14                                      [35m│[0m
[35m│[0m   [90mCase    [0m     [90m[ ] sensitive[0m                             [35m│[0m
[35m│[0m                                                          [35m│[0m
[35m│[0m   [90mPreview[0m                                               [35m│[0m
[35m│[0m    0x[92m(e|f|ff)[0m[90m???????????????[0m[96mbeef[0m[90m????????????????[0m[92mcafe[0m      [35m│[0m
[35m│[0m                                                          [35m│[0m
[35m│[0m   [90mtab navigate  space toggle case  enter start  esc/ctrl+c[0m [35m│[0m
[35m│[0m   [90mquit[0m                                                   [35m│[0m
[35m│[0m                                                          [35m│[0m
[35m╰──────────────────────────────────────────────────────────╯[0m
```

Generate Ethereum wallet addresses matching any pattern you choose.

[![Release](https://img.shields.io/github/v/release/qwSlane/vanity-eth?style=flat-square)](https://github.com/qwSlane/vanity-eth/releases/latest)
[![CI](https://img.shields.io/github/actions/workflow/status/qwSlane/vanity-eth/release.yml?style=flat-square&label=build)](https://github.com/qwSlane/vanity-eth/actions)
[![Go version](https://img.shields.io/badge/go-1.22+-blue?style=flat-square)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green?style=flat-square)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-linux%20%7C%20macOS%20%7C%20windows-lightgrey?style=flat-square)]()

---

## Features

- **Prefix / suffix / substring** matching — combine them freely
- **Interactive TUI** with live stats, ETA, and address preview
- **CLI mode** for scripting (`--format json`, `--output file`)
- Multi-core by default — ~300 K addr/s on Apple Silicon M-series
- Real-time ETA based on measured throughput and pattern difficulty
- Safe: keys are generated locally, never sent anywhere

---

## Download

Pre-built binaries are attached to every [GitHub Release](https://github.com/qwSlane/vanity-eth/releases/latest).

### macOS (Apple Silicon — M1/M2/M3/M4)
```bash
curl -L https://github.com/qwSlane/vanity-eth/releases/latest/download/vanity-eth-darwin-arm64 -o vanity-eth
chmod +x vanity-eth
# First run: macOS may block unsigned binaries — remove the quarantine flag:
xattr -cr vanity-eth
./vanity-eth
```

### macOS (Intel)
```bash
curl -L https://github.com/qwSlane/vanity-eth/releases/latest/download/vanity-eth-darwin-amd64 -o vanity-eth
chmod +x vanity-eth && xattr -cr vanity-eth && ./vanity-eth
```

### Linux
```bash
curl -L https://github.com/qwSlane/vanity-eth/releases/latest/download/vanity-eth-linux-amd64 -o vanity-eth
chmod +x vanity-eth && ./vanity-eth
```

### Windows
Download `vanity-eth-windows-amd64.exe` from the [Releases page](https://github.com/qwSlane/vanity-eth/releases/latest) and run it in a terminal.

---

## Build from source

```bash
# Requires Go 1.22+
git clone https://github.com/qwSlane/vanity-eth
cd vanity-eth
make build          # → ./vanity-eth
make install        # → $GOPATH/bin/vanity-eth
```

---

## Quick start

### Interactive TUI (no flags needed)

```bash
vanity-eth
```

Fill in the pattern fields, press **Enter** to start searching.
Use **Tab** to navigate, **Space** to toggle case-sensitive mode, **s** to save results.

### CLI

```bash
# Find an address starting with "dead"
vanity-eth --prefix dead

# Find 3 addresses ending with "cafe"
vanity-eth --suffix cafe --count 3

# Combine prefix + suffix
vanity-eth --prefix dead --suffix cafe

# Substring match, save to file
vanity-eth --contains beef --output results.txt

# Regex match
vanity-eth --regex "^0x(dead|cafe)"

# JSON output (for scripting)
vanity-eth --prefix 00 --format json
```

---

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--prefix` | `-p` | — | Address must start with this hex string (after `0x`) |
| `--suffix` | `-s` | — | Address must end with this hex string |
| `--contains` | `-c` | — | Address must contain this hex string |
| `--regex` | `-r` | — | Full regex applied to the `0x…` address |
| `--count` | `-n` | `1` | Number of matching addresses to find |
| `--workers` | `-w` | `NumCPU` | Parallel worker goroutines |
| `--case-sensitive` | — | `false` | Match checksummed (mixed-case) address |
| `--output` | `-o` | — | Save results to this file |
| `--format` | — | `text` | Output format: `text` or `json` |
| `--tui` | — | — | Force TUI mode |
| `--version` | — | — | Print version and exit |

---

## Difficulty & ETA

| Pattern length | ~1 in N | Expected time (300 K/s) |
|---------------|---------|------------------------|
| 1 hex char    | 16      | < 1 ms                 |
| 2 hex chars   | 256     | < 1 ms                 |
| 4 hex chars   | 65 536  | ~0.2 s                 |
| 6 hex chars   | 16.7 M  | ~56 s                  |
| 8 hex chars   | 4.3 B   | ~4 h                   |

ETA is shown live during search and adjusts to your actual throughput.

---

## Release a new version

```bash
make tag VERSION=v1.0.0   # creates and pushes the git tag
```

GitHub Actions then automatically builds all platform binaries and attaches them to the release.

---

## Security

Private keys are generated **entirely locally** using Go's `crypto/rand` and the `secp256k1` curve via `go-ethereum/crypto`. Nothing is transmitted over the network. Treat generated private keys with the same care as any wallet key — do not share them.

---

## License

MIT © 2025 [Siarhei Vasileuski](mailto:siarhei.vasileuskij@gmail.com)
