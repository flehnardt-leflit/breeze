//go:build windows

package desktop

import (
	"compress/bzip2"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/breeze-rmm/agent/internal/config"
)

const (
	openH264DLLName = "openh264-2.4.1-win64.dll"
	openH264URL     = "https://github.com/nicedoc/openh264/releases/download/v2.4.1/openh264-2.4.1-win64.dll.bz2"
	// SHA-256 of the decompressed DLL (v2.4.1 win64), verified from Cisco's distribution.
	openH264SHA256 = "081b0c081480d177cbfddfbc90b1613640e702f875897b30d8de195cde73dd34"
	// Fallback URL — Cisco's official CDN (HTTP only, but we verify SHA-256).
	openH264FallbackURL = "http://ciscobinary.openh264.org/openh264-2.4.1-win64.dll.bz2"
)

// getDataDirFn, verifyProgramDataPathFn and downloadOpenH264DLLFn are
// package-level seams so tests can exercise findOpenH264Library's
// fail-closed gates without a real agent data directory or network.
// Production always resolves through config.GetDataDir,
// config.VerifyProgramDataPath and downloadOpenH264DLL.
var (
	getDataDirFn            = config.GetDataDir
	verifyProgramDataPathFn = config.VerifyProgramDataPath
	downloadOpenH264DLLFn   = downloadOpenH264DLL
)

// refuseUntrustedDataDir returns a fail-closed error unless this process can
// itself verify, read-only, that dataDir and every directory above it up to
// the agent ProgramData root are real directories (not links) owned by
// SYSTEM, Administrators or TrustedInstaller that no other principal can
// write. The check runs in whichever process loads the codec: the
// desktop-session helpers that encode never run the service's startup
// repair pass, so they cannot rely on its result. A directory a foreign
// principal controls could swap the file between the SHA-256 check and the
// load, or interfere with the auto-download's temp-file-then-rename, so it
// is neither read from nor written into.
func refuseUntrustedDataDir(dataDir string) error {
	if err := verifyProgramDataPathFn(dataDir); err != nil {
		return fmt.Errorf("agent data directory %q did not pass this process’s trust policy — refusing to load or stage the OpenH264 codec from it: %w", dataDir, err)
	}
	return nil
}

// findOpenH264Library searches for the OpenH264 DLL on Windows.
// Search order: next to executable, agent data dir, auto-download.
//
// A candidate found next to the executable or in the agent data dir is only
// used after its SHA-256 matches the pinned release hash — previously
// os.Stat alone was enough to trust it, so a planted file of the right name
// in either location would be loaded into the LocalSystem service unverified
// (only the fresh-download path checked the hash). A name match with a
// mismatched hash is skipped, not deleted, and the search keeps going.
//
// The agent data dir candidates (2 and 3) additionally require the directory
// and the codec file itself to pass refuseUntrustedDataDir's read-only
// verification in this process. When the directory does not verify, the
// caller (loadOpenH264) degrades to the placeholder encoder instead of
// failing the agent — hardware encoding is disabled, not the process.
func findOpenH264Library() (string, error) {
	// 1. Next to agent executable
	exePath, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exePath), openH264DLLName)
		if ok, verr := verifyFileSHA256(candidate, openH264SHA256); verr == nil && ok {
			return candidate, nil
		} else if verr == nil && !ok {
			slog.Warn("OpenH264 DLL next to executable failed checksum verification, ignoring",
				"path", candidate,
			)
		}
	}

	// 2 & 3. Agent data directory — read or written only once this process
	// has verified its ownership and permissions itself.
	dataDir := getDataDirFn()
	if derr := refuseUntrustedDataDir(dataDir); derr != nil {
		slog.Warn("OpenH264: skipping the agent data directory — it did not pass this process’s trust policy",
			"dataDir", dataDir, "error", derr.Error(),
		)
		return "", fmt.Errorf("OpenH264 DLL not found next to the executable, and the agent data directory is not trusted: %w", derr)
	}

	candidate := filepath.Join(dataDir, openH264DLLName)
	if ferr := verifyProgramDataPathFn(candidate); ferr == nil {
		if ok, verr := verifyFileSHA256(candidate, openH264SHA256); verr == nil && ok {
			return candidate, nil
		} else if verr == nil && !ok {
			slog.Warn("OpenH264 DLL in agent data dir failed checksum verification, ignoring",
				"path", candidate,
			)
		}
	} else if !errors.Is(ferr, os.ErrNotExist) {
		slog.Warn("OpenH264 DLL in agent data dir did not pass this process’s trust policy, ignoring",
			"path", candidate, "error", ferr.Error(),
		)
	}

	// 3. Auto-download from Cisco
	slog.Info("OpenH264 DLL not found or not verified locally, downloading",
		"dest", candidate,
	)
	if err := downloadOpenH264DLLFn(dataDir); err != nil {
		return "", fmt.Errorf("auto-download OpenH264: %w", err)
	}
	if ferr := verifyProgramDataPathFn(candidate); ferr != nil {
		return "", fmt.Errorf("downloaded OpenH264 DLL did not pass this process’s trust policy: %w", ferr)
	}
	if ok, err := verifyFileSHA256(candidate, openH264SHA256); err != nil {
		return "", fmt.Errorf("verify downloaded OpenH264 DLL hash: %w", err)
	} else if !ok {
		return "", fmt.Errorf("downloaded OpenH264 DLL failed SHA-256 verification")
	}
	return candidate, nil
}

