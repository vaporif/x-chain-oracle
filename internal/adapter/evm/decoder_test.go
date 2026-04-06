package evm_test

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vaporif/x-chain-oracle/internal/adapter/evm"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

func buildWormholeTransferPayload(amount *big.Int, tokenAddr common.Address, tokenChain, recipientChain uint16) []byte {
	payload := make([]byte, 133)
	payload[0] = 1
	amountBytes := amount.Bytes()
	copy(payload[1+32-len(amountBytes):33], amountBytes)
	copy(payload[33+12:65], tokenAddr.Bytes())
	binary.BigEndian.PutUint16(payload[65:67], tokenChain)
	binary.BigEndian.PutUint16(payload[99:101], recipientChain)
	return payload
}

func logMessagePublishedTopic() common.Hash {
	return crypto.Keccak256Hash([]byte("LogMessagePublished(address,uint64,uint32,bytes,uint8)"))
}

func TestDecodeWormholeBridgeDeposit(t *testing.T) {
	tokenAddr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	amount := new(big.Int).SetUint64(100_000_000_000)
	payload := buildWormholeTransferPayload(amount, tokenAddr, 2, 1)

	data := buildLogMessagePublishedData(42, 0, payload, 1)
	senderTopic := common.HexToHash("0x000000000000000000000000" + evm.WormholeTokenBridgeAddress[2:])

	log := ethtypes.Log{
		Address: common.HexToAddress(evm.WormholeCoreAddress),
		Topics:  []common.Hash{logMessagePublishedTopic(), senderTopic},
		Data:    data,
	}

	registry := evm.NewDecoderRegistry()
	rawEvent, err := registry.Decode(types.ChainEthereum, log)
	require.NoError(t, err)

	assert.Equal(t, types.EventBridgeDeposit, rawEvent.EventType)
	assert.Equal(t, types.ChainEthereum, rawEvent.Chain)
	assert.Equal(t, "100000000000", rawEvent.Data["amount"])
	assert.Equal(t, "solana", rawEvent.Data["target_chain"])
	assert.Contains(t, rawEvent.Data["token"].(string), "a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48")
}

func TestDecodeWormholeType3Payload(t *testing.T) {
	tokenAddr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	amount := new(big.Int).SetUint64(50_000_000_000)
	payload := make([]byte, 165)
	payload[0] = 3
	amountBytes := amount.Bytes()
	copy(payload[1+32-len(amountBytes):33], amountBytes)
	copy(payload[33+12:65], tokenAddr.Bytes())
	binary.BigEndian.PutUint16(payload[65:67], 2)
	binary.BigEndian.PutUint16(payload[99:101], 1)

	data := buildLogMessagePublishedData(43, 0, payload, 1)
	senderTopic := common.HexToHash("0x000000000000000000000000" + evm.WormholeTokenBridgeAddress[2:])

	log := ethtypes.Log{
		Address: common.HexToAddress(evm.WormholeCoreAddress),
		Topics:  []common.Hash{logMessagePublishedTopic(), senderTopic},
		Data:    data,
	}

	registry := evm.NewDecoderRegistry()
	rawEvent, err := registry.Decode(types.ChainEthereum, log)
	require.NoError(t, err)

	assert.Equal(t, types.EventBridgeDeposit, rawEvent.EventType)
	assert.Equal(t, "50000000000", rawEvent.Data["amount"])
	assert.Equal(t, "solana", rawEvent.Data["target_chain"])
}

func TestDecodeUnknownTopic(t *testing.T) {
	log := ethtypes.Log{
		Topics: []common.Hash{common.HexToHash("0xdeadbeef")},
	}
	registry := evm.NewDecoderRegistry()
	_, err := registry.Decode(types.ChainEthereum, log)
	assert.Error(t, err)
}

func TestDecodeNonTokenBridgeSender(t *testing.T) {
	payload := make([]byte, 133)
	payload[0] = 1
	data := buildLogMessagePublishedData(1, 0, payload, 1)
	senderTopic := common.HexToHash("0x0000000000000000000000001111111111111111111111111111111111111111")

	log := ethtypes.Log{
		Address: common.HexToAddress(evm.WormholeCoreAddress),
		Topics:  []common.Hash{logMessagePublishedTopic(), senderTopic},
		Data:    data,
	}

	registry := evm.NewDecoderRegistry()
	_, err := registry.Decode(types.ChainEthereum, log)
	assert.Error(t, err)
}

func TestDecodeTruncatedPayload(t *testing.T) {
	payload := make([]byte, 50)
	payload[0] = 1
	data := buildLogMessagePublishedData(1, 0, payload, 1)
	senderTopic := common.HexToHash("0x000000000000000000000000" + evm.WormholeTokenBridgeAddress[2:])

	log := ethtypes.Log{
		Address: common.HexToAddress(evm.WormholeCoreAddress),
		Topics:  []common.Hash{logMessagePublishedTopic(), senderTopic},
		Data:    data,
	}

	registry := evm.NewDecoderRegistry()
	_, err := registry.Decode(types.ChainEthereum, log)
	assert.Error(t, err)
}

func buildLogMessagePublishedData(sequence uint64, nonce uint32, payload []byte, consistencyLevel uint8) []byte {
	seqWord := make([]byte, 32)
	binary.BigEndian.PutUint64(seqWord[24:], sequence)
	nonceWord := make([]byte, 32)
	binary.BigEndian.PutUint32(nonceWord[28:], nonce)
	offsetWord := make([]byte, 32)
	binary.BigEndian.PutUint64(offsetWord[24:], 128)
	clWord := make([]byte, 32)
	clWord[31] = consistencyLevel
	lenWord := make([]byte, 32)
	binary.BigEndian.PutUint64(lenWord[24:], uint64(len(payload)))
	paddedPayload := make([]byte, ((len(payload)+31)/32)*32)
	copy(paddedPayload, payload)

	var data []byte
	data = append(data, seqWord...)
	data = append(data, nonceWord...)
	data = append(data, offsetWord...)
	data = append(data, clWord...)
	data = append(data, lenWord...)
	data = append(data, paddedPayload...)
	return data
}
