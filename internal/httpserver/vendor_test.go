package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// vendorDir holds browser libraries fetched at build time rather than page
// load, see internal/httpserver/web/vendor/README.md. manifest.json is the
// source of truth for what belongs there and what it should hash to.
const (
	vendorDir    = "web/vendor"
	manifestPath = vendorDir + "/manifest.json"
)

type vendorManifest struct {
	Registry  string          `json:"registry"`
	Libraries []vendorLibrary `json:"libraries"`
}

type vendorLibrary struct {
	File    string `json:"file"`
	Package string `json:"package"`
	Version string `json:"version"`
	License string `json:"license"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

func readVendorManifest(t *testing.T) vendorManifest {
	t.Helper()

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", manifestPath, err)
	}
	var m vendorManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal %s: %v", manifestPath, err)
	}
	// Every test here walks this list. An empty one would satisfy all of them
	// by having nothing to disagree with.
	if len(m.Libraries) == 0 {
		t.Fatalf("%s lists no libraries", manifestPath)
	}
	return m
}

// The layout is what actually loads these files, so it is what decides which
// ones have to exist. Without this the manifest and the directory could agree
// with each other perfectly while the admin UI served 404s for its renderer.
var vendorScriptPattern = regexp.MustCompile(`src="/vendor/([^"]+)"`)

func TestTheManifestDescribesTheScriptsTheViewsLoad(t *testing.T) {
	t.Parallel()

	referenced := map[string]string{}
	templates, err := filepath.Glob("views/*.templ")
	if err != nil {
		t.Fatalf("glob views: %v", err)
	}
	for _, name := range templates {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, match := range vendorScriptPattern.FindAllStringSubmatch(string(source), -1) {
			referenced[match[1]] = name
		}
	}
	if len(referenced) == 0 {
		t.Fatal("no view loads anything from /vendor/, so this test proves nothing; it and the manifest are now unrelated")
	}

	listed := map[string]bool{}
	for _, lib := range readVendorManifest(t).Libraries {
		listed[lib.File] = true
	}

	t.Run("every script a view loads is in the manifest", func(t *testing.T) {
		t.Parallel()

		for file, source := range referenced {
			if !listed[file] {
				t.Errorf("%s loads /vendor/%s, which %s does not list: nothing verifies it", source, file, manifestPath)
			}
		}
	})

	t.Run("every library in the manifest is loaded by a view", func(t *testing.T) {
		t.Parallel()

		for file := range listed {
			if _, ok := referenced[file]; !ok {
				t.Errorf("%s lists %s, which no view loads: drop it, or load it", manifestPath, file)
			}
		}
	})
}

// A drifted file is invisible until something hashes it — this is that check.
func TestVendoredFilesMatchTheirRecordedChecksums(t *testing.T) {
	t.Parallel()

	m := readVendorManifest(t)
	for _, lib := range m.Libraries {
		t.Run(lib.File, func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(filepath.Join(vendorDir, lib.File))
			if err != nil {
				t.Fatalf("read %s: %v", lib.File, err)
			}

			sum := sha256.Sum256(data)
			got := hex.EncodeToString(sum[:])
			if got != lib.SHA256 {
				t.Errorf("%s does not match %s@%s: recorded %s, got %s; run `make vendor`, "+
					"or `make vendor-record` if the version was just bumped",
					lib.File, lib.Package, lib.Version, lib.SHA256, got)
			}
		})
	}
}

// The manifest and the directory are two lists of the same thing kept by
// hand; nothing but a test keeps a third file from joining one list only.
func TestTheVendorDirectoryAndTheManifestAgree(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(vendorDir)
	if err != nil {
		t.Fatalf("read dir %s: %v", vendorDir, err)
	}

	onDisk := map[string]bool{}
	for _, entry := range entries {
		if entry.Name() == "README.md" || entry.Name() == "manifest.json" {
			continue
		}
		onDisk[entry.Name()] = true
	}

	m := readVendorManifest(t)
	listed := map[string]bool{}
	for _, lib := range m.Libraries {
		listed[lib.File] = true
	}

	t.Run("on disk but not in the manifest", func(t *testing.T) {
		t.Parallel()

		for name := range onDisk {
			if !listed[name] {
				t.Errorf("%s: add it to manifest.json or delete it; nothing verifies it today", name)
			}
		}
	})

	t.Run("in the manifest but not on disk", func(t *testing.T) {
		t.Parallel()

		for name := range listed {
			if !onDisk[name] {
				t.Errorf("%s: listed in manifest.json but missing from %s; run `make vendor`", name, vendorDir)
			}
		}
	})
}

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// A typo in the hand-edited manifest — an empty field, a truncated hash —
// would otherwise silently verify nothing rather than fail.
func TestTheManifestRecordsEnoughToRefetch(t *testing.T) {
	t.Parallel()

	m := readVendorManifest(t)

	if _, err := url.ParseRequestURI(m.Registry); err != nil {
		t.Errorf("registry %q does not parse as an absolute URL: %v", m.Registry, err)
	} else if u, _ := url.Parse(m.Registry); u.Scheme != "https" {
		t.Errorf("registry %q is not https", m.Registry)
	}

	seenFile := map[string]bool{}
	seenPackage := map[string]bool{}

	for _, lib := range m.Libraries {
		t.Run(lib.File, func(t *testing.T) {
			t.Parallel()

			for field, value := range map[string]string{
				"file": lib.File, "package": lib.Package, "version": lib.Version,
				"license": lib.License, "path": lib.Path,
			} {
				if value == "" {
					t.Errorf("%s is empty", field)
				}
			}
			if !sha256Pattern.MatchString(lib.SHA256) {
				t.Errorf("sha256 %q is not 64 lowercase hex characters", lib.SHA256)
			}
		})

		if seenFile[lib.File] {
			t.Errorf("file %q is listed more than once", lib.File)
		}
		seenFile[lib.File] = true

		if seenPackage[lib.Package] {
			t.Errorf("package %q is listed more than once", lib.Package)
		}
		seenPackage[lib.Package] = true
	}
}

