package views

import "testing"

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
