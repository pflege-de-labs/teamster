// Package i18n keeps the text of the admin UI out of the components that show
// it. A string lives in a catalog under a key, which means the wording can be
// changed in one place, and a second language is a file rather than a fork of
// every template.
//
// What is not translated: webhook responses, API errors and log lines. Those
// are read by machines, and by whoever is reading a log at three in the
// morning; a translated error is harder to search for.
package i18n

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/language"
)

//go:embed locales/*.json
var embedded embed.FS

// localeName is what a catalog file may be called: a language, optionally with
// a script or a region.
var localeName = regexp.MustCompile(`^[a-zA-Z]{2,3}(-([a-zA-Z]{2}|[a-zA-Z]{4}|[0-9]{3}))?$`)

// Source is the language every catalog is measured against and the one a
// missing key falls back to.
var Source = language.English

// A Catalog is one language's strings, keyed by identifier.
type Catalog map[string]string

// A Bundle is every language this build carries, plus whatever an operator
// dropped in the override directory. It is immutable once built, so one is
// shared by every request.
type Bundle struct {
	catalogs map[language.Tag]Catalog
	matcher  language.Matcher
	tags     []language.Tag
	fallback language.Tag
}

// New loads the embedded catalogs and then any file in overrideDir with the
// same name, whose entries win. That is what lets an operator retune wording —
// or add a language — without waiting for a release.
func New(overrideDir string, fallback language.Tag) (*Bundle, error) {
	catalogs, err := loadEmbedded()
	if err != nil {
		return nil, err
	}
	if err := mergeOverrides(catalogs, overrideDir); err != nil {
		return nil, err
	}
	if _, ok := catalogs[Source]; !ok {
		return nil, fmt.Errorf("no %s catalog, which every other language falls back to", Source)
	}

	if _, known := catalogs[fallback]; !known || fallback == language.Und {
		fallback = Source
	}

	// The fallback comes first: language.NewMatcher treats the head of the list
	// as what to return when a request matches nothing.
	tags := []language.Tag{fallback}
	for tag := range catalogs {
		if tag != fallback {
			tags = append(tags, tag)
		}
	}
	sort.Slice(tags[1:], func(i, j int) bool { return tags[1:][i].String() < tags[1:][j].String() })

	return &Bundle{catalogs: catalogs, matcher: language.NewMatcher(tags), tags: tags, fallback: fallback}, nil
}

func loadEmbedded() (map[language.Tag]Catalog, error) {
	entries, err := embedded.ReadDir("locales")
	if err != nil {
		return nil, fmt.Errorf("read embedded locales: %w", err)
	}

	catalogs := map[language.Tag]Catalog{}
	for _, entry := range entries {
		raw, err := embedded.ReadFile(filepath.Join("locales", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		tag, catalog, err := parseCatalog(entry.Name(), raw)
		if err != nil {
			return nil, err
		}
		catalogs[tag] = catalog
	}
	return catalogs, nil
}

// mergeOverrides reads a directory an operator controls. A missing directory is
// not an error: most deployments have none.
func mergeOverrides(catalogs map[language.Tag]Catalog, dir string) error {
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		tag, catalog, err := parseCatalog(entry.Name(), raw)
		if err != nil {
			return err
		}

		if existing, ok := catalogs[tag]; ok {
			// Entry by entry, so an override file holding three strings changes
			// three strings rather than removing the rest.
			for key, value := range catalog {
				existing[key] = value
			}
			continue
		}
		catalogs[tag] = catalog
	}
	return nil
}

func parseCatalog(name string, raw []byte) (language.Tag, Catalog, error) {
	base := strings.TrimSuffix(name, ".json")
	// Parsing alone is too generous: language.Parse reads "not-a-language" as a
	// tag with extensions and returns no error. A catalog is named for a
	// language and optionally a region or script — de, pt-BR, zh-Hant — and
	// anything else is a file that landed in the directory by accident.
	if !localeName.MatchString(base) {
		return language.Und, nil, fmt.Errorf("%s is not named after a language", name)
	}

	tag, err := language.Parse(base)
	if err != nil {
		return language.Und, nil, fmt.Errorf("%s is not named after a language: %w", name, err)
	}

	var catalog Catalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return language.Und, nil, fmt.Errorf("%s: %w", name, err)
	}
	return tag, catalog, nil
}

