package httpserver

import (
	"net/http"
	"time"
)

// timeZoneCookie remembers the zone the UI shows times in. Absent means UTC,
// the zone of the logs and of Graph's errors.
const timeZoneCookie = "teamster_timezone"

// handleTimeZone records a choice and returns to the page it was made on. The
// browser's zone arrives as a name, because only a script can read it.
func (s *Server) handleTimeZone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}

	if r.PostFormValue("timezone") != "browser" {
		http.SetCookie(w, &http.Cookie{
			Name: timeZoneCookie, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: isTLS(r), SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, returnTo(r), http.StatusSeeOther)
		return
	}

	loc, ok := loadZone(r.PostFormValue("zone"))
	if !ok {
		http.Error(w, "unknown time zone", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: timeZoneCookie, Value: loc.String(), Path: "/",
		Expires:  time.Now().Add(languageYear),
		MaxAge:   int(languageYear.Seconds()),
		HttpOnly: true, Secure: isTLS(r), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, returnTo(r), http.StatusSeeOther)
}

// timeZonePreference is the zone the operator picked, or UTC.
func timeZonePreference(r *http.Request) *time.Location {
	if cookie, err := r.Cookie(timeZoneCookie); err == nil {
		if loc, ok := loadZone(cookie.Value); ok {
			return loc
		}
	}
	return time.UTC
}

// loadZone accepts an IANA zone name only. "Local" would be the server's zone,
// which says nothing about the operator.
func loadZone(name string) (*time.Location, bool) {
	if name == "" || name == "Local" {
		return nil, false
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, false
	}
	return loc, true
}
