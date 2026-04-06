package types

import "github.com/samber/mo"

type ChainID string

const (
	ChainEthereum  ChainID = "ethereum"
	ChainArbitrum  ChainID = "arbitrum"
	ChainBase      ChainID = "base"
	ChainSolana    ChainID = "solana"
	ChainCosmosHub ChainID = "cosmoshub"
)

type EventType string

const (
	EventBridgeDeposit  EventType = "bridge_deposit"
	EventTokenApproval  EventType = "token_approval"
	EventDEXSwap        EventType = "dex_swap"
	EventIBCSendPacket  EventType = "ibc_send_packet"
	EventIBCAckPacket   EventType = "ibc_ack_packet"
	EventTokenAccCreate EventType = "token_account_create"
)

type RawEvent struct {
	Chain     ChainID
	Block     uint64
	TxHash    string
	Timestamp int64
	EventType EventType
	Data      map[string]any
}

type ChainEvent struct {
	Chain           ChainID
	Block           uint64
	TxHash          string
	Timestamp       int64
	EventType       EventType
	Token           string
	Amount          string
	SourceAddress   string
	ContractAddress string
	DestChain       mo.Option[ChainID]
	RawData         map[string]any
}

type EnrichedEvent struct {
	ChainEvent
	ContractName mo.Option[string]
	Protocol     mo.Option[string]
	AmountUSD    mo.Option[float64]
}

type Signal struct {
	ID                  string
	SignalType          string
	SourceChain         ChainID
	DestinationChain    mo.Option[ChainID]
	Token               string
	Amount              string
	AmountUSD           mo.Option[float64]
	DetectedAt          int64
	EstimatedActionTime mo.Option[int64]
	Confidence          float64
	Metadata            map[string]string
}
