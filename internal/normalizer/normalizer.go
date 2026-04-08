package normalizer

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/mo"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/pipeline"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
	"github.com/vaporif/x-chain-oracle/internal/types"
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
		logger.Warn("field type assertion failed",
			zap.String("field", "amount"),
			zap.String("tx", event.TxHash),
		)
	}

	if v, ok := data[senderKey].(string); ok {
		event.SourceAddress = v
	} else if data[senderKey] != nil {
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

func Run(ctx context.Context, tel *telemetry.Telemetry, in <-chan pipeline.Traced[types.RawEvent], out chan<- pipeline.Traced[types.ChainEvent]) {
	logger := zap.L().Named("normalizer")
	defer close(out)
	for traced := range in {
		if ctx.Err() != nil {
			return
		}

		tel.Metrics.EventsReceived.Add(ctx, 1,
			telemetry.StageAttr(telemetry.StageNormalizer))

		start := time.Now()
		result, err := processNormalize(tel, traced)
		if err != nil {
			tel.Metrics.EventsDropped.Add(ctx, 1,
				telemetry.StageAttr(telemetry.StageNormalizer))
			logger.Warn("skipping malformed event",
				zap.String("tx", traced.Value.TxHash),
				zap.Error(err),
			)
			continue
		}

		tel.Metrics.StageLatency.Record(ctx, float64(time.Since(start).Milliseconds()),
			telemetry.StageAttr(telemetry.StageNormalizer))

		logger.Debug("event normalized",
			zap.String("chain", string(result.Value.Chain)),
			zap.String("tx", result.Value.TxHash),
			zap.String("token", result.Value.Token),
			zap.String("amount", result.Value.Amount),
		)

		select {
		case out <- result:
			tel.Metrics.EventsEmitted.Add(ctx, 1,
				telemetry.StageAttr(telemetry.StageNormalizer))
		case <-ctx.Done():
			return
		}
	}
}

func processNormalize(tel *telemetry.Telemetry, traced pipeline.Traced[types.RawEvent]) (pipeline.Traced[types.ChainEvent], error) {
	ctx := traced.Ctx
	if tel.Config.Tracing.Stages.Normalizer {
		var span trace.Span
		ctx, span = tel.Tracer.Start(traced.Ctx, "pipeline.normalizer")
		defer span.End()
	}
	event, err := Normalize(traced.Value).Get()
	if err != nil {
		return pipeline.Traced[types.ChainEvent]{}, err
	}
	return pipeline.Traced[types.ChainEvent]{Value: event, Ctx: ctx, StartedAt: traced.StartedAt}, nil
}
