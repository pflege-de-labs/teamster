package views

import "testing"

func TestActionRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		actions []string
		want    string
	}{
		{"none", nil, "-----"},
		{"read and attach", []string{"read", "attach"}, "r--a-"},
		{"all five", []string{"read", "update", "delete", "attach", "share"}, "rwdas"},
		{"owner", []string{"own", "read"}, "owner"},
		{"unknown ignored", []string{"read", "bogus"}, "r----"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ActionRef(tt.actions); got != tt.want {
				t.Errorf("ActionRef(%v) = %q, want %q", tt.actions, got, tt.want)
			}
		})
	}
}
