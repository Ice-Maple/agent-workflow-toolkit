package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"agent-workflow-toolkit/internal/orchestration"
	"agent-workflow-toolkit/internal/policy"
)

func LoadProfile(path string) (Profile, error) {
	if strings.TrimSpace(path) == "" {
		return Profile{}, errors.New("profile file is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("read adapter profile: %w", err)
	}
	var profile Profile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		return Profile{}, fmt.Errorf("decode adapter profile: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Profile{}, errors.New("adapter profile contains more than one JSON value")
		}
		return Profile{}, fmt.Errorf("decode adapter profile trailing data: %w", err)
	}
	if profile.TrustAuthorized || trustedProfileValue(profile.Trust) {
		profile.TrustAuthorized = true
	}
	if err := validateProfile(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func LoadBinding(path string) (orchestration.Binding, error) {
	if strings.TrimSpace(path) == "" {
		return orchestration.Binding{}, errors.New("binding file is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return orchestration.Binding{}, fmt.Errorf("read binding: %w", err)
	}
	var binding orchestration.Binding
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&binding); err != nil {
		return orchestration.Binding{}, fmt.Errorf("decode binding: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return orchestration.Binding{}, errors.New("binding contains more than one JSON value")
		}
		return orchestration.Binding{}, fmt.Errorf("decode binding trailing data: %w", err)
	}
	return binding, nil
}

func LoadProfileFromPolicy(path, profileID string) (Profile, error) {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(profileID) == "" {
		return Profile{}, errors.New("policy file and profile id are required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("read policy for adapter profile: %w", err)
	}
	config, err := policy.LoadJSON(raw)
	if err != nil {
		return Profile{}, fmt.Errorf("load policy for adapter profile: %w", err)
	}
	adapterProfile, ok := config.AdapterProfiles[profileID]
	if !ok {
		return Profile{}, fmt.Errorf("adapter profile %q is not registered", profileID)
	}
	profile := Profile{AdapterProfile: adapterProfile, TrustAuthorized: trustedProfileValue(adapterProfile.Trust)}
	if err := validateProfile(profile); err != nil {
		return Profile{}, err
	}
	if !strings.EqualFold(profile.ProfileHash, orchestration.HashAdapterProfile(profile.AdapterProfile)) {
		return Profile{}, errors.New("adapter profile content hash mismatch")
	}
	return profile, nil
}

func trustedProfileValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "authorized", "trusted", "true", "yes":
		return true
	default:
		return false
	}
}
