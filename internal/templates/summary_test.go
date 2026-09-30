package templates

import (
	"strings"
	"testing"
)

func TestSummary(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", maxSummaryRunes+10)
	tests := []struct {
		name  string
		title string
		html  string
		want  string
	}{
		{name: "nothing", want: ""},
		{name: "title wins over text", title: "Disk full", html: "<p>on db-1</p>", want: "Disk full"},
		{name: "title whitespace collapses", title: "  Disk \n full ", want: "Disk full"},
		{name: "no title takes the text", html: "<p>Disk <b>full</b></p>", want: "Disk full"},
		{name: "only the first paragraph", html: "<p>one</p><p>two</p>", want: "one"},
		{name: "a line break ends the line", html: "<p>one<br>two</p>", want: "one"},
		{name: "blank blocks are skipped", html: "<p> </p><ul><li>item</li></ul>", want: "item"},
		{name: "a long title is cut", title: long, want: strings.Repeat("x", maxSummaryRunes-1) + "…"},
		{name: "exactly the limit is kept", title: long[:maxSummaryRunes], want: long[:maxSummaryRunes]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Summary(tt.title, tt.html); got != tt.want {
				t.Errorf("Summary(%q, %q) = %q, want %q", tt.title, tt.html, got, tt.want)
			}
		})
	}
}
