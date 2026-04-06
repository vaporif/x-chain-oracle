package evm

import (
	"context"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

type WebSocketStrategy struct {
	client     *ethclient.Client
	bufferSize int
}

func NewWebSocketStrategy(client *ethclient.Client, bufferSize int) *WebSocketStrategy {
	return &WebSocketStrategy{client: client, bufferSize: bufferSize}
}

func (s *WebSocketStrategy) Subscribe(ctx context.Context, filter ethereum.FilterQuery) (<-chan types.Log, ethereum.Subscription, error) {
	logs := make(chan types.Log, s.bufferSize)
	sub, err := s.client.SubscribeFilterLogs(ctx, filter, logs)
	if err != nil {
		return nil, nil, err
	}
	return logs, sub, nil
}
