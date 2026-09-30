package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

// channelCard is where a channel post landed, as an edit needs to address it.
// An empty ConversationID is a card nothing can edit (ADR 0045).
type channelCard struct {
	ConversationID string
	MessageID      string
}

// ChannelTransport posts and edits channel cards. graph.Message stays the
// channel message shape: HTML text and any number of cards.
type ChannelTransport interface {
	PostToChannel(ctx context.Context, teamID, channelID string, msg graph.Message) (channelCard, error)
	UpdateInChannel(ctx context.Context, teamID, channelID string, card channelCard, msg graph.Message) error
}

// errNoChannelTransport is every channel delivery on a deployment without the
// bot: Graph cannot post to a channel as an application.
var errNoChannelTransport = errors.New("channel delivery needs the bot to be configured (ADR 0045)")

type noChannels struct{}

func (noChannels) PostToChannel(context.Context, string, string, graph.Message) (channelCard, error) {
	return channelCard{}, errNoChannelTransport
}

func (noChannels) UpdateInChannel(context.Context, string, string, channelCard, graph.Message) error {
	return errNoChannelTransport
}

// channelBot is the slice of the Bot Connector client channel delivery uses.
type channelBot interface {
	PostToChannel(ctx context.Context, serviceURL, tenantID, channelID string, msg bot.Message) (bot.ChannelPost, error)
	UpdateMessage(ctx context.Context, ref bot.ConversationReference, activityID string, msg bot.Message) error
}

type botTeams interface {
	GetBotTeam(ctx context.Context, teamID string) (models.BotTeam, error)
}

// BotChannels delivers channel cards through the bot.
type BotChannels struct {
	bot        channelBot
	teams      botTeams
	serviceURL string
	tenantID   string
}

// NewBotChannels uses the team's recorded service URL, else cfg.ServiceURL.
// fallbackTenant names the tenant for a multi-tenant bot, which has none of
// its own.
func NewBotChannels(client channelBot, teams botTeams, cfg config.BotConfig, fallbackTenant string) *BotChannels {
	tenant := cfg.TenantID
	if tenant == "" {
		tenant = fallbackTenant
	}
	return &BotChannels{bot: client, teams: teams, serviceURL: cfg.ServiceURL, tenantID: tenant}
}

func (b *BotChannels) PostToChannel(ctx context.Context, teamID, channelID string, msg graph.Message) (channelCard, error) {
	serviceURL, tenantID, err := b.team(ctx, teamID)
	if err != nil {
		return channelCard{}, err
	}
	out, err := botChannelMessage(msg)
	if err != nil {
		return channelCard{}, err
	}
	post, err := b.bot.PostToChannel(ctx, serviceURL, tenantID, channelID, out)
	if err != nil {
		return channelCard{}, notInstalled(teamID, err)
	}
	return channelCard{ConversationID: post.ConversationID, MessageID: post.ActivityID}, nil
}

func (b *BotChannels) UpdateInChannel(ctx context.Context, teamID, _ string, card channelCard, msg graph.Message) error {
	serviceURL, _, err := b.team(ctx, teamID)
	if err != nil {
		return err
	}
	out, err := botChannelMessage(msg)
	if err != nil {
		return err
	}
	ref := bot.ConversationReference{ServiceURL: serviceURL, ConversationID: card.ConversationID}
	return notInstalled(teamID, b.bot.UpdateMessage(ctx, ref, card.MessageID, out))
}

func (b *BotChannels) team(ctx context.Context, teamID string) (string, string, error) {
	team, err := b.teams.GetBotTeam(ctx, teamID)
	switch {
	case err == nil && team.ServiceURL != "":
		return team.ServiceURL, team.TenantID, nil
	case err == nil && b.serviceURL != "":
		// Found through Graph; no activity from the team has named its URL.
		return b.serviceURL, team.TenantID, nil
	case err == nil:
		return "", "", fmt.Errorf("team %s: no activity from it has named its Bot Connector endpoint, and bot.service-url is empty", teamID)
	case !errors.Is(err, store.ErrNotFound):
		return "", "", fmt.Errorf("bot team: %w", err)
	case b.serviceURL == "":
		return "", "", fmt.Errorf("team %s: no install event has named its Bot Connector endpoint, and bot.service-url is empty", teamID)
	default:
		return b.serviceURL, b.tenantID, nil
	}
}

