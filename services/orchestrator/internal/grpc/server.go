package grpc

import (
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	orchestratorv1 "github.com/bashkirian/fintech-project/libs/genproto/orchestrator/v1"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
)

// Server wraps a gRPC server with health checking and reflection.
type Server struct {
	srv *grpc.Server
	log *zap.Logger
}

func New(log *zap.Logger, pool *pgxpool.Pool, orchestrator *provider.Orchestrator) *Server {
	srv := grpc.NewServer()

	orchestratorv1.RegisterPayoutServiceServer(srv, newPayoutServiceServer(log, pool, orchestrator))

	healthSvc := health.NewServer()
	grpc_health_v1.RegisterHealthServer(srv, healthSvc)
	healthSvc.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	reflection.Register(srv)

	return &Server{srv: srv, log: log}
}

func (s *Server) Serve(lis net.Listener) error {
	s.log.Info("starting grpc server", zap.String("addr", lis.Addr().String()))
	return s.srv.Serve(lis)
}

func (s *Server) GracefulStop() {
	s.log.Info("stopping grpc server")
	s.srv.GracefulStop()
}
