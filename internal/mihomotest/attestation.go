package mihomotest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const attestationVersion = 1

type TestOptions struct {
	ValidationOptions
	Record             bool
	AttestationPath    string
	PromotedConfigPath string
}

type Attestation struct {
	Version        int    `json:"version"`
	Config         string `json:"config"`
	PromotedConfig string `json:"promoted_config,omitempty"`
	WorkDir        string `json:"runtime_dir"`
	Core           string `json:"core"`
	ConfigSHA256   string `json:"config_sha256"`
	Passed         bool   `json:"passed"`
	TestedAt       string `json:"tested_at"`
}

type TestResult struct {
	ValidationResult
	AttestationPath string       `json:"attestation_path,omitempty"`
	Attestation     *Attestation `json:"attestation,omitempty"`
	Recorded        bool         `json:"recorded,omitempty"`
}

type PromoteResult struct {
	Candidate     string `json:"candidate"`
	Promoted      string `json:"promoted"`
	Attestation   string `json:"attestation"`
	ConfigSHA256  string `json:"config_sha256"`
	PromotedBytes int64  `json:"promoted_bytes"`
}

func DefaultAttestationPath(workDir string) string {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		workDir = filepath.Join(".runtime", "mihomo")
	}
	return filepath.Join(workDir, "config-test-attestation.json")
}

func Test(ctx context.Context, opts TestOptions) (TestResult, error) {
	opts.ValidationOptions = normalizeValidationOptions(opts.ValidationOptions)
	if strings.TrimSpace(opts.AttestationPath) == "" {
		opts.AttestationPath = DefaultAttestationPath(opts.WorkDir)
	}
	opts.Force = true
	validation, err := ValidateCached(ctx, opts.ValidationOptions)
	result := TestResult{ValidationResult: validation, AttestationPath: opts.AttestationPath}
	if err != nil || !validation.Passed {
		return result, err
	}
	attestation := Attestation{
		Version:        attestationVersion,
		Config:         validation.ConfigPath,
		PromotedConfig: strings.TrimSpace(opts.PromotedConfigPath),
		WorkDir:        opts.WorkDir,
		Core:           validation.CorePath,
		ConfigSHA256:   validation.ConfigSHA256,
		Passed:         true,
		TestedAt:       validation.ValidatedAt,
	}
	if attestation.TestedAt == "" {
		attestation.TestedAt = time.Now().UTC().Format(time.RFC3339)
	}
	result.Attestation = &attestation
	if opts.Record {
		if err := WriteAttestation(opts.AttestationPath, attestation); err != nil {
			result.Error = err.Error()
			return result, err
		}
		result.Recorded = true
	}
	return result, nil
}

func WriteAttestation(path string, attestation Attestation) error {
	if attestation.Version != attestationVersion {
		return fmt.Errorf("invalid mihomo config test attestation version %d", attestation.Version)
	}
	if !attestation.Passed {
		return fmt.Errorf("refusing to write non-passing mihomo config test attestation")
	}
	if strings.TrimSpace(attestation.ConfigSHA256) == "" {
		return fmt.Errorf("mihomo config test attestation requires config_sha256")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(attestation, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func ReadAttestation(path string) (Attestation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Attestation{}, err
	}
	var attestation Attestation
	if err := json.Unmarshal(data, &attestation); err != nil {
		return Attestation{}, err
	}
	if attestation.Version != attestationVersion {
		return Attestation{}, fmt.Errorf("mihomo config test attestation schema version mismatch: expected %d, got %d", attestationVersion, attestation.Version)
	}
	if !attestation.Passed {
		return Attestation{}, fmt.Errorf("mihomo config test attestation is not passing")
	}
	if strings.TrimSpace(attestation.ConfigSHA256) == "" {
		return Attestation{}, fmt.Errorf("mihomo config test attestation is missing config_sha256")
	}
	return attestation, nil
}

func ConfigSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func VerifyConfigHash(configPath, expected string) (string, error) {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return "", fmt.Errorf("expected config_sha256 is required")
	}
	actual, err := ConfigSHA256(configPath)
	if err != nil {
		return "", err
	}
	if actual != expected {
		return actual, fmt.Errorf("config hash mismatch: current config differs from last mihomo_config_test result")
	}
	return actual, nil
}

func PromoteConfig(candidate, promoted, attestationPath string) (PromoteResult, error) {
	candidate = strings.TrimSpace(candidate)
	promoted = strings.TrimSpace(promoted)
	attestationPath = strings.TrimSpace(attestationPath)
	if candidate == "" {
		return PromoteResult{}, fmt.Errorf("candidate config path is required")
	}
	if promoted == "" {
		return PromoteResult{}, fmt.Errorf("promoted config path is required")
	}
	if attestationPath == "" {
		attestationPath = DefaultAttestationPath(filepath.Dir(promoted))
	}
	attestation, err := ReadAttestation(attestationPath)
	if err != nil {
		return PromoteResult{}, err
	}
	actual, err := VerifyConfigHash(candidate, attestation.ConfigSHA256)
	if err != nil {
		return PromoteResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(promoted), 0o755); err != nil {
		return PromoteResult{}, err
	}
	if err := os.Rename(candidate, promoted); err != nil {
		return PromoteResult{}, err
	}
	info, err := os.Stat(promoted)
	if err != nil {
		return PromoteResult{}, err
	}
	return PromoteResult{
		Candidate:     candidate,
		Promoted:      promoted,
		Attestation:   attestationPath,
		ConfigSHA256:  actual,
		PromotedBytes: info.Size(),
	}, nil
}
