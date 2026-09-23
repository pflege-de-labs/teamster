package templates

import (
	"reflect"
	"slices"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// Vocabulary is what the admin UI's template editor completes inside {{ }}.
// It is derived from RenderData and funcs, so a field or function added there
// is offered without anyone editing the browser code.
type Vocabulary struct {
	// Fields are dotted paths such as .Alert.Labels, in declaration order.
	Fields []string `json:"fields"`
	// LabelMaps and AnnotationMaps are the fields whose keys are label and
	// annotation keys, which the editor completes from sampled alerts.
	LabelMaps      []string `json:"labelMaps"`
	AnnotationMaps []string `json:"annotationMaps"`
	Functions      []string `json:"functions"`
	Keywords       []string `json:"keywords"`
}

// text/template's own functions and actions, which its API does not list.
var (
	builtinFunctions = []string{
		"and", "call", "eq", "ge", "gt", "html", "index", "js", "le", "len", "lt",
		"ne", "not", "or", "print", "printf", "println", "slice", "urlquery",
	}
	actionKeywords = []string{
		"block", "break", "continue", "define", "else", "end", "if", "nil", "range", "template", "with",
	}
)

// EditorVocabulary describes the data every template is executed against.
func EditorVocabulary() Vocabulary {
	v := Vocabulary{Keywords: slices.Clone(actionKeywords)}
	collectFields(&v, "", reflect.ValueOf(RenderData{Alert: models.Alert{}}))

	v.Functions = slices.Clone(builtinFunctions)
	for name := range funcs() {
		v.Functions = append(v.Functions, name)
	}
	slices.Sort(v.Functions)
	return v
}

func collectFields(v *Vocabulary, prefix string, value reflect.Value) {
	if value.Kind() == reflect.Interface {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return
	}
	typ := value.Type()
	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		path := prefix + "." + field.Name
		v.Fields = append(v.Fields, path)

		if field.Type == reflect.TypeFor[map[string]string]() {
			switch field.Name {
			case "Labels":
				v.LabelMaps = append(v.LabelMaps, path)
			case "Annotations":
				v.AnnotationMaps = append(v.AnnotationMaps, path)
			}
			continue
		}
		// time.Time and json.RawMessage have no fields worth offering.
		if field.Type.PkgPath() == "time" {
			continue
		}
		collectFields(v, path, value.Field(i))
	}
}
