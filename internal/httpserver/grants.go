package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// errDeliveryRefused is what a write naming a channel outside the grants gets.
// It names the limit rather than the channel, because the caller already knows
// which channel they asked for and not why it was refused.
var errDeliveryRefused = errors.New("your roles are not granted that Team or channel")

// scopeOf reads the grants that bear on this request. It is a store read per
// request rather than a cache: grants change when an admin changes them, and a
// permission that lags behind the change is the kind of bug nobody finds.
func (s *Server) scopeOf(r *http.Request) (authz.Scope, error) {
	_, roles := principalOf(r)
	grants, err := s.store.ListGrants()
	if err != nil {
		return authz.Scope{}, err
	}
	return authz.ScopeFor(roles, grants), nil
}

// mayReach answers a scoped question — delivering to a channel, seeing one,
// seeing a Team — for the session behind this request.
func (s *Server) mayReach(r *http.Request, action string, resource authz.Resource) (bool, error) {
	scope, err := s.scopeOf(r)
	if err != nil {
		return false, err
	}
	subject, roles := principalOf(r)
	return s.authz.AllowScoped(subject, roles, action, resource, scope), nil
}

// mayDeliverTo is the check every write that names a channel goes through, so
// an editor cannot reach a channel by typing its id into a form that the picker
// would not have offered.
func (s *Server) mayDeliverTo(r *http.Request, teamID, channelID string) (bool, error) {
	// A destination with neither id delivers nowhere, so there is nothing to
	// scope; the store and the Graph client reject it later on their own terms.
	if teamID == "" && channelID == "" {
		return true, nil
	}
	// An id carrying the entity separator could otherwise be read as a channel
	// of a Team that was granted rather than the Team it names.
	if !authz.SafeID(teamID) || !authz.SafeID(channelID) {
		return false, nil
	}
	return s.mayReach(r, authz.ActionDeliver, authz.ChannelResource(teamID, channelID))
}

// mayDeliverToDestination scopes a write that names a destination rather than a
// channel. A route is how an alert actually reaches a channel, so pointing one
// at a destination outside the grants is the same escape as creating the
// destination there.
func (s *Server) mayDeliverToDestination(r *http.Request, destinationID string) (bool, error) {
	if destinationID == "" {
		return true, nil
	}

	destination, err := s.store.GetDestination(destinationID)
	if err != nil {
		// A route may point at a destination that does not exist; the graph
		// shows that as a broken route rather than refusing to save it.
		if errors.Is(err, store.ErrNotFound) {
			return true, nil
		}
		return false, err
	}
	return s.mayDeliverTo(r, destination.TeamID, destination.ChannelID)
}

func (s *Server) handleGrants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.store.ListGrants()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var grant models.Grant
		if err := json.NewDecoder(r.Body).Decode(&grant); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if err := validateGrant(grant); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		created, err := s.store.CreateGrant(grant)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleRoleGrants replaces everything granted to one role. The permissions
