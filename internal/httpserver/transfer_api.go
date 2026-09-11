package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/transfer"
)

// maxBundleBytes bounds an import. A configuration is templates and a few
// hundred rows; anything past this is a mistake or an attack.
const maxBundleBytes = 8 << 20

// handleExport answers with the whole configuration as a bundle. It carries no
// credentials and no runtime state, which is what makes it safe to keep beside
// the rest of a deployment's configuration.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	bundle, err := transfer.Export(s.store, s.directoryNames())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Named so that a browser saving it gets something an operator recognises
	// a week later.
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="teamster-%s.json"`, time.Now().UTC().Format("2006-01-02")))
	writeJSON(w, http.StatusOK, bundle)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	mode, err := transfer.ParseMode(r.URL.Query().Get("mode"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	dryRun := r.URL.Query().Get("dry-run") == "true"

	r.Body = http.MaxBytesReader(w, r.Body, maxBundleBytes)
	var bundle transfer.Bundle
	if err := json.NewDecoder(r.Body).Decode(&bundle); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "that bundle is too large to import")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	result, err := transfer.Import(s.store, bundle, mode, dryRun)
	if err != nil {
		// A bundle this installation will not accept is the caller's to fix,
		// not a failure of the server.
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Team and channel ids belong to one tenant. A bundle carried to another
	// imports cleanly and then delivers nowhere, so the destinations that no
	// longer resolve are named rather than left to be discovered by an alert
	// that never arrives.
	result.Unresolved = s.unresolvedDestinations(bundle)

	writeJSON(w, http.StatusOK, result)
}

// unresolvedDestinations names the destinations whose Team or channel this
// tenant does not have. It asks Graph, and says nothing when Graph cannot be
// reached: an unreachable directory is not evidence that a channel is missing.
func (s *Server) unresolvedDestinations(bundle transfer.Bundle) []string {
	teams, err := s.directory.Teams(s.graph.ListTeams)
	if err != nil {
		return nil
	}

	known := map[string]bool{}
	for _, team := range teams {
		known[team.ID] = true
	}

	var unresolved []string
	channelsOf := map[string]map[string]bool{}
	for _, destination := range bundle.Destinations {
		if !known[destination.TeamID] {
			unresolved = append(unresolved, describeDestination(destination))
			continue
		}

		if _, looked := channelsOf[destination.TeamID]; !looked {
			channels, err := s.directory.Channels(destination.TeamID, func() ([]graph.Channel, error) {
				return s.graph.ListChannels(destination.TeamID)
			})
			if err != nil {
				// One Team that cannot be listed says nothing about the rest.
				continue
			}
			found := map[string]bool{}
			for _, channel := range channels {
				found[channel.ID] = true
			}
			channelsOf[destination.TeamID] = found
		}

		if !channelsOf[destination.TeamID][destination.ChannelID] {
			unresolved = append(unresolved, describeDestination(destination))
		}
	}
	return unresolved
}

// The names the bundle carried are what make an unresolved destination
// recognisable; the ids alone say nothing to a person.
func describeDestination(destination transfer.BundleDestination) string {
	described := destination.Name
	if destination.TeamName != "" {
		described += " (" + destination.TeamName
		if destination.ChannelName != "" {
			described += " › " + destination.ChannelName
		}
		return described + ")"
	}
	return described + " (team " + destination.TeamID + ")"
}

// directoryNames resolves Team and channel names for an export, and gives up
// quietly: Graph being unreachable is no reason to refuse to back a
// configuration up.
func (s *Server) directoryNames() transfer.Directory {
	destinations, err := s.store.ListDestinations()
	if err != nil {
		return nil
	}
	namer := s.channelNamer(destinations)

	teams := map[string]string{}
	if list, err := s.directory.Teams(s.graph.ListTeams); err == nil {
		for _, team := range list {
			teams[team.ID] = team.Name
		}
	}
	return directoryNamer{teams: teams, channel: namer}
}

type directoryNamer struct {
	teams   map[string]string
	channel func(teamID, channelID string) string
}

func (d directoryNamer) TeamName(teamID string) string { return d.teams[teamID] }

// channelNamer answers "Team › channel"; the bundle wants the channel alone.
func (d directoryNamer) ChannelName(teamID, channelID string) string {
	full := d.channel(teamID, channelID)
	team := d.teams[teamID]
	if team == "" {
		team = teamID
	}
	if prefix := team + " › "; len(full) > len(prefix) && full[:len(prefix)] == prefix {
		return full[len(prefix):]
	}
	return full
}

// authorizeTransfer marks import and export as administer actions: an export is
// the whole configuration in one file, and an import rewrites it.
func transferResource() authz.Resource {
	return authz.Resource{Type: "Configuration"}
}
