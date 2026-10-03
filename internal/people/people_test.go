package people

import (
	"fmt"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestDirectoryUserOfCarriesTheProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		user graph.User
		want models.Profile
	}{
		{name: "nothing beyond a name", user: graph.User{ID: "oid", GivenName: "Alice"}},
		{
			name: "everything Entra holds",
			user: graph.User{
				ID: "oid", JobTitle: "SRE", Department: "Platform", CompanyName: "Corp", OfficeLocation: "B1",
				EmployeeID: "4711", StreetAddress: "Hauptstraße 1", PostalCode: "10115", City: "Berlin",
				State: "BE", Country: "DE", BusinessPhones: []string{"+49 30 1"}, MobilePhone: "+49 170 1",
				PreferredLanguage: "de-DE", UsageLocation: "DE",
			},
			want: models.Profile{
				JobTitle: "SRE", Department: "Platform", CompanyName: "Corp", OfficeLocation: "B1", EmployeeID: "4711",
				Address:        models.Address{Street: "Hauptstraße 1", PostalCode: "10115", City: "Berlin", State: "BE", Country: "DE"},
				BusinessPhones: []string{"+49 30 1"}, MobilePhone: "+49 170 1", PreferredLanguage: "de-DE", UsageLocation: "DE",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := directoryUserOf(tt.user, "tenant", time.Now())
			if fmt.Sprint(got.Profile) != fmt.Sprint(tt.want) {
				t.Errorf("Profile = %+v, want %+v", got.Profile, tt.want)
			}
		})
	}
}
