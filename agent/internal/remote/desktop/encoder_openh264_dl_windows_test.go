//go:build windows

package desktop

import (
	"compress/bzip2"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/breeze-rmm/agent/internal/config"
)

// swapVerifyProgramDataPathFn replaces the read-only ProgramData verifier
// seam and restores it after the test. Production always resolves through
// config.VerifyProgramDataPath.
func swapVerifyProgramDataPathFn(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := verifyProgramDataPathFn
	verifyProgramDataPathFn = fn
	t.Cleanup(func() { verifyProgramDataPathFn = orig })
}

func swapGetDataDirFn(t *testing.T, dir string) {
	t.Helper()
	orig := getDataDirFn
	getDataDirFn = func() string { return dir }
	t.Cleanup(func() { getDataDirFn = orig })
}

func swapDownloadFn(t *testing.T, fn func(string) error) {
	t.Helper()
	orig := downloadOpenH264DLLFn
	downloadOpenH264DLLFn = fn
	t.Cleanup(func() { downloadOpenH264DLLFn = orig })
}

func TestRefuseUntrustedDataDir(t *testing.T) {
	swapVerifyProgramDataPathFn(t, func(string) error { return errors.New("owned by a standard user") })
	if err := refuseUntrustedDataDir(`C:\ProgramData\Breeze\data`); err == nil || !strings.Contains(err.Error(), "owned by a standard user") || !strings.Contains(err.Error(), "trust policy") {
		t.Fatalf("expected the verifier’s diagnostic and trust policy, got %v", err)
	}
}

func TestRefuseUntrustedDataDir_AllowsVerifiedDir(t *testing.T) {
	swapVerifyProgramDataPathFn(t, func(string) error { return nil })
	if err := refuseUntrustedDataDir(`C:\ProgramData\Breeze\data`); err != nil {
		t.Errorf("expected no refusal for a verified data directory, got %v", err)
	}
}

// The desktop-session helper that encodes never runs the service's startup
// repair pass, so its in-memory trust record is always empty. The decision
// must come from this process's own read-only verification.
func TestRefuseUntrustedDataDir_DoesNotDependOnTheServiceRepairPass(t *testing.T) {
	dir := `C:\ProgramData\Breeze\data`
	if config.ProgramDataDirTrusted(dir) {
		t.Fatal("precondition: this test process never ran the repair pass")
	}
	var verified []string
	swapVerifyProgramDataPathFn(t, func(p string) error { verified = append(verified, p); return nil })
	if err := refuseUntrustedDataDir(dir); err != nil {
		t.Fatalf("a directory this process verified must be usable without the service pass, got %v", err)
	}
	if len(verified) != 1 || verified[0] != dir {
		t.Fatalf("expected the data directory to be verified in-process, got %v", verified)
	}
}

func TestFindOpenH264Library_RefusesUntrustedDataDir(t *testing.T) {
	dataDir := t.TempDir()
	swapGetDataDirFn(t, dataDir)
	swapVerifyProgramDataPathFn(t, func(string) error { return errors.New("is a link") })
	swapDownloadFn(t, func(string) error { t.Error("must not download into an unverified directory"); return nil })

	planted := filepath.Join(dataDir, openH264DLLName)
	if err := os.WriteFile(planted, []byte("not a real dll"), 0o644); err != nil {
		t.Fatalf("seed planted file: %v", err)
	}

	if _, err := findOpenH264Library(); err == nil {
		t.Fatal("expected findOpenH264Library to fail closed when the data dir does not verify")
	}
}

