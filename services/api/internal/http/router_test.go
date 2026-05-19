package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	apiconfig "github.com/bashkirian/fintech-project/services/api/internal/config"
)

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	cfg := apiconfig.Config{RateLimitEnabled: false}
	router := NewRouterWithClient(zap.NewNop(), nil, nil, cfg)
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
}