func downloadOpenH264DLL(destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	// Try Cisco CDN (most reliable for this specific binary)
	var lastErr error
	for _, url := range []string{openH264FallbackURL, openH264URL} {
		if err := downloadAndVerify(url, destDir); err != nil {
			slog.Warn("OpenH264 download failed, trying next source", "url", url, "error", err.Error())
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// maxOpenH264DLLBytes caps decompression before the untrusted response can
// consume an unbounded amount of disk. The pinned 2.4.1 DLL is well below it.
const (
	maxOpenH264DLLBytes		int64 = 16 << 20
	maxOpenH264CompressedBytes	int64 = 8 << 20
)

func downloadAndVerify(url, destDir string) error {
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	return stageOpenH264DLL(destDir, bzip2.NewReader(io.LimitReader(resp.Body, maxOpenH264CompressedBytes)), openH264SHA256)
}

// stageOpenH264DLL creates an unpredictable, exclusive temporary file and
// checks the contents through that same open handle before moving it into
// place. The caller must first verify and protect destDir. This does not by
// itself pin the final file across the later LoadLibrary call.
func stageOpenH264DLL(destDir string, src io.Reader, expectedHash string) error {
	f, err := os.CreateTemp(destDir, ".openh264-*.tmp")
	if err != nil {
		return fmt.Errorf("create codec temporary file: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)
	written, copyErr := io.Copy(f, io.LimitReader(src, maxOpenH264DLLBytes+1))
	if copyErr != nil {
		_ = f.Close()
		return fmt.Errorf("decompress: %w", copyErr)
	}
	if written > maxOpenH264DLLBytes {
		_ = f.Close()
		return fmt.Errorf("decompressed OpenH264 DLL exceeds size limit")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		return fmt.Errorf("rewind temporary codec: %w", err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		_ = f.Close()
		return fmt.Errorf("hash temporary codec: %w", err)
	}
	hash := hex.EncodeToString(hasher.Sum(nil))
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temporary codec: %w", err)
	}
	if hash != expectedHash {
		return fmt.Errorf("SHA-256 mismatch: got %s, expected %s", hash, expectedHash)
	}
	finalPath := filepath.Join(destDir, openH264DLLName)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	slog.Info("OpenH264 DLL downloaded and verified", "path", finalPath, "size", written, "sha256", hash)
	return nil
}