// A codec file that does not itself verify (e.g. it is a link, or a user can
// write it) is never hashed or returned, even inside a verified directory.
func TestFindOpenH264Library_SkipsUnverifiedCodecFile(t *testing.T) {
	dataDir := t.TempDir()
	swapGetDataDirFn(t, dataDir)
	candidate := filepath.Join(dataDir, openH264DLLName)
	if err := os.WriteFile(candidate, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	swapVerifyProgramDataPathFn(t, func(p string) error {
		if p == candidate {
			return fmt.Errorf("%s is a link", p)
		}
		return nil
	})
	downloaded := false
	swapDownloadFn(t, func(string) error { downloaded = true; return errors.New("offline") })

	path, err := findOpenH264Library()
	if err == nil {
		t.Fatalf("expected no codec, got %s", path)
	}
	if !downloaded {
		t.Error("an unverified codec file must be replaced through the verified download path, not used")
	}
}

// A freshly downloaded codec must itself verify before it is handed to the
// loader.
func TestFindOpenH264Library_VerifiesDownloadedFile(t *testing.T) {
	dataDir := t.TempDir()
	swapGetDataDirFn(t, dataDir)
	candidate := filepath.Join(dataDir, openH264DLLName)
	var checks int
	swapVerifyProgramDataPathFn(t, func(p string) error {
		if p == candidate {
			checks++
			return fmt.Errorf("%s: %w", p, os.ErrNotExist)
		}
		return nil
	})
	swapDownloadFn(t, func(string) error { return nil })

	_, err := findOpenH264Library()
	if err == nil || !strings.Contains(err.Error(), "downloaded") {
		t.Fatalf("a downloaded codec that does not verify must be refused, got %v", err)
	}
	if checks < 2 {
		t.Errorf("expected the codec file to be verified before and after the download, got %d checks", checks)
	}
}

// TestStageOpenH264DLL verifies the bytes staged for installation, rather
// than trusting the bytes reported by the download stream. No network is used.
func TestStageOpenH264DLL(t *testing.T) {
	const payload = "small test codec payload"
	sum := sha256.Sum256([]byte(payload))
	goodHash := hex.EncodeToString(sum[:])
	tests := []struct {
		name	string
		input	io.Reader
		hash	string
		wantErr bool
	}{
		{name: "verified bytes", input: strings.NewReader(payload), hash: goodHash},
		{name: "invalid hash", input: strings.NewReader(payload), hash: strings.Repeat("0", 64), wantErr: true},
		{name: "empty input", input: strings.NewReader(""), hash: goodHash, wantErr: true},
		{name: "oversized input", input: io.LimitReader(zeroReader{}, maxOpenH264DLLBytes+1), hash: goodHash, wantErr: true},
		{name: "read failure", input: failingOpenH264Reader{}, hash: goodHash, wantErr: true},
		{name: "truncated bzip2", input: bzip2.NewReader(strings.NewReader("BZh9")), hash: goodHash, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			err := stageOpenH264DLL(dir, tc.input, tc.hash)
			if (err != nil) != tc.wantErr {
				t.Fatalf("stageOpenH264DLL() error = %v, wantErr %v", err, tc.wantErr)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantErr {
				if len(entries) != 0 {
					t.Fatalf("failed staging left files: %v", entries)
				}
				return
			}
			if len(entries) != 1 || entries[0].Name() != openH264DLLName {
				t.Fatalf("unexpected staged files: %v", entries)
			}
			got, err := os.ReadFile(filepath.Join(dir, openH264DLLName))
			if err != nil || string(got) != payload {
				t.Fatalf("staged bytes %q, error %v", got, err)
			}
		})
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

type failingOpenH264Reader struct{}

func (failingOpenH264Reader) Read(p []byte) (int, error) {
	return copy(p, "partial"), errors.New("injected read failure")
}

func TestFindOpenH264Library_RejectsDownloadedHashMismatch(t *testing.T) {
	dataDir := t.TempDir()
	swapGetDataDirFn(t, dataDir)
	swapVerifyProgramDataPathFn(t, func(string) error { return nil })
	swapDownloadFn(t, func(dir string) error {
		return os.WriteFile(filepath.Join(dir, openH264DLLName), []byte("tampered"), 0o600)
	})
	_, err := findOpenH264Library()
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("downloaded bytes with an incorrect hash must not be returned: %v", err)
	}
}

// A failed download must not truncate an existing DLL or the predictable
// temporary filename used by older versions of the downloader.
func TestStageOpenH264DLL_FailurePreservesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{openH264DLLName, openH264DLLName + ".tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := stageOpenH264DLL(dir, strings.NewReader("untrusted"), strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected mismatch")
	}
	for _, name := range []string{openH264DLLName, openH264DLLName + ".tmp"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(got) != "keep" {
			t.Fatalf("existing file %s changed: %q, %v", name, got, err)
		}
	}
}
