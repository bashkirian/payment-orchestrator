package grpc

import (
	"context"
	stderrors "errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bashkirian/fintech-project/libs/errors"
	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
	"github.com/bashkirian/fintech-project/libs/grpcutil"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/persistence/postgres"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/persistence/sqlcgen"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
)

// errIdempotencyKeyExists is a sentinel used to signal a key conflict inside RunInTx.
var errIdempotencyKeyExists = stderrors.New("idempotency key already exists")

// txResult carries data captured inside the transaction closure.
type txResult struct {
	newPayout   domain.Payout
	existingKey domain.IdempotencyKey
	inserted    bool
}

// PayoutServiceServer implements orchestratorv1.PayoutServiceServer.
type PayoutServiceServer struct {
	orchestratorv1.UnimplementedPayoutServiceServer
	log          *zap.Logger
	pool         *pgxpool.Pool
	payoutRepo   domain.PayoutRepository
	orchestrator *provider.Orchestrator
	routingAlgo  provider.RoutingAlgorithm
}

func newPayoutServiceServer(
	log *zap.Logger,
	pool *pgxpool.Pool,
	orchestrator *provider.Orchestrator,
	routingAlgo provider.RoutingAlgorithm,
) *PayoutServiceServer {
	return &PayoutServiceServer{
		log:          log,
		pool:         pool,
		payoutRepo:   postgres.NewPayoutRepo(sqlcgen.New(pool)),
		orchestrator: orchestrator,
		routingAlgo:  routingAlgo,
	}
}

func (s *PayoutServiceServer) CreatePayout(
	ctx context.Context,
	req *orchestratorv1.CreatePayoutRequest,
) (*orchestratorv1.CreatePayoutResponse, error) {
	requestID := grpcutil.GetRequestID(ctx)

	if req.GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	if req.GetRequestHash() == "" {
		return nil, status.Error(codes.InvalidArgument, "request_hash is required")
	}

	s.log.Info("CreatePayout",
		zap.String("request_id", requestID),
		zap.String("idempotency_key", req.GetIdempotencyKey()),
		zap.Int64("amount", req.GetAmount()),
		zap.String("currency", req.GetCurrency()),
		zap.String("rail", req.GetRail()),
	)

	var result txResult

	txErr := postgres.RunInTx(ctx, s.pool, func(ctx context.Context, q sqlcgen.Querier) error {
		rail := domain.Rail(req.GetRail())

		// Create payout with placeholder provider (will be updated after routing)
		payout, err := postgres.NewPayoutRepo(q).CreatePayout(ctx, domain.CreatePayoutParams{
			State:       domain.PayoutStateCreated,
			AmountCents: req.GetAmount(),
			Currency:    req.GetCurrency(),
			Rail:        rail,
			Provider:    "", // Will be set after routing decision
		})
		if err != nil {
			return err
		}

		key, inserted, err := postgres.NewIdempotencyRepo(q).TryInsertIdempotencyKey(
			ctx, req.GetIdempotencyKey(), req.GetRequestHash(), payout.ID,
		)
		if err != nil {
			return err
		}

		result = txResult{newPayout: payout, existingKey: key, inserted: inserted}
		if !inserted {
			// Signal rollback of the optimistically created payout.
			return errIdempotencyKeyExists
		}
		return nil
	})

	switch {
	case txErr == nil:
		// Happy path: new payout created — attempt to send via orchestrator.
		payout := result.newPayout
		payout = s.sendPayoutWithOrchestrator(ctx, payout)
		return &orchestratorv1.CreatePayoutResponse{
			PayoutId: payout.ID.String(),
			Status:   string(payout.State),
		}, nil

	case stderrors.Is(txErr, errIdempotencyKeyExists):
		// Key already exists – check whether this is a replay or a conflict.
		if result.existingKey.RequestHash != req.GetRequestHash() {
			return nil, status.Error(codes.AlreadyExists, errors.CodeIdempotencyConflict)
		}
		// Same hash: return the existing payout.
		existing, err := s.payoutRepo.GetPayout(ctx, result.existingKey.PayoutID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "fetch existing payout: %v", err)
		}
		s.log.Info("idempotent replay", zap.String("request_id", requestID), zap.String("payout_id", existing.ID.String()))
		return &orchestratorv1.CreatePayoutResponse{
			PayoutId: existing.ID.String(),
			Status:   string(existing.State),
		}, nil

	default:
		return nil, status.Errorf(codes.Internal, "create payout: %v", txErr)
	}
}