// page edits a whole tree of Teams and channels at once, and sending that as a
// list of creates and deletes would leave a half-applied scope on any failure.
func (s *Server) handleRoleGrants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Role   string `json:"role"`
		Scopes []struct {
			TeamID    string `json:"team_id"`
			ChannelID string `json:"channel_id"`
		} `json:"scopes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxScopeBytes)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	role := strings.TrimSpace(req.Role)
	if role == "" {
		writeJSONError(w, http.StatusBadRequest, "a role is required")
		return
	}

	wanted := make([]models.Grant, 0, len(req.Scopes))
	for _, scope := range req.Scopes {
		grant := models.Grant{
			Role:      role,
			TeamID:    strings.TrimSpace(scope.TeamID),
			ChannelID: strings.TrimSpace(scope.ChannelID),
		}
		if err := validateGrant(grant); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		wanted = append(wanted, grant)
	}

	if err := s.replaceRoleGrants(role, wanted); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"role": role, "granted": len(wanted)})
}

// maxScopeBytes bounds the tree a page can post. A tenant with thousands of
// channels is still far inside this.
const maxScopeBytes = 1 << 20

func (s *Server) replaceRoleGrants(role string, wanted []models.Grant) error {
	return s.store.WithTx(func(tx store.Store) error {
		existing, err := tx.ListGrants()
		if err != nil {
			return err
		}
		for _, grant := range existing {
			if grant.Role != role {
				continue
			}
			if err := tx.DeleteGrant(grant.ID); err != nil {
				return err
			}
		}
		for _, grant := range wanted {
			if _, err := tx.CreateGrant(grant); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Server) handleGrantByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/grants/")
	if id == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if err := s.store.DeleteGrant(id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// A grant without a role or a Team narrows nothing and would read as a scope
// that is simply missing.
func validateGrant(grant models.Grant) error {
	if strings.TrimSpace(grant.Role) == "" {
		return errors.New("a grant needs the role it applies to")
	}
	if strings.TrimSpace(grant.TeamID) == "" {
		return errors.New("a grant needs a Team; leave the channel empty to grant all of it")
	}
	return nil
}

func (s *Server) saveGrant(r *http.Request) (string, error) {
	grant := models.Grant{
		Role:      strings.TrimSpace(r.PostFormValue("role")),
		TeamID:    strings.TrimSpace(r.PostFormValue("team_id")),
		ChannelID: strings.TrimSpace(r.PostFormValue("channel_id")),
	}
	if err := validateGrant(grant); err != nil {
		return "", err
	}
	if _, err := s.store.CreateGrant(grant); err != nil {
		return "", err
	}
	return "Grant created.", nil
}

func (s *Server) deleteGrant(r *http.Request) (string, error) {
	if err := s.store.DeleteGrant(r.PostFormValue("id")); err != nil {
		return "", err
	}
	return "Grant deleted.", nil
}

// visibleTeams and visibleChannels ask the same question the pickers do, one
// entry at a time. The scope is read once for the whole list: it does not
// change while a single response is being built, and reading it per entry would
// turn a picker into a hundred store reads.
func (s *Server) visibleTeams(r *http.Request, teams []graph.Team) ([]graph.Team, error) {
	scope, err := s.scopeOf(r)
	if err != nil {
		return nil, err
	}
	if scope.Unrestricted {
		return teams, nil
	}

	subject, roles := principalOf(r)
	visible := make([]graph.Team, 0, len(teams))
	for _, team := range teams {
		if s.authz.AllowScoped(subject, roles, authz.ActionViewTeam, authz.TeamResource(team.ID), scope) {
			visible = append(visible, team)
		}
	}
	return visible, nil
}

func (s *Server) visibleChannels(r *http.Request, teamID string, channels []graph.Channel) ([]graph.Channel, error) {
	scope, err := s.scopeOf(r)
	if err != nil {
		return nil, err
	}
	if scope.Unrestricted {
		return channels, nil
	}

	subject, roles := principalOf(r)
	visible := make([]graph.Channel, 0, len(channels))
	for _, channel := range channels {
		if s.authz.AllowScoped(subject, roles, authz.ActionViewChannel, authz.ChannelResource(teamID, channel.ID), scope) {
			visible = append(visible, channel)
		}
	}
	return visible, nil
}

// visibleDestinations hides the destinations a session may not see, so a viewer
// limited to one Team reads a configuration about that Team rather than one
// full of channels they cannot reach.
func (s *Server) visibleDestinations(r *http.Request, destinations []models.Destination) ([]models.Destination, error) {
	scope, err := s.scopeOf(r)
	if err != nil {
		return nil, err
	}
	if scope.Unrestricted {
		return destinations, nil
	}

	subject, roles := principalOf(r)
	visible := make([]models.Destination, 0, len(destinations))
	for _, destination := range destinations {
		resource := authz.ChannelResource(destination.TeamID, destination.ChannelID)
		if s.authz.AllowScoped(subject, roles, authz.ActionViewChannel, resource, scope) {
			visible = append(visible, destination)
		}
	}
	return visible, nil
}
