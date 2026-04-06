# x-chain-oracle

[![CI](https://github.com/vaporif/x-chain-oracle/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/vaporif/x-chain-oracle/actions/workflows/ci.yml)
[![CI](https://github.com/vaporif/x-chain-oracle/actions/workflows/e2e.yml/badge.svg?branch=main)](https://github.com/vaporif/x-chain-oracle/actions/workflows/e2e.yml)

Intent engine for tracking cross-chain operations with a configurable rule engine. Written in Golang because I have too much crab in my github.

## Installation

### Nix

```bash
# Build
nix build github:vaporif/x-chain-oracle

# Run directly
nix run github:vaporif/x-chain-oracle

# Install to profile
nix profile install github:vaporif/x-chain-oracle

# Development shell
nix develop
```

## Architecture

```
Adapters → Normalizer → Enricher → Engine → gRPC Emitter
```

The oracle watches multiple chains, chews through events, and spits out signals over gRPC.

- **Adapters** (`internal/adapter/`) - Plug into EVM, Solana, and Cosmos RPCs. Websocket or polling, with reconnection when things go sideways.
- **Normalizer** (`internal/normalizer/`) - Takes the chain-specific mess and flattens it into a common shape (token, amount, addresses).
- **Enricher** (`internal/enricher/`) - Bolts on contract metadata from a local registry and live token prices from Chainlink. Runs a worker pool so it doesn't block the pipeline.
- **Engine** (`internal/engine/`) - Where the rules live. Matches enriched events against conditions you define, correlates event sequences within time windows, and fires signals when something hits.
- **gRPC Emitter** (`internal/signal/grpc/`) - Streams signals out to whoever's listening. Clients can filter by signal type, chains, tokens, or confidence threshold.

Everything talks through buffered channels, so each stage runs at its own pace. There's also a CLI client (`cmd/client/`) if you just want to subscribe and see what comes out.

## Configuration

Three TOML files in `config/`, all overridable with `ORACLE_*` env vars (e.g. `ORACLE_CHAINS_ETHEREUM_RPC_URL`).

| File | What it does |
|------|-------------|
| `config.toml` | The basics: log level, gRPC port, chain RPC endpoints, how many enricher workers to spin up, Chainlink cache TTL |
| `registry.toml` | Maps contract addresses to names and protocols, and tokens to their Chainlink price feeds |
| `rules.toml` | The fun part: define rules with trigger conditions and operators, set up correlations that match event sequences within time windows |

Rules support `eq`, `gt`, `lt`, `gte`, `lte`, `in`, and `contains` operators.
