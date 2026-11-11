package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/logging"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// mcpTokenPrefix marks an MCP token, so a secret scanner can recognise one
// and it cannot be mistaken for a webhook token.
const mcpTokenPrefix = "tmcp_"

const mcpTokenKey contextKey = "mcp-token"

// newMCPToken is 256 bits behind a recognisable prefix.
func newMCPToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return mcpTokenPrefix + hex.EncodeToString(raw), nil
}

// mcpAuth admits a request that carries an MCP token as the token's creator,
// as they are now. Only Authorization: Bearer counts; a session or basic auth
// does not open /mcp.
func (s *Server) mcpAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		log := logging.FromContext(ctx)

		provided := bearerToken(r)
		if provided == "" {
			s.mcpUnauthorized(w)
			return
		}
		token, err := s.store.GetMCPTokenByHash(ctx, hashToken(provided))
		if errors.Is(err, store.ErrNotFound) {
			log.Warn("mcp refused", "reason", "unknown token")
			s.mcpUnauthorized(w)
			return
		}
		if err != nil {
			log.Error("mcp: look up token", "err", err)
			writeJSONError(w, http.StatusServiceUnavailable, "cannot check the token right now")
			return
		}

		creator, err := s.tokenCreator(r, token.CreatedBy)
		if errors.Is(err, errTokenScope) {
			log.Warn("mcp refused", "token", token.Name, "creator", token.CreatedBy, "reason", err)
			writeJSONError(w, http.StatusForbidden, errTokenScope.Error())
			return
		}
		if err != nil {
			log.Error("mcp: look up creator", "token", token.Name, "creator", token.CreatedBy, "err", err)
			writeJSONError(w, http.StatusServiceUnavailable, "cannot check the token right now")
			return
		}

		if now := time.Now(); now.Sub(token.LastUsedAt) > touchInterval {
			// Best effort: a failed bookkeeping write must not refuse the call.
			if err := s.store.TouchMCPToken(ctx, token.ID, now); err != nil {
				log.Error("record use of mcp token", "token", token.Name, "err", err)
			}
		}

		// tokenCreator returns no display name; the local admin has no row, so fall back to the subject.
		name := creator.Subject
		if user, err := s.store.GetUser(ctx, creator.Subject); err == nil && user.Name != "" {
			name = user.Name
		}
		ctx = withPrincipal(ctx, creator.Subject, name, models.ViaMCP, creator.Roles, creator.IdPGroups)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, mcpTokenKey, token)))
	})
}

func (s *Server) mcpUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
	w.WriteHeader(http.StatusUnauthorized)
}

// mcpTokenOf is the token mcpAuth admitted.
func mcpTokenOf(ctx context.Context) (models.MCPToken, bool) {
	token, ok := ctx.Value(mcpTokenKey).(models.MCPToken)
	return token, ok
}

// mcpWrites says whether tools that change things may run: neither the
// deployment nor the token may hold them back.
func mcpWrites(ctx context.Context, cfg config.MCPConfig) bool {
	token, ok := mcpTokenOf(ctx)
	return ok && !cfg.ReadOnly && token.Writes()
}
