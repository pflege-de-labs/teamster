package templates

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestEditorVocabulary(t *testing.T) {
	t.Parallel()

	v := EditorVocabulary()
	tests := []struct {
		name string
		list []string
		want string
	}{
		{"the event itself", v.Fields, ".Event"},
		{"an event field", v.Fields, ".Event.State"},
		{"a top-level field", v.Fields, ".Now"},
		{"an extension field", v.Fields, ".Event.Alertmanager.GroupKey"},
		{"who a chat message is for", v.Fields, ".Recipient.GivenName"},
		{"who a universal event names", v.Fields, ".Event.Universal.Recipients"},
		{"labels are a label map", v.LabelMaps, ".Event.Labels"},
		{"group labels are a label map", v.LabelMaps, ".Event.Alertmanager.CommonLabels"},
		{"annotations are an attribute map", v.AttributeMaps, ".Event.Alertmanager.Annotations"},
		{"common annotations are an attribute map", v.AttributeMaps, ".Event.Alertmanager.CommonAnnotations"},
		{"attributes are an attribute map", v.AttributeMaps, ".Event.Universal.Attributes"},
		{"our own function", v.Functions, "default"},
		{"another of ours", v.Functions, "toJSON"},
		{"a text/template builtin", v.Functions, "printf"},
		{"an action keyword", v.Keywords, "range"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !slices.Contains(tt.list, tt.want) {
				t.Errorf("%v does not contain %q", tt.list, tt.want)
			}
		})
	}

	// Every function a template can call is offered, and nothing else.
	for name := range funcs() {
		if !slices.Contains(v.Functions, name) {
			t.Errorf("funcs() has %q, which the vocabulary does not offer", name)
		}
	}
	for _, field := range v.Fields {
		if field == ".Event.Alertmanager.StartsAt.wall" || field == ".Event.Card.0" {
			t.Errorf("vocabulary offers %q, which no template can use", field)
		}
	}
}

// The editor reads these keys by name, so a renamed tag breaks completion silently.
func TestEditorVocabularyJSONKeys(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(EditorVocabulary())
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"fields", "labelMaps", "attributeMaps", "functions", "keywords"} {
		if _, ok := keys[key]; !ok {
			t.Errorf("vocabulary JSON lacks %q: %s", key, raw)
		}
	}
}
