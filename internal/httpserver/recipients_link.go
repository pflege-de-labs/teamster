package httpserver

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// linkFlowTTL mirrors loginFlowTTL: the code sits in a Teams chat transcript
// once sent, so it is short-lived for the same reason an OIDC state is.
const linkFlowTTL = 10 * time.Minute

// linkCodeAlphabet excludes 0/O and 1/I/L, the pairs a person reading a code
// off a phone screen confuses most often.
const linkCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// linkCodeGroups and linkCodeGroupLen give twelve symbols from the 31-symbol
// linkCodeAlphabet, ~4.95 bits each: ~59.4 bits, comfortably past the "~50
// bits" a code living in a chat transcript needs, displayed as three dashed
// groups of four the way a product key is.
const (
	linkCodeGroups   = 3
	linkCodeGroupLen = 4
)

// maxLinkCodeAttempts bounds the retry when a freshly generated code collides
// with one already in the table. A collision surviving this many attempts is
// not bad luck any more.
const maxLinkCodeAttempts = 5

// generateLinkCode draws each symbol independently from crypto/rand via
// math/big's rejection-sampling Int, rather than a modulo on a byte: the
// alphabet's 31 symbols do not divide evenly into 256, and rejection sampling
// is what keeps the draw unbiased regardless of the alphabet's size.
//
// The result carries no dashes: it is what gets stored and what a redemption
// looks up by, both after normalizeLinkCode has stripped whatever grouping the
// person typed back in. groupCode adds them back only for display.
func generateLinkCode() (string, error) {
	const length = linkCodeGroups * linkCodeGroupLen
	alphabetSize := big.NewInt(int64(len(linkCodeAlphabet)))

	symbols := make([]byte, length)
	for i := range symbols {
		n, err := rand.Int(rand.Reader, alphabetSize)
		if err != nil {
			return "", fmt.Errorf("draw link code symbol: %w", err)
		}
		symbols[i] = linkCodeAlphabet[n.Int64()]
	}
	return string(symbols), nil
}

// groupCode is generateLinkCode's inverse for display: dashes every
// linkCodeGroupLen symbols, the way a product key is shown.
func groupCode(code string) string {
	var grouped strings.Builder
	for i := 0; i < len(code); i += linkCodeGroupLen {
		if i > 0 {
			grouped.WriteByte('-')
		}
		end := min(i+linkCodeGroupLen, len(code))
		grouped.WriteString(code[i:end])
	}
	return grouped.String()
}

// createLinkFlow mints a code for subject, first retiring whatever it already
// had outstanding: without that, old codes pile up until the hourly sweep
// catches them, and every live one widens the surface a guess has to beat.
func (s *Server) createLinkFlow(ctx context.Context, subject string) (models.LinkFlow, error) {
	if err := s.store.DeleteLinkFlowsForSubject(ctx, subject); err != nil {
		return models.LinkFlow{}, fmt.Errorf("retire outstanding codes: %w", err)
	}

	expiresAt := time.Now().UTC().Add(linkFlowTTL)
	for attempt := 0; attempt < maxLinkCodeAttempts; attempt++ {
		code, err := generateLinkCode()
		if err != nil {
			return models.LinkFlow{}, err
		}

		flow := models.LinkFlow{Code: code, Subject: subject, ExpiresAt: expiresAt}
		err = s.store.CreateLinkFlow(ctx, flow)
		switch {
		case err == nil:
			return flow, nil
		case errors.Is(err, store.ErrConflict):
			// Somebody else's code landed on the same string; try again with a
			// fresh one rather than surfacing a constraint violation as a 500.
			continue
		default:
			return models.LinkFlow{}, fmt.Errorf("create link flow: %w", err)
		}
	}
	return models.LinkFlow{}, fmt.Errorf("could not generate a unique link code after %d attempts", maxLinkCodeAttempts)
}

// handleLinkRecipient mints a one-time code that binds whoever redeems it in
// Teams to the caller's own subject -- never anyone else's, which is why a
// basic-auth caller is refused below rather than handed a code for the shared
// admin account.
func (s *Server) handleLinkRecipient(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()

	// basicAuth sets the principal's subject to the *configured admin
	// username*, with RoleAdmin, regardless of who actually holds the shared
	// password. Minting a code under that subject would bind a chat to "the
	// admin account" rather than to whoever is asking, which is exactly the
	// operator-acts-on-someone's-behalf shape ADR 0026 rejects -- and it would
	// collide with any identity provider that happens to emit sub: "admin".
	// currentSession reports the real thing: a cookie naming a session row,
	// which only a completed sign-in creates.
	session, ok := s.currentSession(r)
	if !ok {
		writeJSONError(w, http.StatusForbidden, "link codes require a signed-in session, not basic auth")
		return
	}

	flow, err := s.createLinkFlow(ctx, session.Subject)
	if err != nil {
		// Unlike the admin API's own 500s, this endpoint is reachable by a
		// viewer, so the wrapped store error stays in the log rather than the
		// response.
		logError("create link flow", err)
		writeJSONError(w, http.StatusInternalServerError, "could not mint a link code")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"code":       groupCode(flow.Code),
		"expires_at": flow.ExpiresAt.Format(time.RFC3339),
	})
}
