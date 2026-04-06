package evm

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/samber/mo"
	"github.com/wormhole-foundation/wormhole/sdk/vaa"

	"github.com/vaporif/x-chain-oracle/internal/types"
)

type DecoderFunc func(chain types.ChainID, log ethtypes.Log) mo.Result[types.RawEvent]

type DecoderRegistry struct {
	decoders map[common.Hash]DecoderFunc
}

func NewDecoderRegistry(tokenBridgeAddr string) *DecoderRegistry {
	logMsgPubSig := crypto.Keccak256Hash([]byte("LogMessagePublished(address,uint64,uint32,bytes,uint8)"))
	approvalSig := crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))

	return &DecoderRegistry{
		decoders: map[common.Hash]DecoderFunc{
			logMsgPubSig: makeWormholeDecoder(tokenBridgeAddr),
			approvalSig:  decodeERC20Approval,
		},
	}
}

func makeWormholeDecoder(tokenBridgeAddr string) DecoderFunc {
	return func(chain types.ChainID, log ethtypes.Log) mo.Result[types.RawEvent] {
		return decodeWormholeLogMessagePublished(chain, log, tokenBridgeAddr)
	}
}

func (r *DecoderRegistry) Decode(chain types.ChainID, log ethtypes.Log) mo.Result[types.RawEvent] {
	if len(log.Topics) == 0 {
		return mo.Err[types.RawEvent](fmt.Errorf("log has no topics"))
	}
	decoder, ok := r.decoders[log.Topics[0]]
	if !ok {
		return mo.Err[types.RawEvent](fmt.Errorf("no decoder for topic %s", log.Topics[0].Hex()))
	}
	return decoder(chain, log)
}

func LogMessagePublishedTopicHash() common.Hash {
	return crypto.Keccak256Hash([]byte("LogMessagePublished(address,uint64,uint32,bytes,uint8)"))
}

const logMessagePublishedMinDataLen = 160

func decodeWormholeLogMessagePublished(chain types.ChainID, log ethtypes.Log, tokenBridgeAddr string) mo.Result[types.RawEvent] {
	if len(log.Topics) < 2 {
		return mo.Err[types.RawEvent](fmt.Errorf("LogMessagePublished: expected 2 topics, got %d", len(log.Topics)))
	}

	sender := common.BytesToAddress(log.Topics[1].Bytes())
	tokenBridge := common.HexToAddress(tokenBridgeAddr)
	if sender != tokenBridge {
		return mo.Err[types.RawEvent](fmt.Errorf("sender %s is not Token Bridge %s", sender.Hex(), tokenBridge.Hex()))
	}

	if len(log.Data) < logMessagePublishedMinDataLen {
		return mo.Err[types.RawEvent](fmt.Errorf("LogMessagePublished: data too short (%d bytes)", len(log.Data)))
	}

	payloadOffset := new(big.Int).SetBytes(log.Data[64:96]).Uint64()
	if payloadOffset+32 > uint64(len(log.Data)) {
		return mo.Err[types.RawEvent](fmt.Errorf("LogMessagePublished: payload offset out of bounds"))
	}

	payloadLen := new(big.Int).SetBytes(log.Data[payloadOffset : payloadOffset+32]).Uint64()
	payloadStart := payloadOffset + 32
	if payloadStart+payloadLen > uint64(len(log.Data)) {
		return mo.Err[types.RawEvent](fmt.Errorf("LogMessagePublished: payload data out of bounds"))
	}

	payload := log.Data[payloadStart : payloadStart+payloadLen]

	return decodeWormholeTransferPayload(chain, log, payload, tokenBridgeAddr)
}

func decodeWormholeTransferPayload(chain types.ChainID, log ethtypes.Log, payload []byte, tokenBridgeAddr string) mo.Result[types.RawEvent] {
	if !vaa.IsTransfer(payload) {
		return mo.Err[types.RawEvent](fmt.Errorf("unsupported wormhole payload type: %d", payload[0]))
	}

	hdr, err := vaa.DecodeTransferPayloadHdr(payload)
	if err != nil {
		return mo.Err[types.RawEvent](fmt.Errorf("decode wormhole transfer: %w", err))
	}

	tokenAddr := common.BytesToAddress(hdr.OriginAddress[:])
	destChain := WormholeChainID(uint16(hdr.TargetChain))

	data := map[string]any{
		"token":        strings.ToLower(tokenAddr.Hex()),
		"amount":       hdr.Amount.String(),
		"sender":       common.HexToAddress(tokenBridgeAddr).Hex(),
		"target_chain": string(destChain),
		"contract":     log.Address.Hex(),
	}

	return mo.Ok(types.RawEvent{
		Chain:     chain,
		Block:     log.BlockNumber,
		TxHash:    log.TxHash.Hex(),
		Timestamp: 0,
		EventType: types.EventBridgeDeposit,
		Data:      data,
	})
}

func decodeERC20Approval(chain types.ChainID, log ethtypes.Log) mo.Result[types.RawEvent] {
	if len(log.Topics) < 3 {
		return mo.Err[types.RawEvent](fmt.Errorf("approval: expected 3 topics, got %d", len(log.Topics)))
	}
	if len(log.Data) < 32 {
		return mo.Err[types.RawEvent](fmt.Errorf("approval: data too short (%d bytes)", len(log.Data)))
	}

	owner := common.BytesToAddress(log.Topics[1].Bytes())
	spender := common.BytesToAddress(log.Topics[2].Bytes())
	value := new(big.Int).SetBytes(log.Data[:32])

	data := map[string]any{
		"token":    strings.ToLower(log.Address.Hex()),
		"sender":   owner.Hex(),
		"spender":  spender.Hex(),
		"amount":   value.String(),
		"contract": log.Address.Hex(),
	}

	return mo.Ok(types.RawEvent{
		Chain:     chain,
		Block:     log.BlockNumber,
		TxHash:    log.TxHash.Hex(),
		Timestamp: 0,
		EventType: types.EventTokenApproval,
		Data:      data,
	})
}
