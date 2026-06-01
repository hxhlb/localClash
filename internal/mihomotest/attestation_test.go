package mihomotest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReadAttestationAndPromoteConfig(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, "config.candidate.yaml")
	promoted := filepath.Join(dir, "config.yaml")
	attestationPath := filepath.Join(dir, "config-test-attestation.json")
	if err := os.WriteFile(candidate, []byte("mixed-port: 7890\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := ConfigSHA256(candidate)
	if err != nil {
		t.Fatal(err)
	}
	attestation := Attestation{
		Version:      attestationVersion,
		Config:       candidate,
		WorkDir:      dir,
		Core:         filepath.Join(dir, "lc-mihomo-meta"),
		ConfigSHA256: sha,
		Passed:       true,
		TestedAt:     "2026-06-01T00:00:00Z",
	}
	if err := WriteAttestation(attestationPath, attestation); err != nil {
		t.Fatal(err)
	}
	read, err := ReadAttestation(attestationPath)
	if err != nil {
		t.Fatal(err)
	}
	if read.ConfigSHA256 != sha {
		t.Fatalf("attestation sha = %q, want %q", read.ConfigSHA256, sha)
	}
	result, err := PromoteConfig(candidate, promoted, attestationPath)
	if err != nil {
		t.Fatal(err)
	}
	if result.ConfigSHA256 != sha || result.PromotedBytes == 0 {
		t.Fatalf("promote result = %+v", result)
	}
	if _, err := os.Stat(candidate); !os.IsNotExist(err) {
		t.Fatalf("candidate should be moved, err=%v", err)
	}
	if _, err := os.Stat(promoted); err != nil {
		t.Fatalf("promoted config missing: %v", err)
	}
}

func TestPromoteConfigRejectsHashMismatch(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, "config.candidate.yaml")
	promoted := filepath.Join(dir, "config.yaml")
	attestationPath := filepath.Join(dir, "config-test-attestation.json")
	if err := os.WriteFile(candidate, []byte("mixed-port: 7890\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteAttestation(attestationPath, Attestation{
		Version:      attestationVersion,
		Config:       candidate,
		WorkDir:      dir,
		Core:         filepath.Join(dir, "lc-mihomo-meta"),
		ConfigSHA256: strings.Repeat("0", 64),
		Passed:       true,
		TestedAt:     "2026-06-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := PromoteConfig(candidate, promoted, attestationPath)
	if err == nil || !strings.Contains(err.Error(), "config hash mismatch") {
		t.Fatalf("error = %v, want hash mismatch", err)
	}
	if _, err := os.Stat(candidate); err != nil {
		t.Fatalf("candidate should remain after failed promote: %v", err)
	}
}