// sendPayoutWithOrchestrator uses the orchestrator to send the payout with fallback support.
func (s *PayoutServiceServer) sendPayoutWithOrchestrator(ctx context.Context, payout domain.Payout) domain.Payout {
	if s.orchestrator == nil {
		s.log.Error("orchestrator not configured", zap.String("payout_id", payout.ID.String()))
		updated, updateErr := s.payoutRepo.UpdatePayoutState(ctx, payout.ID, domain.UpdatePayoutParams{
			State: domain.PayoutStateFailed,
		})
		if updateErr != nil {
			s.log.Error("failed to mark payout as failed", zap.String("payout_id", payout.ID.String()), zap.Error(updateErr))
		}
		return updated
	}

	result := s.orchestrator.SendPayoutWithFallback(ctx, payout, s.routingAlgo)

	if result.Success {
		updated, err := s.payoutRepo.UpdatePayoutState(ctx, payout.ID, domain.UpdatePayoutParams{
			State:      domain.PayoutStateSent,
			ExternalID: &result.ExternalID,
			Provider:   result.UsedProvider,
		})
		if err != nil {
			s.log.Error("failed to mark payout as sent",
				zap.String("payout_id", payout.ID.String()),
				zap.Error(err),
			)
			return payout
		}
		return updated
	}

	// All providers failed
	s.log.Error("payout failed after trying all providers",
		zap.String("payout_id", payout.ID.String()),
		zap.Strings("tried_providers", providerNames(result.TriedProviders)),
	)

	// Store which provider was last tried (for debugging)
	var lastProvider domain.Provider
	if len(result.TriedProviders) > 0 {
		lastProvider = result.TriedProviders[len(result.TriedProviders)-1]
	}

	updated, err := s.payoutRepo.UpdatePayoutState(ctx, payout.ID, domain.UpdatePayoutParams{
		State:    domain.PayoutStateFailed,
		Provider: lastProvider,
	})
	if err != nil {
		s.log.Error("failed to mark payout as failed", zap.String("payout_id", payout.ID.String()), zap.Error(err))
		return payout
	}
	return updated
}

// providerNames converts provider slice to strings for logging.
func providerNames(providers []domain.Provider) []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = string(p)
	}
	return names
}

func (s *PayoutServiceServer) GetPayout(
	ctx context.Context,
	req *orchestratorv1.GetPayoutRequest,
) (*orchestratorv1.GetPayoutResponse, error) {
	requestID := grpcutil.GetRequestID(ctx)

	if req.GetPayoutId() == "" {
		return nil, status.Error(codes.InvalidArgument, "payout_id is required")
	}

	id, err := uuid.Parse(req.GetPayoutId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, errors.CodeInvalidUUID)
	}

	payout, err := s.payoutRepo.GetPayout(ctx, id)
	if err != nil {
		if stderrors.Is(err, postgres.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, errors.CodeNotFound)
		}
		return nil, status.Errorf(codes.Internal, "get payout: %v", err)
	}

	s.log.Info("GetPayout",
		zap.String("request_id", requestID),
		zap.String("payout_id", payout.ID.String()),
	)

	resp := &orchestratorv1.GetPayoutResponse{
		PayoutId: payout.ID.String(),
		Status:   string(payout.State),
		Amount:   payout.AmountCents,
		Currency: payout.Currency,
		Provider: string(payout.Provider),
		Rail:     string(payout.Rail),
	}
	if payout.ExternalID != nil {
		resp.ExternalId = *payout.ExternalID
	}
	return resp, nil
}

// cancelableStates is the set of states from which a payout may be canceled.
var cancelableStates = []domain.PayoutState{
	domain.PayoutStateCreated,
	domain.PayoutStateQueued,
	domain.PayoutStateSent,
	domain.PayoutStatePending,
}

