package grpc

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/persistence/postgres"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/persistence/sqlcgen"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
)

// errIdempotencyKeyExists is a sentinel used to signal a key conflict inside RunInTx.
var errIdempotencyKeyExists = errors.New("idempotency key already exists")

// txResult carries data captured inside the transaction closure.
type txResult struct {
	newPayout   domain.Payout
	existingKey domain.IdempotencyKey
	inserted    bool
}

// PayoutServiceServer implements orchestratorv1.PayoutServiceServer.
type PayoutServiceServer struct {
	orchestratorv1.UnimplementedPayoutServiceServer
	log        *zap.Logger
	pool       *pgxpool.Pool
	payoutRepo domain.PayoutRepository
	registry   *provider.Registry
}

func newPayoutServiceServer(log *zap.Logger, pool *pgxpool.Pool, registry *provider.Registry) *PayoutServiceServer {
	return &PayoutServiceServer{
		log:        log,
		pool:       pool,
		payoutRepo: postgres.NewPayoutRepo(sqlcgen.New(pool)),
		registry:   registry,
	}
}

// railToProvider maps a payment rail to its default provider.
func railToProvider(rail domain.Rail) domain.Provider {
	switch rail {
	case domain.RailCrypto:
		return domain.ProviderCryptoSim
	default:
		return domain.ProviderStripe
	}
}

