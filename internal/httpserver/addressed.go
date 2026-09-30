package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/people"
	"github.com/pflege-de-labs/teamster/internal/routing"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

// peopleFinder turns an address into a person the bot can reach (ADR 0063);
// people.Finder is one.
type peopleFinder interface {
	Resolve(ctx context.Context, address string) (models.DirectoryUser, error)
	Ensure(ctx context.Context, u models.DirectoryUser, verify, install bool) (models.DirectoryUser, string, error)
}

// An Option configures a Server beyond what every deployment needs.
type Option func(*Server)

// WithPeople lets messages name the people an addressed route delivers to.
func WithPeople(p peopleFinder) Option {
	return func(s *Server) { s.people = p }
}

// personKeyPrefix marks a claim row in active_event_recipients as a directory
// user's rather than a linked recipient's (ADR 0064).
const personKeyPrefix = "aad:"

// reasonNoRecipient is an addressed route reached by a message that names
// nobody.
const reasonNoRecipient = "no-recipient"

// reasonBlocked is a person who blocked or removed the bot.
const reasonBlocked = "blocked"

// errTooManyRecipients refuses a message naming more people than
// webhook.max-recipients, before anything is delivered.
var errTooManyRecipients = errors.New("the message names more recipients than webhook.max-recipients allows")

// undelivered is one person a message could not reach, and why. The reason
// is permanent: sending the same message again gets the same answer.
type undelivered struct {
	Recipient string `json:"recipient"`
	Reason    string `json:"reason"`
}

// report is what delivering one event came to, beyond the error.
type report struct {
	delivered   int
	undelivered []undelivered
}

func (r *report) add(other report) {
	r.delivered += other.delivered
	r.undelivered = append(r.undelivered, other.undelivered...)
}

// writeReport answers a webhook: 200 when everything arrived, 200 "partial"
// when some people could not be reached for good, 422 when nobody could, and
// the error otherwise (ADR 0063).
func writeReport(w http.ResponseWriter, r *http.Request, rep report, err error) {
	switch {
	case errors.Is(err, errTooManyRecipients):
		writeError(w, r, http.StatusBadRequest, err)
	case err != nil:
		writeError(w, r, http.StatusBadGateway, err)
	case len(rep.undelivered) == 0:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		status, word := http.StatusOK, "partial"
		if rep.delivered == 0 {
			status, word = http.StatusUnprocessableEntity, "undelivered"
		}
		writeJSON(w, status, map[string]any{"status": word, "delivered": rep.delivered, "undelivered": rep.undelivered})
	}
}

// checkRecipients refuses a message that names too many people.
func (s *Server) checkRecipients(ev models.Event) error {
	limit := s.cfg.Webhook.MaxRecipients
	if n := len(models.AddressesOf(ev)); limit > 0 && n > limit {
		return fmt.Errorf("%w: %d named, %d allowed", errTooManyRecipients, n, limit)
	}
	return nil
}

// resolution is one address resolved, remembered for the rest of the event.
type resolution struct {
	user models.DirectoryUser
	err  error
}

// expandAddressed replaces each addressed delivery with one per person the
// event names. A person who cannot be reached for good is reported, not
// failed; an address that fails for a reason a retry may fix is an error.
// Without a people finder the delivery is kept, and fails when delivered.
func (s *Server) expandAddressed(ctx context.Context, ev models.Event, plan []routing.Delivery) ([]routing.Delivery, report, map[string]string, error) {
	var (
		out      = make([]routing.Delivery, 0, len(plan))
		rep      report
		failures []error
		// addressOf is the address each person was named by, for the report.
		addressOf = map[string]string{}
		resolved  = map[string]resolution{}
		budget    = s.cfg.Bot.InlineInstallBudget
	)
	addresses := models.AddressesOf(ev)
	for _, delivery := range plan {
		if delivery.Kind != routing.DeliveryAddressed || s.people == nil {
			out = append(out, delivery)
			continue
		}
		if len(addresses) == 0 {
			s.miss(ctx, &rep, delivery, "", reasonNoRecipient)
			continue
		}
		seen := map[string]bool{}
		for _, address := range addresses {
			u, err := s.resolvePerson(ctx, address, resolved, &budget)
			if err != nil {
				if reason := people.Reason(err); reason != "" {
					s.miss(ctx, &rep, delivery, address, reason)
					continue
				}
				failures = append(failures, fmt.Errorf("recipient %s: %w", address, err))
				continue
			}
			if seen[u.AADObjectID] {
				continue
			}
			seen[u.AADObjectID] = true
			addressOf[u.AADObjectID] = address
			person := delivery
			person.PersonID = u.AADObjectID
			out = append(out, person)
		}
	}
	return out, rep, addressOf, errors.Join(failures...)
}

