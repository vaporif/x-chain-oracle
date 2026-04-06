package evm

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/vaporif/x-chain-oracle/internal/types"
)

type DecoderFunc func(chain types.ChainID, log ethtypes.Log) (types.RawEvent, error)

type DecoderRegistry struct {
	decoders map[common.Hash]DecoderFunc
}

func NewDecoderRegistry() *DecoderRegistry {
	logMsgPubSig := crypto.Keccak256Hash([]byte("LogMessagePublished(address,uint64,uint32,bytes,uint8)"))

	return &DecoderRegistry{
		decoders: map[common.Hash]DecoderFunc{
			logMsgPubSig: decodeWormholeLogMessagePublished,
		},
	}
}

func (r *DecoderRegistry) Decode(chain types.ChainID, log ethtypes.Log) (types.RawEvent, error) {
	if len(log.Topics) == 0 {
		return types.RawEvent{}, fmt.Errorf("log has no topics")
	}
	decoder, ok := r.decoders[log.Topics[0]]
	if !ok {
		return types.RawEvent{}, fmt.Errorf("no decoder for topic %s", log.Topics[0].Hex())
	}
	return decoder(chain, log)
}

func LogMessagePublishedTopicHash() common.Hash {
	return crypto.Keccak256Hash([]byte("LogMessagePublished(address,uint64,uint32,bytes,uint8)"))
}

func decodeWormholeLogMessagePublished(chain types.ChainID, log ethtypes.Log) (types.RawEvent, error) {
	if len(log.Topics) < 2 {
		return types.RawEvent{}, fmt.Errorf("LogMessagePublished: expected 2 topics, got %d", len(log.Topics))
	}

	sender := common.BytesToAddress(log.Topics[1].Bytes())
	tokenBridge := common.HexToAddress(WormholeTokenBridgeAddress)
	if sender != tokenBridge {
		return types.RawEvent{}, fmt.Errorf("sender %s is not Token Bridge %s", sender.Hex(), tokenBridge.Hex())
	}

	if len(log.Data) < 160 {
		return types.RawEvent{}, fmt.Errorf("LogMessagePublished: data too short (%d bytes)", len(log.Data))
	}

	payloadOffset := new(big.Int).SetBytes(log.Data[64:96]).Uint64()
	if payloadOffset+32 > uint64(len(log.Data)) {
		return types.RawEvent{}, fmt.Errorf("LogMessagePublished: payload offset out of bounds")
	}

	payloadLen := new(big.Int).SetBytes(log.Data[payloadOffset : payloadOffset+32]).Uint64()
	payloadStart := payloadOffset + 32
	if payloadStart+payloadLen > uint64(len(log.Data)) {
		return types.RawEvent{}, fmt.Errorf("LogMessagePublished: payload data out of bounds")
	}

	payload := log.Data[payloadStart : payloadStart+payloadLen]

	return decodeWormholeTransferPayload(chain, log, payload)
}

func decodeWormholeTransferPayload(chain types.ChainID, log ethtypes.Log, payload []byte) (types.RawEvent, error) {
	if len(payload) < 101 {
		return types.RawEvent{}, fmt.Errorf("wormhole payload too short: %d bytes, need at least 101", len(payload))
	}

	payloadType := payload[0]
	if payloadType != 1 && payloadType != 3 {
		return types.RawEvent{}, fmt.Errorf("unsupported wormhole payload type: %d", payloadType)
	}

	amount := new(big.Int).SetBytes(payload[1:33])
	tokenAddr := common.BytesToAddress(payload[33:65])
	recipientChain := binary.BigEndian.Uint16(payload[99:101])

	destChain := WormholeChainID(recipientChain)

	data := map[string]any{
		"token":        strings.ToLower(tokenAddr.Hex()),
		"amount":       amount.String(),
		"sender":       common.HexToAddress(WormholeTokenBridgeAddress).Hex(),
		"target_chain": string(destChain),
		"contract":     log.Address.Hex(),
	}

	return types.RawEvent{
		Chain:     chain,
		Block:     log.BlockNumber,
		TxHash:    log.TxHash.Hex(),
		Timestamp: 0,
		EventType: types.EventBridgeDeposit,
		Data:      data,
	}, nil
}
