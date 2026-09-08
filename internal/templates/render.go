package templates

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/template"
)

type RenderData struct {
	Alert any
	Now   string
}

func Render(body string, data RenderData) (json.RawMessage, error) {
	funcs := template.FuncMap{
		"toJSON": func(v any) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
		"default": func(value string, fallback string) string {
			if value == "" {
				return fallback
			}
			return value
		},
	}

	tmpl, err := template.New("card").Funcs(funcs).Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	output := buf.Bytes()
	var tmp any
	if err := json.Unmarshal(output, &tmp); err != nil {
		return nil, fmt.Errorf("template output is not valid JSON: %w", err)
	}

	return json.RawMessage(output), nil
}
