package views

import (
	"testing"

	"golang.org/x/text/language"

	"github.com/pflege-de-labs/teamster/internal/i18n"
)

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

func TestActionRefTranslatesTheOwner(t *testing.T) {
	t.Parallel()

	bundle, err := i18n.New("", language.English)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		lang    language.Tag
		actions []string
		want    string
	}{
		{language.English, []string{"own"}, "owner"},
		{language.German, []string{"own"}, "Eigentümer"},
		{language.German, []string{"read"}, "r----"},
	}
	for _, tt := range tests {
		t.Run(tt.lang.String()+tt.want, func(t *testing.T) {
			t.Parallel()
			ctx := i18n.WithLanguage(t.Context(), bundle, tt.lang)
			if got := actionRef(ctx, tt.actions); got != tt.want {
				t.Errorf("actionRef(%v) = %q, want %q", tt.actions, got, tt.want)
			}
		})
	}
}
