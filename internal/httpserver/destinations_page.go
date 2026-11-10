package httpserver

import (
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
)

// handleDestinationList serves GET /admin/destinations: the destinations this session may read.
func (s *Server) handleDestinationList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	list := views.DestinationList{
		Viewer: s.viewerFor(r), Notice: q.Get("notice"), Error: q.Get("error"),
		CanCreate: s.allow(r, authz.ActionCreate, authz.Resource{Type: typeDestination}),
	}

	all, err := s.store.ListDestinations(ctx)
	if err == nil {
		all, err = s.listDestinations(r, all)
	}
	if err != nil {
		list.Error = failureText(ctx, "list destinations", err)
	}
	teams := make([]string, len(all))
	for i, d := range all {
		teams[i] = d.TeamID
	}
	states := s.installStates(ctx, teams)
	for _, d := range all {
		list.Rows = append(list.Rows, views.DestinationRow{Destination: d, InstallState: states[d.TeamID]})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.DestinationListPage(list).Render(ctx, w); err != nil {
		logError(ctx, "render destination list", err)
	}
}
