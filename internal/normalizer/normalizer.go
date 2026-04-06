package normalizer

import (
	"context"
	"fmt"

	"github.com/samber/mo"
	"github.com/vaporif/x-chain-oracle/internal/types"
	"go.uber.org/zap"
)

func Normalize(raw types.RawEvent) mo.Result[types.ChainEvent] {
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
		return mo.Err[types.ChainEvent](fmt.Errorf("unknown event type: %s", raw.EventType))
	}
}

func normalizeEVM(event types.ChainEvent, data map[string]any) mo.Result[types.ChainEvent] {
	result := normalizeTokenEvent(event, data, "token", "sender")
	if e, err := result.Get(); err == nil {
		e.ContractAddress, _ = data["contract"].(string)
		return mo.Ok(e)
	}
	return result
}

func normalizeSolana(event types.ChainEvent, data map[string]any) mo.Result[types.ChainEvent] {
	return normalizeTokenEvent(event, data, "mint", "owner")
}

func normalizeCosmos(event types.ChainEvent, data map[string]any) mo.Result[types.ChainEvent] {
	return normalizeTokenEvent(event, data, "token", "sender")
}

func normalizeTokenEvent(event types.ChainEvent, data map[string]any, tokenKey, senderKey string) mo.Result[types.ChainEvent] {
	token, ok := data[tokenKey].(string)
	if !ok {
		return mo.Err[types.ChainEvent](fmt.Errorf("missing or invalid '%s' field", tokenKey))
	}
	event.Token = token
	event.Amount, _ = data["amount"].(string)
	event.SourceAddress, _ = data[senderKey].(string)

	if dest, ok := data["target_chain"].(string); ok {
		event.DestChain = mo.Some(types.ChainID(dest))
	} else {
		event.DestChain = mo.None[types.ChainID]()
	}
	return mo.Ok(event)
}

func Run(ctx context.Context, in <-chan types.RawEvent, out chan<- types.ChainEvent) {
	logger := zap.L().Named("normalizer")
	defer close(out)
	for raw := range in {
		if ctx.Err() != nil {
			return
		}
		event, err := Normalize(raw).Get()
		if err != nil {
			logger.Warn("skipping malformed event",
				zap.String("tx", raw.TxHash),
				zap.Error(err),
			)
			continue
		}
		logger.Debug("event normalized",
			zap.String("chain", string(event.Chain)),
			zap.String("tx", event.TxHash),
			zap.String("token", event.Token),
			zap.String("amount", event.Amount),
		)
		select {
		case out <- event:
		case <-ctx.Done():
			return
		}
	}
}
