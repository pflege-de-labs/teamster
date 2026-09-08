package templates

import (
	"testing"
)

func TestRenderValidJSON(t *testing.T) {
	body := `{"type":"AdaptiveCard","version":"1.4","body":[{"type":"TextBlock","text":"{{ default (index .Alert "alertname") "unknown" }}"}]}`

	payload, err := Render(body, RenderData{Alert: map[string]string{"alertname": "HighCPU"}})
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(payload) == 0 {
		t.Fatalf("expected payload bytes")
	}
}

func TestRenderInvalidJSON(t *testing.T) {
	body := `{"type":"AdaptiveCard","body":[` // invalid JSON after template render

	_, err := Render(body, RenderData{Alert: map[string]string{"alertname": "HighCPU"}})
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}
