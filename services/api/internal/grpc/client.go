package grpc

import (
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
)

// OrchestratorClient is a thin wrapper around the generated PayoutServiceClient.
// It owns the underlying *grpc.ClientConn so callers do not need to manage it.
type OrchestratorClient struct {
	conn   *grpc.ClientConn
	Payout orchestratorv1.PayoutServiceClient
}

// NewOrchestratorClient dials the orchestrator at addr and returns a ready-to-use
// client. The caller is responsible for calling Close() when done.
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
