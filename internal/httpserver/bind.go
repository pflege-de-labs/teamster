package httpserver

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/people"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// bindState is how far finding a signed-in user's own Teams chat got.
type bindState string

const (
	bindLinked bindState = "linked"
	// bindNoAccount: nothing the sign-in said matched a directory user.
	bindNoAccount bindState = "no_account"
	// bindNoChat: the user was found, but the bot has no chat with them yet.
	bindNoChat bindState = "no_chat"
	// bindLocal: a local login says nothing about a Teams account.
	bindLocal bindState = "local"
)

// managedChats is whether the app is installed for everyone and chats are
// bound from sign-in rather than link codes (ADR 0065).
func (s *Server) managedChats() bool {
	return s.cfg.Bot.GlobalInstall && s.people != nil
}

// bindOwnChat finds the session holder's directory user and binds their chat
// to the session's subject as a recipient, so a route to "yourself" reaches
// it. install lets it install the app when there is no chat yet. It matches,
// in order, the Entra object id, the username as a UPN, and a verified email;
// a local login never binds.
func (s *Server) bindOwnChat(ctx context.Context, session models.Session, install bool) (models.Recipient, bindState, error) {
	if session.Source == "local" {
		return models.Recipient{}, bindLocal, nil
	}
	u, found, err := s.findOwnDirectoryUser(ctx, session.Identity)
	if err != nil {
		return models.Recipient{}, "", err
	}
	if !found {
		return models.Recipient{}, bindNoAccount, nil
	}

	u, _, err = s.people.Ensure(ctx, u, false, install)
	if errors.Is(err, people.ErrNotInstalled) {
		return models.Recipient{}, bindNoChat, nil
	}
	if err != nil {
		return models.Recipient{}, "", err
	}

	recipient := models.Recipient{
		Subject: session.Subject, Name: firstNonBlank(u.DisplayName, session.Name), AADObjectID: u.AADObjectID,
		ConversationID: u.ConversationID, ServiceURL: firstNonBlank(u.ServiceURL, s.cfg.Bot.ServiceURL),
		BotChannelID: "msteams", TenantID: u.TenantID,
	}
	existing, err := s.store.GetRecipientBySubject(ctx, session.Subject)
	switch {
	case err == nil:
		if existing.AADObjectID == recipient.AADObjectID && existing.ConversationID == recipient.ConversationID && existing.ServiceURL == recipient.ServiceURL {
			return existing, bindLinked, nil
		}
		recipient.ID = existing.ID
		recipient, err = s.store.UpdateRecipient(ctx, recipient)
	case errors.Is(err, store.ErrNotFound):
		recipient, err = s.store.CreateRecipient(ctx, recipient)
	}
	if err != nil {
		return models.Recipient{}, "", err
	}
	return recipient, bindLinked, nil
}

// findOwnDirectoryUser matches what the sign-in said against the directory.
// A match by username has to be the UPN itself, and one by email needs the
// provider to vouch for the address: the directory also matches aliases, and
// an unverified email is anybody's to claim.
func (s *Server) findOwnDirectoryUser(ctx context.Context, identity models.Identity) (models.DirectoryUser, bool, error) {
	try := func(address string, accept func(models.DirectoryUser) bool) (models.DirectoryUser, bool, error) {
		if address == "" {
			return models.DirectoryUser{}, false, nil
		}
		u, err := s.people.Resolve(ctx, address)
		if people.Reason(err) != "" {
			return models.DirectoryUser{}, false, nil
		}
		if err != nil {
			return models.DirectoryUser{}, false, err
		}
		return u, accept(u), nil
	}

	if u, ok, err := try(identity.ObjectID, func(u models.DirectoryUser) bool {
		return strings.EqualFold(u.AADObjectID, identity.ObjectID)
	}); ok || err != nil {
		return u, ok, err
	}
	if strings.Contains(identity.Username, "@") {
		if u, ok, err := try(identity.Username, func(u models.DirectoryUser) bool {
			return strings.EqualFold(u.UserPrincipalName, identity.Username)
		}); ok || err != nil {
			return u, ok, err
		}
	}
	if identity.EmailVerified {
		return try(identity.Email, func(models.DirectoryUser) bool { return true })
	}
	return models.DirectoryUser{}, false, nil
}

// followPersonsChat moves the recipients bound to a person onto the chat the
// bot has with them now.
func (s *Server) followPersonsChat(ctx context.Context, oid, conversationID, serviceURL string) {
	if _, err := s.store.UpdateRecipientChatsForObjectID(ctx, oid, conversationID, serviceURL, time.Now().UTC()); err != nil {
		logError(ctx, "move recipients to the current chat", err)
	}
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
