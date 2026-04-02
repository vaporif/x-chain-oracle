package normalizer

import (
	"context"
	"fmt"

	"github.com/samber/mo"
	"github.com/vaporif/x-chain-oracle/internal/types"
	"go.uber.org/zap"
)

func Normalize(raw types.RawEvent) (types.ChainEvent, error) {
	event := types.ChainEvent{
		Chain:     raw.Chain,
		Block:     raw.Block,
		TxHash:    raw.TxHash,
		Timestamp: raw.Timestamp,
		EventType: raw.EventType,
		RawData:   raw.Data,
	}

	switch raw.EventType {
	case types.EventBridgeDeposit, types.EventDEXSwap, types.EventTokenApproval:
		return normalizeEVM(event, raw.Data)
	case types.EventTokenAccCreate:
		return normalizeSolana(event, raw.Data)
	case types.EventIBCSendPacket, types.EventIBCAckPacket:
		return normalizeCosmos(event, raw.Data)
	default:
		return types.ChainEvent{}, fmt.Errorf("unknown event type: %s", raw.EventType)
	}
}

func normalizeEVM(event types.ChainEvent, data map[string]any) (types.ChainEvent, error) {
	event, err := normalizeTokenEvent(event, data, "token", "sender")
	if err != nil {
		return event, err
	}
	event.ContractAddress, _ = data["contract"].(string)
	return event, nil
}

func normalizeSolana(event types.ChainEvent, data map[string]any) (types.ChainEvent, error) {
	return normalizeTokenEvent(event, data, "mint", "owner")
}

func normalizeCosmos(event types.ChainEvent, data map[string]any) (types.ChainEvent, error) {
	return normalizeTokenEvent(event, data, "token", "sender")
}

func normalizeTokenEvent(event types.ChainEvent, data map[string]any, tokenKey, senderKey string) (types.ChainEvent, error) {
	token, ok := data[tokenKey].(string)
	if !ok {
		return types.ChainEvent{}, fmt.Errorf("missing or invalid '%s' field", tokenKey)
	}
	event.Token = token
	event.Amount, _ = data["amount"].(string)
	event.SourceAddress, _ = data[senderKey].(string)

	if dest, ok := data["target_chain"].(string); ok {
		event.DestChain = mo.Some(types.ChainID(dest))
	} else {
		event.DestChain = mo.None[types.ChainID]()
	}
	return event, nil
}

func Run(ctx context.Context, in <-chan types.RawEvent, out chan<- types.ChainEvent) {
	logger := zap.L().Named("normalizer")
	defer close(out)
	for raw := range in {
		if ctx.Err() != nil {
			return
		}
		event, err := Normalize(raw)
		if err != nil {
			logger.Warn("skipping malformed event",
				zap.String("tx", raw.TxHash),
				zap.Error(err),
			)
			continue
		}
		select {
		case out <- event:
		case <-ctx.Done():
			return
		}
	}
}
