package models

import (
	"maps"
	"slices"
	"strings"
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

func TestNormalizeSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{name: "none is any", in: nil, want: nil},
		{name: "ordered and deduplicated", in: []string{SourceTeamsV2, SourceAlertmanager, SourceTeamsV2}, want: []string{SourceAlertmanager, SourceTeamsV2}},
		{name: "unknown", in: []string{"email"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeSources(tt.in)
			if (err != nil) != tt.wantErr || !slices.Equal(got, tt.want) {
				t.Errorf("NormalizeSources(%v) = %v, %v, want %v (error %v)", tt.in, got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestTemplateHandles(t *testing.T) {
	t.Parallel()

	if !(Template{}).Handles(SourceTeamsV2) {
		t.Error("a template without sources should handle any")
	}
	only := Template{Sources: []string{SourceAlertmanager}}
	if !only.Handles(SourceAlertmanager) || only.Handles(SourceUniversal) {
		t.Errorf("Handles on %v is wrong", only.Sources)
	}
}

func TestAddressesOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ev   Event
		want []string
	}{
		{name: "nobody", ev: Event{}},
		{
			name: "the list, trimmed and without repeats",
			ev:   Event{Universal: &UniversalEvent{Recipients: []string{" alice@corp.example ", "ALICE@corp.example", "", "bob@corp.example"}}},
			want: []string{"alice@corp.example", "bob@corp.example"},
		},
		{
			name: "the label when there is no list",
			ev:   Event{Labels: map[string]string{RecipientLabel: "alice@corp.example, bob@corp.example,"}},
			want: []string{"alice@corp.example", "bob@corp.example"},
		},
		{
			name: "the list wins over the label",
			ev: Event{
				Labels:    map[string]string{RecipientLabel: "bob@corp.example"},
				Universal: &UniversalEvent{Recipients: []string{"alice@corp.example"}},
			},
			want: []string{"alice@corp.example"},
		},
		{
			name: "an empty list falls back to the label",
			ev:   Event{Labels: map[string]string{RecipientLabel: "bob@corp.example"}, Universal: &UniversalEvent{}},
			want: []string{"bob@corp.example"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := AddressesOf(tt.ev); strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("AddressesOf() = %v, want %v", got, tt.want)
			}
		})
	}
}
