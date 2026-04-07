package normalizer

import (
	"context"
	"fmt"

	"github.com/samber/mo"
	"github.com/vaporif/x-chain-oracle/internal/types"
	"go.uber.org/zap"
)

func Normalize(raw types.RawEvent) mo.Result[types.ChainEvent] {
	logger := zap.L().Named("normalizer")
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
		return normalizeEVM(event, raw.Data, logger)
	case types.EventTokenAccCreate:
		return normalizeSolana(event, raw.Data, logger)
	case types.EventIBCSendPacket, types.EventIBCAckPacket:
		return normalizeCosmos(event, raw.Data, logger)
	default:
		return mo.Err[types.ChainEvent](fmt.Errorf("unknown event type: %s", raw.EventType))
	}
}

func normalizeEVM(event types.ChainEvent, data map[string]any, logger *zap.Logger) mo.Result[types.ChainEvent] {
	result := normalizeTokenEvent(event, data, "token", "sender", logger)
	if e, err := result.Get(); err == nil {
		if v, ok := data["contract"].(string); ok {
			e.ContractAddress = v
		} else if data["contract"] != nil {
			// TODO: replace with metrics counter
			logger.Warn("field type assertion failed",
				zap.String("field", "contract"),
				zap.String("tx", e.TxHash),
			)
		}
		return mo.Ok(e)
	}
	return result
}

func normalizeSolana(event types.ChainEvent, data map[string]any, logger *zap.Logger) mo.Result[types.ChainEvent] {
	return normalizeTokenEvent(event, data, "mint", "owner", logger)
}

func normalizeCosmos(event types.ChainEvent, data map[string]any, logger *zap.Logger) mo.Result[types.ChainEvent] {
	return normalizeTokenEvent(event, data, "token", "sender", logger)
}

func normalizeTokenEvent(event types.ChainEvent, data map[string]any, tokenKey, senderKey string, logger *zap.Logger) mo.Result[types.ChainEvent] {
	token, ok := data[tokenKey].(string)
	if !ok {
		return mo.Err[types.ChainEvent](fmt.Errorf("missing or invalid '%s' field", tokenKey))
	}
	event.Token = token

	if v, ok := data["amount"].(string); ok {
		event.Amount = v
	} else if data["amount"] != nil {
		// TODO: replace with metrics counter
		logger.Warn("field type assertion failed",
			zap.String("field", "amount"),
			zap.String("tx", event.TxHash),
		)
	}

	if v, ok := data[senderKey].(string); ok {
		event.SourceAddress = v
	} else if data[senderKey] != nil {
		// TODO: replace with metrics counter
		logger.Warn("field type assertion failed",
			zap.String("field", senderKey),
			zap.String("tx", event.TxHash),
		)
	}

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
			// TODO: replace with metrics counter
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
