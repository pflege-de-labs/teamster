package cryptutil_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/cryptutil"
)

func key(fill byte) []byte {
	k := make([]byte, cryptutil.KeySize)
	for i := range k {
		k[i] = fill
	}
	return k
}

func TestNewSealerRejectsAWrongSizedKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int
	}{
		{name: "empty", size: 0},
		{name: "16 bytes", size: 16},
		{name: "24 bytes", size: 24},
		{name: "33 bytes", size: 33},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := cryptutil.NewSealer(make([]byte, tt.size))
			if !errors.Is(err, cryptutil.ErrInvalidKeySize) {
				t.Errorf("NewSealer(%d bytes) = %v, want ErrInvalidKeySize", tt.size, err)
			}
		})
	}
}

func TestSealOpenRoundTrips(t *testing.T) {
	t.Parallel()

	sealer, err := cryptutil.NewSealer(key(0x01))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	tests := []struct {
		name      string
		plaintext string
		aad       string
	}{
		{name: "typical token", plaintext: "eyJhbGciOi...access-token", aad: "session-123"},
		{name: "empty plaintext", plaintext: "", aad: "session-abc"},
		{name: "empty aad", plaintext: "some-refresh-token", aad: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ciphertext, err := sealer.Seal([]byte(tt.plaintext), []byte(tt.aad))
			if err != nil {
				t.Fatalf("Seal: %v", err)
			}
			if tt.plaintext != "" && bytes.Contains(ciphertext, []byte(tt.plaintext)) {
				t.Error("ciphertext contains the plaintext verbatim")
			}

			got, err := sealer.Open(ciphertext, []byte(tt.aad))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if string(got) != tt.plaintext {
				t.Errorf("Open() = %q, want %q", got, tt.plaintext)
			}
		})
	}
}

func TestOpenFailsWithWrongAAD(t *testing.T) {
	t.Parallel()

	sealer, err := cryptutil.NewSealer(key(0x02))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	ciphertext, err := sealer.Seal([]byte("secret"), []byte("session-1"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if _, err := sealer.Open(ciphertext, []byte("session-2")); err == nil {
		t.Error("Open() with the wrong aad = nil error, want it refused")
	}
}

func TestOpenFailsWithWrongKey(t *testing.T) {
	t.Parallel()

	sealed, err := cryptutil.NewSealer(key(0x03))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	other, err := cryptutil.NewSealer(key(0x04))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	ciphertext, err := sealed.Seal([]byte("secret"), []byte("aad"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if _, err := other.Open(ciphertext, []byte("aad")); err == nil {
		t.Error("Open() with the wrong key = nil error, want it refused")
	}
}

func TestOpenFailsOnTamperedCiphertext(t *testing.T) {
	t.Parallel()

	sealer, err := cryptutil.NewSealer(key(0x05))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	ciphertext, err := sealer.Seal([]byte("secret"), []byte("aad"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	tampered := bytes.Clone(ciphertext)
	tampered[len(tampered)-1] ^= 0xFF

	if _, err := sealer.Open(tampered, []byte("aad")); err == nil {
		t.Error("Open() on tampered ciphertext = nil error, want it refused")
	}
}

func TestOpenFailsOnTooShortCiphertext(t *testing.T) {
	t.Parallel()

	sealer, err := cryptutil.NewSealer(key(0x06))
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}

	if _, err := sealer.Open([]byte("short"), []byte("aad")); err == nil {
		t.Error("Open() on a too-short ciphertext = nil error, want it refused")
	}
}