func (s *PayoutServiceServer) CancelPayout(
	ctx context.Context,
	req *orchestratorv1.CancelPayoutRequest,
) (*orchestratorv1.CancelPayoutResponse, error) {
	requestID := grpcutil.GetRequestID(ctx)

	if req.GetPayoutId() == "" {
		return nil, status.Error(codes.InvalidArgument, "payout_id is required")
	}

	id, err := uuid.Parse(req.GetPayoutId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, errors.CodeInvalidUUID)
	}

	// Get payout first to check state and provider
	payout, err := s.payoutRepo.GetPayout(ctx, id)
	if err != nil {
		if stderrors.Is(err, postgres.ErrNotFound) {
			return nil, status.Error(codes.NotFound, errors.CodeNotFound)
		}
		return nil, status.Errorf(codes.Internal, "get payout: %v", err)
	}

	// Check if state is cancelable
	isCancelable := false
	for _, s := range cancelableStates {
		if payout.State == s {
			isCancelable = true
			break
		}
	}
	if !isCancelable {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: payout cannot be canceled in state %q",
			errors.CodeInvalidState, payout.State)
	}

	// Try to cancel with provider if orchestrator is configured
	if s.orchestrator != nil && payout.Provider != "" {
		if err := s.orchestrator.CancelPayout(ctx, payout); err != nil {
			// Log but continue - we still want to update our DB state
			s.log.Warn("provider cancel failed, continuing with DB update",
				zap.String("payout_id", id.String()),
				zap.String("provider", string(payout.Provider)),
				zap.Error(err),
			)
		}
	}

	// Update DB state
	_, err = s.payoutRepo.CancelPayout(ctx, id, cancelableStates)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "cancel payout: %v", err)
	}

	s.log.Info("CancelPayout", zap.String("request_id", requestID), zap.String("payout_id", req.GetPayoutId()))
	return &orchestratorv1.CancelPayoutResponse{Success: true}, nil
}

func (s *PayoutServiceServer) ApplyProviderUpdate(
	ctx context.Context,
	req *orchestratorv1.ApplyProviderUpdateRequest,
) (*orchestratorv1.ApplyProviderUpdateResponse, error) {
	if req.GetPayoutId() == "" {
		return nil, status.Error(codes.InvalidArgument, "payout_id is required")
	}

	s.log.Info("ApplyProviderUpdate",
		zap.String("payout_id", req.GetPayoutId()),
		zap.String("provider_status", req.GetProviderStatus()),
		zap.String("provider_reference", req.GetProviderReference()),
	)

	// Find payout by external_id (provider reference like pi_xxx)
	payout, err := s.findPayoutByExternalIDOrID(ctx, req.GetPayoutId())
	if err != nil {
		return nil, err
	}

	// Map provider status to internal state
	newState, err := mapProviderStatusToState(req.GetProviderStatus())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unknown provider status: %s", req.GetProviderStatus())
	}

	// Update the payout state (preserve external_id and provider)
	updated, err := s.payoutRepo.UpdatePayoutState(ctx, payout.ID, domain.UpdatePayoutParams{
		State:      newState,
		ExternalID: payout.ExternalID,
		Provider:   payout.Provider,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update payout state: %v", err)
	}

	s.log.Info("payout state updated via provider webhook",
		zap.String("payout_id", updated.ID.String()),
		zap.String("external_id", req.GetPayoutId()),
		zap.String("old_state", string(payout.State)),
		zap.String("new_state", string(updated.State)),
	)

	return &orchestratorv1.ApplyProviderUpdateResponse{Success: true}, nil
}

// findPayoutByExternalIDOrID looks up a payout by external_id first (provider reference),
// then by internal UUID if not found and the reference looks like a UUID.
func (s *PayoutServiceServer) findPayoutByExternalIDOrID(ctx context.Context, ref string) (domain.Payout, error) {
	// Try to find by external_id first (e.g., "pi_xxx" from Stripe or "mock-ext-xxx" from test)
	payout, err := s.payoutRepo.FindByExternalID(ctx, ref)
	if err == nil {
		return payout, nil
	}

	// If not found by external_id, try as internal UUID only if it parses as UUID
	id, parseErr := uuid.Parse(ref)
	if parseErr != nil {
		// Not a valid UUID and not found by external_id -> not found
		return domain.Payout{}, status.Errorf(codes.NotFound, "payout %s not found", ref)
	}

	payout, err = s.payoutRepo.GetPayout(ctx, id)
	if err != nil {
		if stderrors.Is(err, postgres.ErrNotFound) {
			return domain.Payout{}, status.Errorf(codes.NotFound, "payout %s not found", ref)
		}
		return domain.Payout{}, status.Errorf(codes.Internal, "get payout: %v", err)
	}
	return payout, nil
}

// mapProviderStatusToState converts webhook provider_status to internal PayoutState.
func mapProviderStatusToState(providerStatus string) (domain.PayoutState, error) {
	switch providerStatus {
	case "payout_succeeded":
		return domain.PayoutStateCompleted, nil
	case "payout_failed":
		return domain.PayoutStateFailed, nil
	case "payout_canceled":
		return domain.PayoutStateCanceled, nil
	default:
		return "", fmt.Errorf("unknown provider status: %s", providerStatus)
	}
}