type renovateConfig struct {
	CustomManagers []renovateCustomManager `json:"customManagers"`
}

type renovateCustomManager struct {
	ManagerFilePatterns []string `json:"managerFilePatterns"`
	MatchStrings        []string `json:"matchStrings"`
}

// The only thing that would catch a reordered key or a renamed field
// silently blinding Renovate to a vendored pin.
// The Renovate pattern matches the manifest's literal bytes, and
// `make vendor-record` rewrites those bytes with json.MarshalIndent. If the
// two ever disagree, a recorded checksum silently reformats the file out from
// under the pattern and Renovate stops seeing the pins.
func TestTheManifestIsWrittenTheWayRecordWritesIt(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", manifestPath, err)
	}

	encoded, err := json.MarshalIndent(readVendorManifest(t), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	encoded = append(encoded, '\n')

	if string(raw) != string(encoded) {
		t.Errorf("%s is not what `make vendor-record` would write; run it, or match its two-space indent and trailing newline", manifestPath)
	}
}

func TestRenovateSeesEveryVendoredPin(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read %s: %v", manifestPath, err)
	}
	m := readVendorManifest(t)

	renovateRaw, err := os.ReadFile("../../renovate.json")
	if err != nil {
		t.Fatalf("read renovate.json: %v", err)
	}
	var cfg renovateConfig
	if err := json.Unmarshal(renovateRaw, &cfg); err != nil {
		t.Fatalf("unmarshal renovate.json: %v", err)
	}

	var manager *renovateCustomManager
	for i := range cfg.CustomManagers {
		for _, pattern := range cfg.CustomManagers[i].ManagerFilePatterns {
			if strings.Contains(pattern, "vendor/manifest") {
				manager = &cfg.CustomManagers[i]
			}
		}
	}
	if manager == nil {
		t.Fatalf("renovate.json has no customManager matching %s", manifestPath)
	}
	// Only the first pattern is checked below, so a second one added later
	// would go unverified. Fail loudly instead of silently covering less.
	if len(manager.MatchStrings) != 1 {
		t.Fatalf("the vendor manifest customManager has %d matchStrings, want 1; extend this test before adding another",
			len(manager.MatchStrings))
	}

	re, err := regexp.Compile(manager.MatchStrings[0])
	if err != nil {
		t.Fatalf("compile matchStrings[0]: %v", err)
	}

	matches := re.FindAllStringSubmatch(string(raw), -1)
	if len(matches) != len(m.Libraries) {
		t.Fatalf("renovate pattern found %d matches, want %d (one per library)", len(matches), len(m.Libraries))
	}

	depName := re.SubexpIndex("depName")
	currentValue := re.SubexpIndex("currentValue")
	if depName == -1 || currentValue == -1 {
		t.Fatalf("renovate pattern has no depName/currentValue capture groups")
	}

	for i, lib := range m.Libraries {
		if got := matches[i][depName]; got != lib.Package {
			t.Errorf("match %d: depName = %q, want %q", i, got, lib.Package)
		}
		if got := matches[i][currentValue]; got != lib.Version {
			t.Errorf("match %d: currentValue = %q, want %q", i, got, lib.Version)
		}
	}
}

// Every file the manifest pins has to be reachable, or a browser gets a 404
// for a script the admin pages ask for.
func TestEveryVendoredFileIsServed(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	m := readVendorManifest(t)

	for _, lib := range m.Libraries {
		rec := probe(t, handler, http.MethodGet, "/vendor/"+lib.File)
		if rec.Code != http.StatusOK {
			t.Errorf("GET /vendor/%s = %d, want 200", lib.File, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET /vendor/%s served nothing", lib.File)
		}
	}
}
