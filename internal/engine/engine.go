package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samber/mo"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/pipeline"
	"github.com/vaporif/x-chain-oracle/internal/telemetry"
	"github.com/vaporif/x-chain-oracle/internal/types"
)

type Engine struct {
	rules      *RulesConfig
	correlator *Correlator
	tel        *telemetry.Telemetry
}

func New(rules *RulesConfig, cfg CorrelatorConfig, tel *telemetry.Telemetry) *Engine {
	return &Engine{
		rules:      rules,
		correlator: NewCorrelator(rules.Correlations, cfg),
		tel:        tel,
	}
}

func (e *Engine) Evaluate(event types.EnrichedEvent) []types.Signal {
	var signals []types.Signal
	for _, rule := range e.rules.Rules {
		if rule.Trigger != string(event.EventType) {
			continue
		}
		matched := matchConditions(rule.Conditions, event)
		zap.L().Named("engine").Debug("rule evaluated",
			zap.String("rule", rule.Name),
			zap.Bool("matched", matched),
		)
		if !matched {
			continue
		}
		sig := newSignal(rule.Signal, rule.Confidence, event, buildMetadata(rule.MetadataFields, event))
		signals = append(signals, sig)
	}
	return signals
}

func matchConditions(conditions []Condition, event types.EnrichedEvent) bool {
	for _, c := range conditions {
		if !evalCondition(c, event) {
			return false
		}
	}
	return true
}

func evalCondition(c Condition, event types.EnrichedEvent) bool {
	fieldVal := getFieldValue(c.Field, event)

	switch c.Op {
	case "eq":
		return fieldVal == c.Value
	case "gt", "lt", "gte", "lte":
		return compareNumeric(c.Op, fieldVal, c.Value)
	case "in":
		for _, s := range strings.Split(c.Value, ",") {
			if strings.TrimSpace(s) == fieldVal {
				return true
			}
		}
		return false
	case "contains":
		return strings.Contains(fieldVal, c.Value)
	default:
		zap.L().Named("engine").Warn("unknown operator", zap.String("op", c.Op))
		return false
	}
}

func compareNumeric(op, fieldStr, valueStr string) bool {
	fieldVal, err1 := strconv.ParseFloat(fieldStr, 64)
	valueVal, err2 := strconv.ParseFloat(valueStr, 64)
	if err1 != nil || err2 != nil {
		zap.L().Named("engine").Debug("numeric comparison failed",
			zap.String("field_value", fieldStr),
			zap.String("rule_value", valueStr),
			zap.String("op", op),
		)
		return false
	}
	switch op {
	case "gt":
		return fieldVal > valueVal
	case "lt":
		return fieldVal < valueVal
	case "gte":
		return fieldVal >= valueVal
	case "lte":
		return fieldVal <= valueVal
	}
	return false
}

