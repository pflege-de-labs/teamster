package httpserver

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/logging"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
)

// Commands in a team channel (ADR 0069). Teams delivers a channel message only
// when it @mentions the bot, so every one of them is addressed to it.
const (
	channelHelpReply = "**Teamster commands**\n\n" +
		"- `help` — this list\n" +
		"- `status` — which routes post to this channel\n" +
		"- `test` — post a test alert to this channel\n\n" +
		"Mention me first, as in `@Teamster status`."
	channelNotACommandReply = "Mention me with a command, such as `help`, to see what I can do here."
	channelUnlinkReply      = "Unlinking is for your personal chat with me. What reaches a channel is " +
		"decided by the routes in the Teamster admin UI."
	channelNoDestinationStatus = "No destination in Teamster names this channel, so nothing is posted " +
		"here. Add one under **Destinations** in the admin UI."
	channelNoDestinationTest = "No destination in Teamster names this channel, so there is nowhere to " +
		"send a test alert. Add one under **Destinations** in the admin UI."
)

// handleChannelMessage answers a command in a channel. A link code is never
// read here: linking a channel would broadcast one person's alerts to it.
func (s *Server) handleChannelMessage(ctx context.Context, activity botActivity) {
	command, ok := parseBotCommand(activity.Text, activity.Entities)
	if !ok {
		s.replyText(ctx, activity, channelNotACommandReply)
		return
	}
	logging.FromContext(ctx).Info("bot command", "command", command, "conversation_type", "channel")
	switch command {
	case commandHelp:
		s.replyText(ctx, activity, channelHelpReply)
	case commandStatus:
		s.replyText(ctx, activity, s.channelStatusReply(ctx, activity))
	case commandTest:
		s.sendChannelTestAlert(ctx, activity)
	case commandUnlink:
		s.replyText(ctx, activity, channelUnlinkReply)
	default:
		s.replyText(ctx, activity, unknownCommandReply)
	}
}

// activityChannelID is the channel a message was posted in. The conversation
// id is the channel's id with ";messageid=…" for the thread appended.
func activityChannelID(activity botActivity) string {
	if id := activity.ChannelData.Channel.ID; id != "" {
		return id
	}
	id, _, _ := strings.Cut(activity.Conversation.ID, ";")
	return id
}

// channelDestinations are the destinations naming the channel the activity
// came from, in name order so the first is stable.
func (s *Server) channelDestinations(ctx context.Context, activity botActivity) ([]models.Destination, error) {
	teamID, channelID := activity.ChannelData.Team.AADGroupID, activityChannelID(activity)
	if teamID == "" || channelID == "" {
		return nil, nil
	}
	all, err := s.store.ListDestinations(ctx)
	if err != nil {
		return nil, err
	}
	var out []models.Destination
	for _, d := range all {
		if d.TeamID == teamID && d.ChannelID == channelID {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b models.Destination) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (s *Server) channelStatusReply(ctx context.Context, activity botActivity) string {
	destinations, err := s.channelDestinations(ctx, activity)
	if err != nil {
		return failureText(ctx, "bot channel status", err)
	}
	if len(destinations) == 0 {
		return channelNoDestinationStatus
	}
	routes, err := s.store.ListRoutes(ctx)
	if err != nil {
		return failureText(ctx, "bot channel status", err)
	}
	return formatChannelStatus(destinations, routes)
}

func formatChannelStatus(destinations []models.Destination, routes []models.Route) string {
	var b strings.Builder
	ids := make(map[string]bool, len(destinations))
	names := make([]string, 0, len(destinations))
	isDefault := false
	for _, d := range destinations {
		ids[d.ID] = true
		names = append(names, "**"+plainMarkdown(d.Name)+"**")
		isDefault = isDefault || d.IsDefault
	}
	fmt.Fprintf(&b, "This channel is the destination %s.\n\n", strings.Join(names, ", "))
	if isDefault {
		b.WriteString("It is the global default, so every alert no route claims is posted here.\n\n")
	}

	var lines []string
	for _, route := range routes {
		if !ids[route.DestinationID] {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: `%s`", plainMarkdown(route.Name), selectorLine(route.LabelSelector)))
	}
	if len(lines) == 0 {
		b.WriteString("No route names this channel yet. Pick it in **Delivers to** on a route in the admin UI.")
		return b.String()
	}
	slices.Sort(lines)
	b.WriteString("**Routes posting here**\n\n")
	b.WriteString(strings.Join(lines, "\n"))
	return b.String()
}

// sendChannelTestAlert posts through the same untracked path a routed alert
// takes, so the channel transport and rendering are what gets tested.
func (s *Server) sendChannelTestAlert(ctx context.Context, activity botActivity) {
	destinations, err := s.channelDestinations(ctx, activity)
	if err != nil {
		s.replyText(ctx, activity, failureText(ctx, "bot test", err))
		return
	}
	if len(destinations) == 0 {
		s.replyText(ctx, activity, channelNoDestinationTest)
		return
	}
	delivery := routing.Delivery{Kind: routing.DeliveryChannel, DestinationID: destinations[0].ID, RouteName: "bot /test"}
	if err := s.deliverToChannelOnce(ctx, testEvent(), delivery); err != nil {
		s.replyText(ctx, activity, failureText(ctx, "bot test", err))
	}
}
