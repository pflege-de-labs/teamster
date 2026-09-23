package templates

import (
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
		{"the alert itself", v.Fields, ".Alert"},
		{"an alert field", v.Fields, ".Alert.Status"},
		{"a top-level field", v.Fields, ".Now"},
		{"labels are a label map", v.LabelMaps, ".Alert.Labels"},
		{"annotations are an annotation map", v.AnnotationMaps, ".Alert.Annotations"},
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
		if field == ".Alert.StartsAt.wall" || field == ".Alert.Card.0" {
			t.Errorf("vocabulary offers %q, which no template can use", field)
		}
	}
}
