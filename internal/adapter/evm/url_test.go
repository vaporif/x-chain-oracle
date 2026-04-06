package evm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vaporif/x-chain-oracle/internal/adapter/evm"
)

func TestDeriveHTTPURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"wss://eth-mainnet.g.alchemy.com/v2/key123", "https://eth-mainnet.g.alchemy.com/v2/key123"},
		{"ws://localhost:8546", "http://localhost:8546"},
		{"https://eth.example.com", "https://eth.example.com"},
		{"http://localhost:8545", "http://localhost:8545"},
	}
	for _, tt := range tests {
		got := evm.DeriveHTTPURL(tt.input)
		assert.Equal(t, tt.expected, got, "input: %s", tt.input)
	}
}
