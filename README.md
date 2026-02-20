# vanity-eth

```
 __   __  ___  __  _  _  ____  ____  _  _       ____  ____  _  _
 \ \ / / / _ \ | \| || ||_  _||_  _|| || |     | ___||_  _|| || |
  \ V / | (_) || .` || |  | |   | | | __ |  _  | _|    | |  | __ |
   \_/   \__\_\|_|\_||_| _|_|  _|_| |_||_| |_| |___|  _|_| |_||_|
```

Generate Ethereum wallet addresses matching any pattern you choose.

[![Go version](https://img.shields.io/badge/go-1.22+-blue)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-linux%20%7C%20macOS%20%7C%20windows-lightgrey)]()

---

## Features

- **Prefix / suffix / substring / regex** matching
- **Interactive TUI** (bubbletea) with live stats and ETA
- **CLI mode** for scripting and automation (`--format json`, `--output file`)
- Multi-core by default — ~300 K addr/s on Apple Silicon M-series
- Real-time ETA based on measured throughput and pattern difficulty
- Safe: keys are generated locally, never sent anywhere

---

## Install

```bash
# From source (requires Go 1.22+)
go install github.com/svasileuski/vanity-eth@latest

# Or build manually
git clone https://github.com/svasileuski/vanity-eth
cd vanity-eth
make build          # produces ./vanity-eth
make install        # installs to $GOPATH/bin
```

---

## Quick start

### Interactive TUI (no flags needed)

```bash
vanity-eth
```

Use **Tab** to navigate fields, **←→** to change pattern type, **Enter** to start, **s** to save results.

### CLI

```bash
# Find an address starting with "dead"
vanity-eth --prefix dead

# Find 3 addresses ending with "cafe"
vanity-eth --suffix cafe --count 3

# Substring match, save to file
vanity-eth --contains beef --output results.txt

# Regex match
vanity-eth --regex "^0x(dead|cafe)"

# JSON output
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

## Security

Private keys are generated **entirely locally** using Go's `crypto/rand` and the `secp256k1` curve via `go-ethereum/crypto`. Nothing is transmitted over the network. Treat generated private keys with the same care as any wallet key — do not share them.

---

## License

MIT © 2025 [Siarhei Vasileuski](mailto:siarhei.vasileuskij@gmail.com)
