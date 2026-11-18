package views

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestPrincipalPickerKinds(t *testing.T) {
	t.Parallel()

	opts := PrincipalOptions{
		Users:     []models.User{{Subject: "s-1", Name: "Ann"}},
		Groups:    []models.Group{{ID: "g-1", Name: "G"}},
		IdPGroups: []string{"devs"},
		Roles:     []string{"viewer"},
	}
	tests := []struct {
		name     string
		cfg      PickerConfig
		want     []string
		unwanted []string
	}{
		{"all kinds", PickerConfig{Name: "principal", Options: opts}, []string{`value="user:s-1"`, `value="group:g-1"`, `value="idp_group:devs"`, `value="role:viewer"`}, []string{"other_id"}},
		{"member kinds", PickerConfig{Name: "principal", Options: opts, Kinds: []string{"user", "group", "idp_group"}, AllowOther: true},
			[]string{`value="user:s-1"`, `value="group:g-1"`, `value="idp_group:devs"`, "other_id"}, []string{`value="role:`, `<option value="role"`}},
		{"groups only leaves no free text", PickerConfig{Name: "principal", Options: opts, Kinds: []string{"group"}, AllowOther: true},
			[]string{`value="group:g-1"`}, []string{"other_id", "other_type", `value="user:`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := principalPicker(tt.cfg).Render(context.Background(), &buf); err != nil {
				t.Fatal(err)
			}
			got := buf.String()
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("lacks %q", w)
				}
			}
			for _, u := range tt.unwanted {
				if strings.Contains(got, u) {
					t.Errorf("has %q", u)
				}
			}
		})
	}
}

func TestViewerCurrent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path, href string
		want       bool
	}{
		{"/admin/templates", "/admin/templates", true},
		{"/admin/templates/x", "/admin/templates", true},
		{"/admin/routes", "/admin/routing", false},
		{"/admin/routing", "/admin/routes", false},
	}

	for _, tt := range tests {
		t.Run(tt.path+" "+tt.href, func(t *testing.T) {
			t.Parallel()

			if got := (Viewer{Path: tt.path}).current(tt.href); got != tt.want {
				t.Errorf("current = %v, want %v", got, tt.want)
			}
		})
	}
}
