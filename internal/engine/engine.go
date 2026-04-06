package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samber/mo"
	"go.uber.org/zap"

	"github.com/vaporif/x-chain-oracle/internal/types"
)

type Engine struct {
	rules      *RulesConfig
	correlator *Correlator
}

func New(rules *RulesConfig, cfg CorrelatorConfig) *Engine {
	return &Engine{
		rules:      rules,
		correlator: NewCorrelator(rules.Correlations, cfg),
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

func (e *Engine) Run(ctx context.Context, in <-chan types.EnrichedEvent, out chan<- types.Signal) {
	defer close(out)

	done := make(chan struct{})
	defer close(done)
	e.correlator.StartPruner(done)

	for event := range in {
		if ctx.Err() != nil {
			return
		}
		for _, sig := range e.Evaluate(event) {
			select {
			case out <- sig:
			case <-ctx.Done():
				return
			}
		}
		corrSignals := e.correlator.Process(event)
		for _, sig := range corrSignals {
			zap.L().Named("engine").Debug("correlation matched",
				zap.String("signal_type", sig.SignalType),
			)
			select {
			case out <- sig:
			case <-ctx.Done():
				return
			}
		}
	}
}