func getFieldValue(field string, event types.EnrichedEvent) string {
	switch field {
	case "amount_usd":
		if v, ok := event.AmountUSD.Get(); ok {
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
		return ""
	case "destination_chain":
		if v, ok := event.DestChain.Get(); ok {
			return string(v)
		}
		return ""
	case "source_chain":
		return string(event.Chain)
	case "token":
		return event.Token
	case "amount":
		return event.Amount
	case "source_address":
		return event.SourceAddress
	case "protocol":
		if v, ok := event.Protocol.Get(); ok {
			return v
		}
		return ""
	case "contract_name":
		if v, ok := event.ContractName.Get(); ok {
			return v
		}
		return ""
	case "contract_address":
		return event.ContractAddress
	default:
		return ""
	}
}

func newSignal(signalType string, confidence float64, event types.EnrichedEvent, metadata map[string]string) types.Signal {
	return types.Signal{
		ID:                  uuid.NewString(),
		SignalType:          signalType,
		SourceChain:         event.Chain,
		DestinationChain:    event.DestChain,
		Token:               event.Token,
		Amount:              event.Amount,
		AmountUSD:           event.AmountUSD,
		DetectedAt:          time.Now().Unix(),
		EstimatedActionTime: mo.None[int64](),
		Confidence:          confidence,
		Metadata:            metadata,
	}
}

func buildMetadata(fields []string, event types.EnrichedEvent) map[string]string {
	meta := make(map[string]string)
	for _, f := range fields {
		if v := getFieldValue(f, event); v != "" {
			meta[f] = v
		}
	}
	return meta
}

func (e *Engine) Run(ctx context.Context, in <-chan pipeline.Traced[types.EnrichedEvent], out chan<- pipeline.Traced[types.Signal]) {
	defer close(out)

	done := make(chan struct{})
	defer close(done)
	e.correlator.StartPruner(done)

	for traced := range in {
		if ctx.Err() != nil {
			return
		}

		e.tel.Metrics.EventsReceived.Add(ctx, 1,
			otelmetric.WithAttributes(attribute.String("stage", "engine")))

		start := time.Now()

		for _, sig := range e.evaluateWithSpan(traced) {
			select {
			case out <- sig:
				e.tel.Metrics.EventsEmitted.Add(ctx, 1,
					otelmetric.WithAttributes(attribute.String("stage", "engine")))
			case <-ctx.Done():
				return
			}
		}

		for _, sig := range e.correlateWithSpan(traced.Value) {
			zap.L().Named("engine").Debug("correlation matched",
				zap.String("signal_type", sig.Value.SignalType),
			)
			select {
			case out <- sig:
				e.tel.Metrics.EventsEmitted.Add(ctx, 1,
					otelmetric.WithAttributes(attribute.String("stage", "engine")))
			case <-ctx.Done():
				return
			}
		}

		e.tel.Metrics.StageLatency.Record(ctx, float64(time.Since(start).Milliseconds()),
			otelmetric.WithAttributes(attribute.String("stage", "engine")))
		e.tel.Metrics.CorrelationsOpen.Record(ctx, e.correlator.OpenEntries())
	}
}

func (e *Engine) evaluateWithSpan(traced pipeline.Traced[types.EnrichedEvent]) []pipeline.Traced[types.Signal] {
	ctx := traced.Ctx
	var span trace.Span
	if e.tel.Config.Tracing.Stages.Engine {
		ctx, span = e.tel.Tracer.Start(traced.Ctx, "pipeline.engine.evaluate")
		defer span.End()
	}

	signals := e.Evaluate(traced.Value)

	e.tel.Metrics.RulesEvaluated.Add(ctx, int64(len(e.rules.Rules)))
	if span != nil {
		span.SetAttributes(attribute.Bool("matched", len(signals) > 0))
	}

	var result []pipeline.Traced[types.Signal]
	for _, sig := range signals {
		e.tel.Metrics.RulesMatched.Add(ctx, 1)
		result = append(result, pipeline.Traced[types.Signal]{Value: sig, Ctx: ctx, StartedAt: traced.StartedAt})
	}
	return result
}

func (e *Engine) correlateWithSpan(event types.EnrichedEvent) []pipeline.Traced[types.Signal] {
	signals := e.correlator.Process(event)
	if len(signals) == 0 {
		return nil
	}
	var result []pipeline.Traced[types.Signal]
	for _, sig := range signals {
		result = append(result, e.wrapCorrelationSignal(sig))
	}
	return result
}

func (e *Engine) wrapCorrelationSignal(sig types.Signal) pipeline.Traced[types.Signal] {
	ctx := context.Background()
	if e.tel.Config.Tracing.Stages.Engine {
		var span trace.Span
		ctx, span = e.tel.Tracer.Start(ctx, "pipeline.engine.correlate")
		defer span.End()
	}
	return pipeline.Traced[types.Signal]{Value: sig, Ctx: ctx, StartedAt: time.Now()}
}
