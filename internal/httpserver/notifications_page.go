package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// handleNotificationsPage is the self-service opt-in page: whether this
// session's own chat is linked, and the controls to link or unlink it. It
// sits behind requireSession like every other admin page, but unlike most of
// them is offered to a viewer too -- this is a person managing their own
// membership, not administering anyone else's.
func (s *Server) handleNotificationsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.renderNotifications(w, r, s.notificationsPage(r))
}

// notificationsQueryNotice and notificationsQueryError translate a closed set
// of ?notice=/?error= values into the text this page shows, rather than
// rendering whatever arrived on the query string. Every other admin page's
// redirect-with-notice carries a message this service itself composed, but a
// bare link straight to this URL is exactly the shape a phishing attempt
// takes -- "click here, then send the code below to the bot" -- rendered in
// this page's own role="alert" styling to whoever is signed in and follows
// it. An unrecognised value renders nothing rather than the value itself.
func notificationsQueryNotice(ctx context.Context, key string) string {
	switch key {
	case "unlinked":
		return i18n.T(ctx, "notifications.unlinked")
	case "canceled":
		return i18n.T(ctx, "notifications.canceled")
	default:
		return ""
	}
}

func notificationsQueryError(ctx context.Context, key string) string {
	switch key {
	case "sign_in_required":
		return i18n.T(ctx, "notifications.sign_in_required")
	case "nothing_to_unlink":
		return i18n.T(ctx, "notifications.nothing_to_unlink")
	case "unlink_failed":
		return i18n.T(ctx, "notifications.unlink_failed")
	case "cancel_failed":
		return i18n.T(ctx, "notifications.cancel_failed")
	default:
		return ""
	}
}

// notificationsPage loads what the page shows: the notice or error a redirect
// carried, and whether this subject -- not any id a caller could name -- has
// a linked recipient.
func (s *Server) notificationsPage(r *http.Request) views.Notifications {
	ctx := r.Context()
	page := views.Notifications{
		Viewer: s.viewerFor(r),
		Notice: notificationsQueryNotice(ctx, r.URL.Query().Get("notice")),
		Error:  notificationsQueryError(ctx, r.URL.Query().Get("error")),
	}

	session, ok := s.currentSession(r)
	if !ok {
		// requireSession already guarantees a session for every path under
		// /admin/; this only guards against that wrapper ever being bypassed.
		return page
	}

	recipient, err := s.store.GetRecipientBySubject(ctx, session.Subject)
	switch {
	case err == nil:
		page.Recipient = &recipient
	case isNotFound(err):
		// Not linked yet, which is the ordinary case the page's own copy
		// explains -- not a failure to report.
	default:
		logError("get recipient by subject", err)
		// StatusUnknown, not just Error, because Recipient == nil alone reads
		// as "not linked" everywhere else in this file and the template --
		// which is exactly the wrong thing to tell someone who is actually
		// linked but whose lookup just failed.
		page.Error = i18n.T(ctx, "notifications.load_failed")
		page.StatusUnknown = true
	}
	return page
}

func (s *Server) renderNotifications(w http.ResponseWriter, r *http.Request, page views.Notifications) {
	// Every response here can show a live link code, or who currently holds
	// one. No-store is what actually keeps that out of a shared machine or a
	// signed-out browser's Back button: unlike the URL or an access log, it
	// is the response *body* a cache or history entry would replay, and this
	// inline render is the body.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.NotificationsPage(page).Render(r.Context(), w); err != nil {
		logError("render notifications page", err)
	}
}