func (s *PayoutServiceServer) CreatePayout(
	ctx context.Context,
	req *orchestratorv1.CreatePayoutRequest,
) (*orchestratorv1.CreatePayoutResponse, error) {
	if req.GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	if req.GetRequestHash() == "" {
		return nil, status.Error(codes.InvalidArgument, "request_hash is required")
	}

	s.log.Info("CreatePayout",
		zap.String("idempotency_key", req.GetIdempotencyKey()),
		zap.Int64("amount", req.GetAmount()),
		zap.String("currency", req.GetCurrency()),
		zap.String("rail", req.GetRail()),
	)

	var result txResult

	txErr := postgres.RunInTx(ctx, s.pool, func(ctx context.Context, q sqlcgen.Querier) error {
		rail := domain.Rail(req.GetRail())

		payout, err := postgres.NewPayoutRepo(q).CreatePayout(ctx, domain.CreatePayoutParams{
			State:       domain.PayoutStateCreated,
			AmountCents: req.GetAmount(),
			Currency:    req.GetCurrency(),
			Rail:        rail,
			Provider:    railToProvider(rail),
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
		// Happy path: new payout created — attempt to send via provider.
		payout := result.newPayout
		payout = s.sendPayout(ctx, payout)
		return &orchestratorv1.CreatePayoutResponse{
			PayoutId: payout.ID.String(),
			Status:   string(payout.State),
		}, nil

	case errors.Is(txErr, errIdempotencyKeyExists):
		// Key already exists – check whether this is a replay or a conflict.
		if result.existingKey.RequestHash != req.GetRequestHash() {
			return nil, status.Error(codes.AlreadyExists, "idempotency key reused with different request")
		}
		// Same hash: return the existing payout.
		existing, err := s.payoutRepo.GetPayout(ctx, result.existingKey.PayoutID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "fetch existing payout: %v", err)
		}
		s.log.Info("idempotent replay", zap.String("payout_id", existing.ID.String()))
		return &orchestratorv1.CreatePayoutResponse{
			PayoutId: existing.ID.String(),
			Status:   string(existing.State),
		}, nil

	default:
		return nil, status.Errorf(codes.Internal, "create payout: %v", txErr)
	}
}

// sendPayout looks up the provider for the payout's rail, calls SendPayout,
// and persists the resulting state (sent + external_id, or failed).
// RetryableError is used to distinguish terminal vs transient provider failures
// for logging purposes; both result in a failed state persisted to the DB.
func (s *PayoutServiceServer) sendPayout(ctx context.Context, payout domain.Payout) domain.Payout {
	if s.registry == nil {
		s.log.Error("provider registry not configured", zap.String("payout_id", payout.ID.String()))
		updated, updateErr := s.payoutRepo.UpdatePayoutState(ctx, payout.ID, domain.UpdatePayoutParams{
			State: domain.PayoutStateFailed,
		})
		if updateErr != nil {
			s.log.Error("failed to mark payout as failed", zap.String("payout_id", payout.ID.String()), zap.Error(updateErr))
			return payout
		}
		return updated
	}

	client, err := s.registry.Get(payout.Rail)
	if err != nil {
		s.log.Error("no provider for rail", zap.String("rail", string(payout.Rail)), zap.Error(err))
		updated, updateErr := s.payoutRepo.UpdatePayoutState(ctx, payout.ID, domain.UpdatePayoutParams{
			State: domain.PayoutStateFailed,
		})
		if updateErr != nil {
			s.log.Error("failed to mark payout as failed", zap.String("payout_id", payout.ID.String()), zap.Error(updateErr))
			return payout
		}
		return updated
	}

	extID, sendErr := client.SendPayout(ctx, payout)
	if sendErr != nil {
		var re *provider.RetryableError
		if errors.As(sendErr, &re) && !re.Retryable {
			s.log.Warn("provider rejected payout (non-retryable)",
				zap.String("payout_id", payout.ID.String()),
				zap.Error(sendErr),
			)
		} else {
			s.log.Error("provider error (retryable or unknown)",
				zap.String("payout_id", payout.ID.String()),
				zap.Error(sendErr),
			)
		}
		updated, updateErr := s.payoutRepo.UpdatePayoutState(ctx, payout.ID, domain.UpdatePayoutParams{
			State: domain.PayoutStateFailed,
		})
		if updateErr != nil {
			s.log.Error("failed to mark payout as failed", zap.String("payout_id", payout.ID.String()), zap.Error(updateErr))
			return payout
		}
		return updated
	}

	updated, updateErr := s.payoutRepo.UpdatePayoutState(ctx, payout.ID, domain.UpdatePayoutParams{
		State:      domain.PayoutStateSent,
		ExternalID: &extID,
	})
	if updateErr != nil {
		s.log.Error("failed to mark payout as sent", zap.String("payout_id", payout.ID.String()), zap.Error(updateErr))
		return payout
	}
	s.log.Info("payout sent", zap.String("payout_id", payout.ID.String()), zap.String("external_id", extID))
	return updated
}

func (s *PayoutServiceServer) GetPayout(
	ctx context.Context,
	req *orchestratorv1.GetPayoutRequest,
) (*orchestratorv1.GetPayoutResponse, error) {
	if req.GetPayoutId() == "" {
		return nil, status.Error(codes.InvalidArgument, "payout_id is required")
	}

	id, err := uuid.Parse(req.GetPayoutId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "payout_id must be a valid UUID")
	}

	payout, err := s.payoutRepo.GetPayout(ctx, id)
	if err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "payout %s not found", req.GetPayoutId())
		}
		return nil, status.Errorf(codes.Internal, "get payout: %v", err)
	}

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
}

func (s *PayoutServiceServer) CancelPayout(
	ctx context.Context,
	req *orchestratorv1.CancelPayoutRequest,
) (*orchestratorv1.CancelPayoutResponse, error) {
	if req.GetPayoutId() == "" {
		return nil, status.Error(codes.InvalidArgument, "payout_id is required")
	}

	id, err := uuid.Parse(req.GetPayoutId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "payout_id must be a valid UUID")
	}

	_, err = s.payoutRepo.CancelPayout(ctx, id, cancelableStates)
	if err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			// ErrNotFound here means either payout doesn't exist OR state is not cancelable.
			// Distinguish by fetching the payout.
			existing, getErr := s.payoutRepo.GetPayout(ctx, id)
			if getErr != nil {
				if errors.Is(getErr, postgres.ErrNotFound) {
					return nil, status.Errorf(codes.NotFound, "payout %s not found", req.GetPayoutId())
				}
				return nil, status.Errorf(codes.Internal, "get payout: %v", getErr)
			}
			return nil, status.Errorf(codes.FailedPrecondition,
				"payout cannot be canceled in state %q", existing.State)
		}
		return nil, status.Errorf(codes.Internal, "cancel payout: %v", err)
	}

	s.log.Info("CancelPayout", zap.String("payout_id", req.GetPayoutId()))
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