// resolvePerson finds and, within the event's install budget, installs for
// one address.
func (s *Server) resolvePerson(ctx context.Context, address string, resolved map[string]resolution, budget *int) (models.DirectoryUser, error) {
	key := strings.ToLower(address)
	if r, ok := resolved[key]; ok {
		return r.user, r.err
	}
	u, err := s.people.Resolve(ctx, address)
	if err == nil {
		needsChat := u.InstallState != models.InstallInstalled || u.ConversationID == ""
		install := needsChat && *budget > 0 && s.cfg.Bot.GlobalInstall
		if install {
			*budget--
		}
		u, _, err = s.people.Ensure(ctx, u, false, install)
	}
	resolved[key] = resolution{user: u, err: err}
	return u, err
}

func (s *Server) miss(ctx context.Context, rep *report, delivery routing.Delivery, address, reason string) {
	s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), reason)
	rep.undelivered = append(rep.undelivered, undelivered{Recipient: address, Reason: reason})
}

// deliverEvent delivers an event's plan: every delivery to a channel or a
// linked person one after the other as before, and the people an addressed
// route was expanded to in parallel, at most webhook.fanout-concurrency at a
// time. A person who blocked the bot is reported rather than failed.
func (s *Server) deliverEvent(ctx context.Context, ev models.Event, plan []routing.Delivery, addressOf map[string]string, once bool) (report, error) {
	deliverFn := s.deliver
	if once {
		deliverFn = s.deliverOnce
	}

	var (
		rep      report
		failures []error
		persons  []routing.Delivery
	)
	for _, delivery := range plan {
		if delivery.PersonID != "" {
			persons = append(persons, delivery)
			continue
		}
		if err := deliverFn(ctx, ev, delivery); err != nil {
			failures = append(failures, fmt.Errorf("route %s: %w", delivery.RouteName, err))
			continue
		}
		rep.delivered++
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, max(s.cfg.Webhook.FanoutConcurrency, 1))
	for _, delivery := range persons {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			err := deliverFn(ctx, ev, delivery)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				rep.delivered++
			case recipientBlocked(err):
				rep.undelivered = append(rep.undelivered, undelivered{Recipient: addressOf[delivery.PersonID], Reason: reasonBlocked})
			default:
				failures = append(failures, fmt.Errorf("route %s, recipient %s: %w", delivery.RouteName, addressOf[delivery.PersonID], err))
			}
		}()
	}
	wg.Wait()
	return rep, errors.Join(failures...)
}

// deliverToPerson sends one addressed delivery to the person it was expanded
// to, rendered for them, through the same claim state machine as a linked
// recipient's chat.
func (s *Server) deliverToPerson(ctx context.Context, ev models.Event, delivery routing.Delivery, once bool) error {
	if s.bot == nil {
		return errNoBotConfigured
	}
	u, err := s.store.GetDirectoryUser(ctx, delivery.PersonID)
	if err != nil {
		return fmt.Errorf("directory user: %w", err)
	}
	target := s.personChat(u)
	rendered, err := s.renderMessageFor(ctx, ev, delivery, target.person)
	if err != nil {
		return err
	}
	msg, err := s.chatMessage(rendered)
	if err != nil {
		return err
	}
	if once {
		return s.sendToChatOnce(ctx, delivery, msg, target)
	}
	return s.sendToChat(ctx, ev, delivery, msg, target)
}

// personChat is a directory user's chat, keyed apart from recipients.
func (s *Server) personChat(u models.DirectoryUser) chatTarget {
	serviceURL := u.ServiceURL
	if serviceURL == "" {
		serviceURL = s.cfg.Bot.ServiceURL
	}
	oid := u.AADObjectID
	return chatTarget{
		key: personKeyPrefix + oid,
		ref: bot.ConversationReference{
			ServiceURL: serviceURL, ConversationID: u.ConversationID, BotChannelID: "msteams",
			TenantID: u.TenantID, AADObjectID: oid,
		},
		person: templates.Person{
			ID: oid, DisplayName: u.DisplayName, GivenName: u.GivenName, Surname: u.Surname,
			UPN: u.UserPrincipalName, Mail: u.Mail,
		},
		markBlocked: func(ctx context.Context, cause error) {
			if err := s.store.MarkDirectoryUserBlocked(ctx, oid, s.now(), blockedReason(cause)); err != nil {
				logError(ctx, "mark directory user blocked", err)
			}
		},
		clearBlocked: func(ctx context.Context) {
			if err := s.store.ClearDirectoryUserBlocked(ctx, oid); err != nil {
				logError(ctx, "clear directory user blocked", err)
			}
		},
	}
}
