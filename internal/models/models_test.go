package models

import (
	"maps"
	"testing"
)

func TestWithSourceLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		want   map[string]string
	}{
		{name: "nil", labels: nil, want: map[string]string{SourceLabel: SourceTeamsV2}},
		{name: "added", labels: map[string]string{"a": "b"}, want: map[string]string{"a": "b", SourceLabel: SourceTeamsV2}},
		{name: "overwritten", labels: map[string]string{SourceLabel: "forged"}, want: map[string]string{SourceLabel: SourceTeamsV2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before := maps.Clone(tt.labels)
			got := WithSourceLabel(tt.labels, SourceTeamsV2)
			if !maps.Equal(got, tt.want) {
				t.Errorf("WithSourceLabel = %v, want %v", got, tt.want)
			}
			if !maps.Equal(tt.labels, before) {
				t.Errorf("input changed to %v, want it left alone", tt.labels)
			}
		})
	}
}
