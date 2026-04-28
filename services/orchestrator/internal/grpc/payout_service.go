package grpc

import (
	"context"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
)

// PayoutServiceServer implements orchestratorv1.PayoutServiceServer.
// All methods are stubs that return deterministic dummy responses; real
// business logic will be layered in once persistence and domain packages exist.
type PayoutServiceServer struct {
	orchestratorv1.UnimplementedPayoutServiceServer
	log *zap.Logger
}

func newPayoutServiceServer(log *zap.Logger) *PayoutServiceServer {
	return &PayoutServiceServer{log: log}
}

func (s *PayoutServiceServer) CreatePayout(
	ctx context.Context,
	req *orchestratorv1.CreatePayoutRequest,
) (*orchestratorv1.CreatePayoutResponse, error) {
	s.log.Info("CreatePayout stub", zap.String("payout_id", req.GetPayoutId()))
	return &orchestratorv1.CreatePayoutResponse{
		PayoutId: req.GetPayoutId(),
		Status:   "PENDING",
	}, nil
}

func (s *PayoutServiceServer) GetPayout(
	ctx context.Context,
	req *orchestratorv1.GetPayoutRequest,
) (*orchestratorv1.GetPayoutResponse, error) {
	s.log.Info("GetPayout stub", zap.String("payout_id", req.GetPayoutId()))
	return &orchestratorv1.GetPayoutResponse{
		PayoutId: req.GetPayoutId(),
		Status:   "PENDING",
		Amount:   0,
		Currency: "USD",
		Provider: "stub",
	}, nil
}

func (s *PayoutServiceServer) CancelPayout(
	ctx context.Context,
	req *orchestratorv1.CancelPayoutRequest,
) (*orchestratorv1.CancelPayoutResponse, error) {
	s.log.Info("CancelPayout stub", zap.String("payout_id", req.GetPayoutId()))
	return &orchestratorv1.CancelPayoutResponse{Success: true}, nil
}

func (s *PayoutServiceServer) ApplyProviderUpdate(
	ctx context.Context,
	req *orchestratorv1.ApplyProviderUpdateRequest,
) (*orchestratorv1.ApplyProviderUpdateResponse, error) {
	s.log.Info("ApplyProviderUpdate stub",
		zap.String("payout_id", req.GetPayoutId()),
		zap.String("provider_status", req.GetProviderStatus()),
	)
	if req.GetPayoutId() == "" {
		return nil, status.Error(codes.InvalidArgument, "payout_id is required")
	}
	return &orchestratorv1.ApplyProviderUpdateResponse{Success: true}, nil
}