// Match picks a language from what a browser asked for. An Accept-Language
// naming something this build does not carry gets the configured fallback
// rather than the first catalog that happens to sort first.
func (b *Bundle) Match(acceptLanguage string) language.Tag {
	if b == nil {
		return Source
	}
	if acceptLanguage == "" {
		return b.fallback
	}

	wanted, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil {
		return b.fallback
	}

	_, index, confidence := b.matcher.Match(wanted...)
	if confidence == language.No || index >= len(b.tags) {
		return b.fallback
	}
	return b.tags[index]
}

// Languages are the tags this build can render, for showing in the UI and for
// tests that assert what shipped.
func (b *Bundle) Languages() []language.Tag {
	if b == nil {
		return []language.Tag{Source}
	}
	return append([]language.Tag(nil), b.tags...)
}

// Lookup returns the string for a key, falling back to the source language and
// then to the key itself. A missing key renders as its identifier rather than
// as an empty element: a page with "admin.templates.title" on it says what is
// wrong, and an empty one does not.
func (b *Bundle) Lookup(tag language.Tag, key string) string {
	if b == nil {
		return key
	}
	if value, ok := b.catalogs[tag][key]; ok && value != "" {
		return value
	}
	if value, ok := b.catalogs[Source][key]; ok && value != "" {
		return value
	}
	return key
}

type contextKey struct{}

type translator struct {
	bundle *Bundle
	tag    language.Tag
}

// WithLanguage carries the chosen language into the handlers, and from there
// into the templ components, which take a context already. Threading a printer
// through every component's parameters instead is the sort of change that gets
// half done.
func WithLanguage(ctx context.Context, bundle *Bundle, tag language.Tag) context.Context {
	return context.WithValue(ctx, contextKey{}, translator{bundle: bundle, tag: tag})
}

// LanguageOf is what the page reports in <html lang>.
func LanguageOf(ctx context.Context) string {
	if t, ok := ctx.Value(contextKey{}).(translator); ok {
		return t.tag.String()
	}
	return Source.String()
}

// T is what a component calls. Values go in at {0}, {1} and so on rather than
// at %s: a translator can reorder them, which some languages require, and the
// numbering says plainly how many there are. It also keeps this from looking
// like a printf wrapper to anyone reading the call, including go vet.
func T(ctx context.Context, key string, args ...any) string {
	t, ok := ctx.Value(contextKey{}).(translator)
	if !ok {
		// A component rendered outside a request still renders, which is the
		// case in a test that calls one directly.
		return substitute(key, args)
	}
	return substitute(t.bundle.Lookup(t.tag, key), args)
}

// substitute replaces {0}, {1} … with the arguments. A placeholder with no
// argument is left as it is: showing {1} says a string is missing a value,
// where dropping it silently says nothing.
func substitute(format string, args []any) string {
	if len(args) == 0 {
		return format
	}

	replacements := make([]string, 0, len(args)*2)
	for i, arg := range args {
		replacements = append(replacements, fmt.Sprintf("{%d}", i), fmt.Sprint(arg))
	}
	return strings.NewReplacer(replacements...).Replace(format)
}

// Parse turns a configured language into a tag, falling back to the source
// language rather than failing to start: a typo in a setting about wording
// should not stop a service that routes alerts.
func Parse(value string) language.Tag {
	if value == "" {
		return Source
	}
	tag, err := language.Parse(value)
	if err != nil {
		return Source
	}
	return tag
}
