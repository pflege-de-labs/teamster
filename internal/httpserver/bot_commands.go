package httpserver

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/logging"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// Bot commands in the personal chat (ADR 0048), named without a slash
// because Teams shares the slash menu with every app (ADR 0068). A slash
// command may carry words after it; a bare word only counts as the whole
// message, because "help" or "stop" inside a sentence is not a request
// (ADR 0032).
const (
	commandHelp    = "help"
	commandStatus  = "status"
	commandTest    = "test"
	commandUnlink  = "unlink"
	commandUnknown = ""
)

const (
	helpReply = "**Teamster commands**\n\n" +
		"- `help` — this list\n" +
		"- `status` — whether this chat is linked, and which routes deliver to it\n" +
		"- `test` — send a test alert to this chat\n" +
		"- `unlink` — stop alerts arriving in this chat\n\n" +
		"To link this chat, send me the code from **Notifications** in the Teamster admin UI."
	// managedHelpReply is the list while the app is installed for everyone:
	// nobody links or unlinks a chat then (ADR 0061).
	managedHelpReply = "**Teamster commands**\n\n" +
		"- `help` — this list\n" +
		"- `status` — what reaches this chat\n" +
		"- `test` — send a test message to this chat"
	managedUnlinkReply = "Your IT department sends messages to this chat, so it cannot be " +
		"unlinked. Ask them if you think you should not receive these."
	managedStatus       = "Your IT department sends messages to this chat through Teamster."
	unknownCommandReply = "I don't know that command. Send help for the ones I do."
	notLinkedStatus     = "This chat is **not linked**. Get a link code from **Notifications** in the " +
		"Teamster admin UI and send it to me here."
	testNotLinkedReply = "This chat is not linked yet, so there is nowhere to send a test alert. " +
		"Send me a link code first."
	testTitle = "Teamster test alert"
	testText  = "If you can read this, alerts reach this chat."
)

// bareCommands are the words that are a command when they are the whole
// message. The unlink synonyms predate slash commands and keep working.
var bareCommands = map[string]string{
	commandHelp: commandHelp, commandStatus: commandStatus, commandTest: commandTest,
	commandUnlink: commandUnlink, "stop": commandUnlink, "unsubscribe": commandUnlink,
}

// parseBotCommand finds the command a message asks for, if it asks for one.
// An unknown slash command is still a command, answered with a pointer to
// help, so a typo is not read as a wrong link code.
func parseBotCommand(text string, entities []botEntity) (string, bool) {
	fields := strings.Fields(stripMentions(text, entities))
	if len(fields) == 0 {
		return "", false
	}
	first := strings.ToLower(fields[0])
	if name, ok := strings.CutPrefix(first, "/"); ok {
		if command, known := bareCommands[name]; known {
			return command, true
		}
		return commandUnknown, true
	}
	if len(fields) == 1 {
		command, ok := bareCommands[first]
		return command, ok
	}
	return "", false
}

func (s *Server) handleBotCommand(ctx context.Context, activity botActivity, command string) {
	logging.FromContext(ctx).Info("bot command", "command", command)
	switch command {
	case commandHelp:
		if s.cfg.Bot.GlobalInstall {
			s.replyText(ctx, activity, managedHelpReply)
			return
		}
		s.replyText(ctx, activity, helpReply)
	case commandStatus:
		s.replyText(ctx, activity, s.statusReply(ctx, activity.Conversation.ID))
	case commandTest:
		s.sendTestAlert(ctx, activity)
	case commandUnlink:
		s.unlinkFromChat(ctx, activity)
	default:
		s.replyText(ctx, activity, unknownCommandReply)
	}
}

func (s *Server) unlinkFromChat(ctx context.Context, activity botActivity) {
	if s.cfg.Bot.GlobalInstall {
		s.replyText(ctx, activity, managedUnlinkReply)
		return
	}
	retired, err := s.retireLink(ctx, activity.Conversation.ID, "unlink command")
	switch {
	case err != nil:
		s.replyText(ctx, activity, linkNeutralReply)
	case retired:
		s.replyText(ctx, activity, unlinkConfirmedReply)
	default:
		s.replyText(ctx, activity, unlinkNotLinkedReply)
	}
}

