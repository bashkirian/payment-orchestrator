package grpc

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
)

// PayoutServiceClient is the interface for the orchestrator's payout service.
type PayoutServiceClient interface {
	ApplyProviderUpdate(ctx context.Context, req *orchestratorv1.ApplyProviderUpdateRequest, opts ...grpc.CallOption) (*orchestratorv1.ApplyProviderUpdateResponse, error)
}

// OrchestratorClient is a thin wrapper around the generated PayoutServiceClient.
type OrchestratorClient struct {
	conn   *grpc.ClientConn
	Payout PayoutServiceClient
}

// NewOrchestratorClient dials the orchestrator and returns a ready-to-use client.
func NewOrchestratorClient(addr string) (*OrchestratorClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial orchestrator %s: %w", addr, err)
	}
	return &OrchestratorClient{
		conn:   conn,
		Payout: orchestratorv1.NewPayoutServiceClient(conn),
	}, nil
}

// Close tears down the underlying connection.
func (c *OrchestratorClient) Close() error {
	return c.conn.Close()
}