// appNotInstalledError is a post the Connector refused in a team, which almost
// always means the bot's app is not installed there.
type appNotInstalledError struct {
	teamID string
	err    error
}

func (e *appNotInstalledError) Error() string {
	return fmt.Sprintf("team %s refused the bot; is the Teams app installed there? %v", e.teamID, e.err)
}

func (e *appNotInstalledError) Unwrap() error { return e.err }

// notInstalled says what a refused post most likely means, which a bare 403
// from the Connector does not.
func notInstalled(teamID string, err error) error {
	var apiErr *bot.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		return &appNotInstalledError{teamID: teamID, err: err}
	}
	return err
}

// channelFailure is the outcome a failed channel post or edit is counted as.
func channelFailure(err error) string {
	var missing *appNotInstalledError
	if errors.As(err, &missing) {
		return metrics.OutcomeAppMissing
	}
	return metrics.OutcomeFailed
}

// botChannelMessage converts the sanitized HTML to the Markdown the Connector
// renders, as chatMessage does. A new channel post must be exactly one Teams
// message -- text and a card, or two cards, are refused with "Activity
// resulted into multiple skype activities" -- so any card takes the title and
// text into itself, and the summary keeps the feed from showing "Card".
func botChannelMessage(msg graph.Message) (bot.Message, error) {
	text, err := templates.ToMarkdown(msg.Text)
	if err != nil {
		return bot.Message{}, fmt.Errorf("markdown: %w", err)
	}
	summary := templates.Summary(msg.Title, msg.Text)
	if len(msg.Cards) == 0 {
		return bot.Message{Title: msg.Title, Text: text, Summary: summary}, nil
	}
	card, err := oneCard(msg.Title, text, msg.Cards)
	if err != nil {
		return bot.Message{}, err
	}
	return bot.Message{Card: card, Summary: summary}, nil
}

// oneCard folds a title, Markdown text and several Adaptive Cards into the
// first card: the title and text lead as TextBlocks, each further card's body
// follows in a separated Container, and every card's actions are kept.
func oneCard(title, text string, cards []json.RawMessage) (json.RawMessage, error) {
	var first map[string]any
	if err := json.Unmarshal(cards[0], &first); err != nil {
		return nil, fmt.Errorf("card: %w", err)
	}
	var body, actions []any
	if title != "" {
		body = append(body, map[string]any{"type": "TextBlock", "text": title, "weight": "Bolder", "size": "Medium", "wrap": true})
	}
	if text != "" {
		body = append(body, map[string]any{"type": "TextBlock", "text": text, "wrap": true})
	}
	body = append(body, cardList(first, "body")...)
	actions = append(actions, cardList(first, "actions")...)

	for _, raw := range cards[1:] {
		var card map[string]any
		if err := json.Unmarshal(raw, &card); err != nil {
			return nil, fmt.Errorf("card: %w", err)
		}
		if items := cardList(card, "body"); len(items) > 0 {
			body = append(body, map[string]any{"type": "Container", "separator": true, "items": items})
		}
		actions = append(actions, cardList(card, "actions")...)
	}

	first["body"] = body
	if len(actions) > 0 {
		first["actions"] = actions
	}
	if _, ok := first["type"]; !ok {
		first["type"] = "AdaptiveCard"
	}
	if _, ok := first["version"]; !ok {
		first["version"] = "1.4"
	}
	return json.Marshal(first)
}

func cardList(card map[string]any, key string) []any {
	list, _ := card[key].([]any)
	return list
}