// statusReply describes the link behind this conversation. A store failure
// answers with a reference, the way a page does (ADR 0046).
func (s *Server) statusReply(ctx context.Context, conversationID string) string {
	recipient, err := s.store.GetRecipientByConversation(ctx, conversationID)
	if errors.Is(err, store.ErrNotFound) {
		if s.cfg.Bot.GlobalInstall {
			return managedStatus
		}
		return notLinkedStatus
	}
	if err != nil {
		return failureText(ctx, "bot status", err)
	}
	routes, err := s.store.ListRoutes(ctx)
	if err != nil {
		return failureText(ctx, "bot status", err)
	}
	return formatStatus(recipient, routes)
}

func formatStatus(recipient models.Recipient, routes []models.Route) string {
	var b strings.Builder
	name := recipient.Name
	if name == "" {
		name = recipient.Subject
	}
	fmt.Fprintf(&b, "This chat is **linked** to %s since %s.\n\n", plainMarkdown(name), recipient.CreatedAt.Format("2006-01-02"))
	if recipient.Blocked() {
		fmt.Fprintf(&b, "The last delivery failed (%s) on %s; the next alert tries again.\n\n",
			plainMarkdown(recipient.BlockedReason), recipient.BlockedAt.Format("2006-01-02 15:04 MST"))
	}

	var lines []string
	for _, route := range routes {
		if route.RecipientID != recipient.ID {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: `%s`", plainMarkdown(route.Name), selectorLine(route.LabelSelector)))
	}
	if len(lines) == 0 {
		b.WriteString("No route delivers to this chat yet. Pick it in **Delivers to** on a route in the admin UI.")
		return b.String()
	}
	slices.Sort(lines)
	b.WriteString("**Routes delivering here**\n\n")
	b.WriteString(strings.Join(lines, "\n"))
	return b.String()
}

// selectorLine renders a label selector the way /route will accept it later.
func selectorLine(selector map[string]string) string {
	if len(selector) == 0 {
		return "every alert"
	}
	pairs := make([]string, 0, len(selector))
	for key, value := range selector {
		pairs = append(pairs, key+"="+value)
	}
	slices.Sort(pairs)
	return strings.ReplaceAll(strings.Join(pairs, ","), "`", "'")
}

// plainMarkdown keeps a name typed by someone else from restyling the reply.
func plainMarkdown(s string) string {
	return strings.NewReplacer("*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`, "\n", " ").Replace(s)
}

// sendTestAlert sends through the same path a routed, untracked alert takes,
// so a broken token, service URL or rendering shows up here rather than on
// the first real alert. The test message is the success reply.
func (s *Server) sendTestAlert(ctx context.Context, activity botActivity) {
	recipient, err := s.store.GetRecipientByConversation(ctx, activity.Conversation.ID)
	if errors.Is(err, store.ErrNotFound) {
		if s.cfg.Bot.GlobalInstall {
			// No routes name a managed chat, so the reply itself is the test.
			s.replyText(ctx, activity, "**"+testTitle+"**\n\n"+testText)
			return
		}
		s.replyText(ctx, activity, testNotLinkedReply)
		return
	}
	if err != nil {
		s.replyText(ctx, activity, failureText(ctx, "bot test", err))
		return
	}

	// A source the store knows: an unknown one fails the default-template lookup.
	alert := models.Event{
		Source: models.SourceUniversal, Labels: map[string]string{"alertname": "TeamsterTest"},
		Title: testTitle, Text: testText,
	}
	delivery := routing.Delivery{Kind: routing.DeliveryRecipient, RecipientID: recipient.ID, RouteName: "bot /test"}
	if err := s.deliverToRecipientOnce(ctx, alert, delivery); err != nil {
		// If the send itself failed this reply likely fails too; the log has both.
		s.replyText(ctx, activity, failureText(ctx, "bot test", err))
	}
}
