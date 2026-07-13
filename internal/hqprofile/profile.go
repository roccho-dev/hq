// Package hqprofile loads the exact installed runtime bindings used by hq
// clients and the managed worker. The profile is a generated deployment
// projection, not an authority or a second environment model.
package hqprofile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const Kind = "hq.profile.v1"

type Profile struct {
	Kind                   string `json:"kind"`
	Name                   string `json:"name"`
	DeploymentID           string `json:"deployment_id"`
	WorldPath              string `json:"world_path"`
	AcceptedPath           string `json:"accepted_path"`
	WorkspaceRoot          string `json:"workspace_root"`
	EventsPath             string `json:"events_path"`
	CapabilitiesPath       string `json:"capabilities_path"`
	ExecutableBindingsPath string `json:"executable_bindings_path,omitempty"`
	PollIntervalMS         int    `json:"poll_interval_ms"`
	HealthTimeoutMS        int    `json:"health_timeout_ms"`
}

func DefaultRoot() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	if strings.TrimSpace(root) == "" {
		return "", errors.New("user config directory is empty")
	}
	return filepath.Join(root, "roccho", "hq", "profiles"), nil
}
func Load(name, rootOverride string) (Profile, error) {
	if err := validateName(name); err != nil {
		return Profile{}, err
	}
	root := rootOverride
	if root == "" {
		var err error
		root, err = DefaultRoot()
		if err != nil {
			return Profile{}, err
		}
	}
	if !filepath.IsAbs(root) {
		return Profile{}, errors.New("profile root must be an absolute path")
	}
	file, err := os.Open(filepath.Join(filepath.Clean(root), name+".json"))
	if err != nil {
		return Profile{}, fmt.Errorf("open profile %q: %w", name, err)
	}
	defer file.Close()
	p, err := Decode(file)
	if err != nil {
		return Profile{}, fmt.Errorf("decode profile %q: %w", name, err)
	}
	if p.Name != name {
		return Profile{}, fmt.Errorf("profile name mismatch: requested %q, got %q", name, p.Name)
	}
	if err := p.Validate(); err != nil {
		return Profile{}, fmt.Errorf("validate profile %q: %w", name, err)
	}
	return p, nil
}
func Decode(r io.Reader) (Profile, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return Profile{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var p Profile
	if err := dec.Decode(&p); err != nil {
		return Profile{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return Profile{}, errors.New("profile contains multiple JSON values")
		}
		return Profile{}, err
	}
	return p, nil
}
func (p Profile) Validate() error {
	if p.Kind != Kind {
		return fmt.Errorf("kind must be %q", Kind)
	}
	if err := validateName(p.Name); err != nil {
		return err
	}
	if strings.TrimSpace(p.DeploymentID) == "" {
		return errors.New("deployment_id is required")
	}
	for field, value := range map[string]string{"world_path": p.WorldPath, "accepted_path": p.AcceptedPath, "workspace_root": p.WorkspaceRoot, "events_path": p.EventsPath} {
		if strings.TrimSpace(value) == "" || !filepath.IsAbs(value) {
			return fmt.Errorf("%s must be a non-empty absolute path", field)
		}
	}
	if p.CapabilitiesPath != "" && !filepath.IsAbs(p.CapabilitiesPath) {
		return errors.New("capabilities_path must be absolute when present")
	}
	if p.ExecutableBindingsPath != "" && !filepath.IsAbs(p.ExecutableBindingsPath) {
		return errors.New("executable_bindings_path must be absolute when present")
	}
	if p.PollIntervalMS < 20 || p.PollIntervalMS > 60000 {
		return errors.New("poll_interval_ms must be between 20 and 60000")
	}
	if p.HealthTimeoutMS < p.PollIntervalMS*2 || p.HealthTimeoutMS > 300000 {
		return errors.New("health_timeout_ms must be at least two poll intervals and at most 300000")
	}
	if info, err := os.Stat(p.WorkspaceRoot); err != nil || !info.IsDir() {
		return fmt.Errorf("workspace_root is not an existing directory: %s", p.WorkspaceRoot)
	}
	for field, path := range map[string]string{"world_path": p.WorldPath, "accepted_path": p.AcceptedPath} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not an existing regular file: %s", field, path)
		}
	}
	if p.CapabilitiesPath != "" {
		if info, err := os.Stat(p.CapabilitiesPath); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("capabilities_path is not an existing regular file: %s", p.CapabilitiesPath)
		}
	}
	if p.ExecutableBindingsPath != "" {
		if info, err := os.Stat(p.ExecutableBindingsPath); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("executable_bindings_path is not an existing regular file: %s", p.ExecutableBindingsPath)
		}
	}
	parent := filepath.Dir(p.EventsPath)
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return fmt.Errorf("events_path parent is not an existing directory: %s", parent)
	}
	return nil
}
func (p Profile) PollInterval() time.Duration {
	return time.Duration(p.PollIntervalMS) * time.Millisecond
}
func (p Profile) HealthTimeout() time.Duration {
	return time.Duration(p.HealthTimeoutMS) * time.Millisecond
}
func validateName(name string) error {
	if strings.TrimSpace(name) == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return fmt.Errorf("invalid profile name %q", name)
	}
	return nil
}