// handleMintLink mints a link code for the caller's own subject and renders
// the result inline, in this same response, rather than through the
// redirect-with-notice pattern every other admin form uses. A code is live
// for linkFlowTTL; putting it in a redirect's query string would leave it
// sitting in the URL and any access log for that whole window, which a page
// showing a person their own credential should not do -- and unlike those
// two, the no-store header above is what keeps it out of the third place a
// query string would otherwise sit: the browser's own history.
func (s *Server) handleMintLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// The same CSRF guard formPost applies to every other write under
	// /admin/: a session cookie travels with a cross-site request, and there
	// is no token to hang a check on besides where the request came from.
	if !sameOrigin(r) {
		http.Error(w, i18n.T(r.Context(), "notifications.cross_origin_rejected"), http.StatusForbidden)
		return
	}

	ctx := r.Context()
	session, ok := s.currentSession(r)
	if !ok {
		http.Error(w, i18n.T(ctx, "notifications.sign_in_required"), http.StatusForbidden)
		return
	}

	page := s.notificationsPage(r)
	// A load failure notificationsPage already reported takes priority over
	// anything minting decides below: minting a code says nothing about
	// whether that earlier lookup can be trusted, so it must not be cleared
	// or overwritten by a mint outcome unrelated to it.
	if !page.StatusUnknown {
		page.Notice = ""
		page.Error = ""
	}

	flow, err := s.createLinkFlow(ctx, session.Subject)
	switch {
	case err != nil:
		logError("create link flow", err)
		if !page.StatusUnknown {
			page.Error = i18n.T(ctx, "notifications.mint_failed")
		}
	default:
		page.Minted = &views.MintedLink{Code: groupCode(flow.Code), ExpiresAt: flow.ExpiresAt}
	}
	s.renderNotifications(w, r, page)
}

// cancelLink invalidates every code the caller currently has outstanding,
// resolved from the session the same way unlinkNotifications resolves whose
// recipient to remove. It is the only way to invalidate a code once minted:
// minting a fresh one supersedes the old one (createLinkFlow retires it) but
// only once a new one exists, which is no help if the old one was pasted into
// the wrong window and nobody wants a replacement yet. Available regardless
// of whether the caller is currently linked -- a code can be outstanding
// either way.
func (s *Server) cancelLink(r *http.Request) (string, error) {
	session, ok := s.currentSession(r)
	if !ok {
		return "", errors.New("sign_in_required")
	}
	if err := s.store.DeleteLinkFlowsForSubject(r.Context(), session.Subject); err != nil {
		logError("cancel link flows", err)
		return "", errors.New("cancel_failed")
	}
	return "canceled", nil
}

// unlinkNotifications removes the caller's own recipient, resolved the same
// way notificationsPage resolves it -- by the session's subject, never by an
// id read from the form. A form field naming another row is exactly what an
// attacker would try, and there is deliberately nothing here that reads one.
//
// The notice and error values returned are closed-set keys, not the
// user-facing text itself: formPostTo round-trips whatever is returned here
// through a redirect's query string, and notificationsQueryNotice /
// notificationsQueryError are what turn a recognised key back into text on
// the way out. See their comment for why free text never makes that trip on
// this page.
func (s *Server) unlinkNotifications(r *http.Request) (string, error) {
	ctx := r.Context()
	session, ok := s.currentSession(r)
	if !ok {
		return "", errors.New("sign_in_required")
	}

	recipient, err := s.store.GetRecipientBySubject(ctx, session.Subject)
	if err != nil {
		if isNotFound(err) {
			return "", errors.New("nothing_to_unlink")
		}
		logError("get recipient by subject for unlink", err)
		return "", errors.New("unlink_failed")
	}

	if err := s.store.DeleteRecipient(ctx, recipient.ID); err != nil {
		logError("delete recipient", err)
		return "", errors.New("unlink_failed")
	}

	s.notifyUnlinked(ctx, recipient)
	return "unlinked", nil
}

// notifyUnlinked is best-effort, the same shape as notifyConversationDisplaced:
// the unlink has already committed, so a failure to tell the chat about it is
// logged rather than allowed to undo it.
func (s *Server) notifyUnlinked(ctx context.Context, recipient models.Recipient) {
	if s.bot == nil {
		return
	}
	ref := bot.ConversationReference{
		ServiceURL:     recipient.ServiceURL,
		ConversationID: recipient.ConversationID,
		BotChannelID:   recipient.BotChannelID,
		TenantID:       recipient.TenantID,
		AADObjectID:    recipient.AADObjectID,
	}
	if _, err := s.bot.SendMessage(ctx, ref, bot.Message{Text: linkRemovedReply}); err != nil {
		logError("bot unlink notice", fmt.Errorf("conversation %s: %w", recipient.ConversationID, err))
	}
}
