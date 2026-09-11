package i18n

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/language"
)

func bundle(t *testing.T, overrideDir string, fallback language.Tag) *Bundle {
	t.Helper()

	b, err := New(overrideDir, fallback)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

// A translator works from a list of keys, so a catalog that is missing some
// should say so here rather than in a page nobody looked at.
func TestEveryCatalogCarriesEverySourceKey(t *testing.T) {
	t.Parallel()

	catalogs, err := loadEmbedded()
	if err != nil {
		t.Fatalf("loadEmbedded: %v", err)
	}

	source, ok := catalogs[Source]
	if !ok || len(source) == 0 {
		t.Fatalf("no %s catalog, or an empty one", Source)
	}

	for tag, catalog := range catalogs {
		if tag == Source {
			continue
		}
		for key := range source {
			if value, ok := catalog[key]; !ok || value == "" {
				t.Errorf("the %s catalog is missing %q", tag, key)
			}
		}
		for key := range catalog {
			if _, ok := source[key]; !ok {
				t.Errorf("the %s catalog has %q, which %s does not", tag, key, Source)
			}
		}
	}
}

// A placeholder that the source uses has to survive translation, or a value
// disappears from a sentence in one language only.
func TestTranslationsKeepTheirPlaceholders(t *testing.T) {
	t.Parallel()

	catalogs, err := loadEmbedded()
	if err != nil {
		t.Fatalf("loadEmbedded: %v", err)
	}

	for tag, catalog := range catalogs {
		if tag == Source {
			continue
		}
		for key, source := range catalogs[Source] {
			for _, placeholder := range []string{"{0}", "{1}"} {
				wanted := containsPlaceholder(source, placeholder)
				got := containsPlaceholder(catalog[key], placeholder)
				if wanted != got {
					t.Errorf("%s %q: %s in source = %v, in translation = %v", tag, key, placeholder, wanted, got)
				}
			}
		}
	}
}

func containsPlaceholder(value, placeholder string) bool {
	return len(value) > 0 && stringsContains(value, placeholder)
}

func stringsContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestMatch(t *testing.T) {
	t.Parallel()

	b := bundle(t, "", language.English)

	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "nothing asked for", want: "en"},
		{name: "German", header: "de", want: "de"},
		{name: "a German dialect", header: "de-AT,de;q=0.9", want: "de"},
		{name: "German preferred over English", header: "de;q=0.9,en;q=0.8", want: "de"},
		{
			// The fallback rather than whichever catalog sorts first.
			name: "a language this build does not carry", header: "fr-CA,fr;q=0.9", want: "en",
		},
		{name: "nonsense", header: "!!!", want: "en"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := b.Match(tt.header).String(); got != tt.want {
				t.Errorf("Match(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

// A deployment can say which language an anonymous browser gets.
func TestTheConfiguredFallbackWins(t *testing.T) {
	t.Parallel()

	b := bundle(t, "", language.German)
	if got := b.Match("").String(); got != "de" {
		t.Errorf("Match() = %q, want the configured fallback", got)
	}
	if got := b.Match("fr").String(); got != "de" {
		t.Errorf("Match(fr) = %q, want the configured fallback", got)
	}
}

func TestLookupFallsBack(t *testing.T) {
	t.Parallel()

	b := bundle(t, "", language.English)

	if got := b.Lookup(language.German, "nav.routing"); got == "" {
		t.Error("a translated key came back empty")
	}
	// A key no catalog has renders as itself: a page saying "some.missing.key"
	// says what is wrong, and an empty element does not.
	if got := b.Lookup(language.German, "some.missing.key"); got != "some.missing.key" {
		t.Errorf("Lookup() = %q, want the key itself", got)
	}
}

// The part that makes the text editable without waiting for a release.
func TestOverridesWinOverTheBuiltInText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	override := map[string]string{"nav.routing": "Wegefindung"}
	raw, _ := json.Marshal(override)
	if err := os.WriteFile(filepath.Join(dir, "de.json"), raw, 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}

	b := bundle(t, dir, language.English)

	if got := b.Lookup(language.German, "nav.routing"); got != "Wegefindung" {
		t.Errorf("Lookup() = %q, want the override", got)
	}
	// Entry by entry: an override holding one string changes one string.
	if got := b.Lookup(language.German, "nav.configuration"); got == "" || got == "nav.configuration" {
		t.Errorf("Lookup() = %q, want the built-in text kept", got)
	}
}

// An operator can add a language the build does not carry.
func TestAnOverrideCanAddALanguage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	raw, _ := json.Marshal(map[string]string{"nav.routing": "Routage"})
	if err := os.WriteFile(filepath.Join(dir, "fr.json"), raw, 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}

	b := bundle(t, dir, language.English)

	if got := b.Match("fr").String(); got != "fr" {
		t.Errorf("Match(fr) = %q, want the added language", got)
	}
	if got := b.Lookup(language.French, "nav.routing"); got != "Routage" {
		t.Errorf("Lookup() = %q, want the added string", got)
	}
	// Everything it does not carry still falls back to the source.
	if got := b.Lookup(language.French, "nav.configuration"); got != "Configuration" {
		t.Errorf("Lookup() = %q, want the English text", got)
	}
}

func TestOverrideDirectoryProblems(t *testing.T) {
	t.Parallel()

	// A missing directory is not an error: most deployments have none.
	if _, err := New(filepath.Join(t.TempDir(), "nope"), language.English); err != nil {
		t.Errorf("New() with no override directory = %v, want it ignored", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "de.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}
	if _, err := New(dir, language.English); err == nil {
		t.Error("New() accepted a catalog that is not JSON")
	}

	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "not-a-language.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write override: %v", err)
	}
	if _, err := New(other, language.English); err == nil {
		t.Error("New() accepted a file not named after a language")
	}
}

func TestT(t *testing.T) {
	t.Parallel()

	b := bundle(t, "", language.English)
	ctx := WithLanguage(context.Background(), b, language.German)

	if got := T(ctx, "nav.configuration"); got != "Konfiguration" {
		t.Errorf("T() = %q, want the German text", got)
	}
	if got := T(ctx, "grants.scope_channel", "platform", "alerts"); got != "Team platform · Kanal alerts" {
		t.Errorf("T() = %q, want both values substituted", got)
	}
	if got := LanguageOf(ctx); got != "de" {
		t.Errorf("LanguageOf() = %q, want de", got)
	}

	// A component rendered outside a request still renders.
	if got := T(context.Background(), "nav.configuration"); got != "nav.configuration" {
		t.Errorf("T() without a language = %q, want the key", got)
	}
	if got := LanguageOf(context.Background()); got != "en" {
		t.Errorf("LanguageOf() without a language = %q, want the source", got)
	}
}

// A placeholder with no argument stays visible: it says a value is missing,
// where dropping it silently says nothing.
func TestSubstitutionLeavesUnfilledPlaceholders(t *testing.T) {
	t.Parallel()

	if got := substitute("Team {0} · channel {1}", []any{"platform"}); got != "Team platform · channel {1}" {
		t.Errorf("substitute() = %q, want the second placeholder left in place", got)
	}
	if got := substitute("no placeholders", nil); got != "no placeholders" {
		t.Errorf("substitute() = %q", got)
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	if got := Parse("de").String(); got != "de" {
		t.Errorf("Parse(de) = %q", got)
	}
	// A typo in a setting about wording must not stop a service that routes
	// alerts.
	if got := Parse("klingon!!").String(); got != Source.String() {
		t.Errorf("Parse(nonsense) = %q, want the source language", got)
	}
	if got := Parse("").String(); got != Source.String() {
		t.Errorf("Parse(empty) = %q, want the source language", got)
	}
}
