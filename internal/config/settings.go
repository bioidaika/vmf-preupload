package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/bioidaika/vmf-preupload/pkg/api"
)

const appDirName = "VMFPreupload"
const currentProfile = "vmf@2"

func DefaultSettings() api.Settings {
	return api.Settings{
		ReleaseGroup:        "NoGroup",
		Separator:           ".",
		PreserveExistingP2P: true,
		Profile:             currentProfile,
	}
}

func SettingsPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appDirName, "settings.json"), nil
}

func Load() (api.Settings, error) {
	settings := DefaultSettings()
	path, err := SettingsPath()
	if err != nil {
		return settings, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// Development convenience; production UI should prompt before using
		// provider-dependent features.
		applyEnvironment(&settings)
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		return settings, err
	}
	if settings.ReleaseGroup == "" {
		settings.ReleaseGroup = "NoGroup"
	}
	if settings.Separator == "" {
		settings.Separator = "."
	}
	if needsProfileMigration(settings.Profile) {
		settings.Profile = currentProfile
	}
	applyEnvironment(&settings)
	return settings, nil
}

func Save(settings api.Settings) error {
	if strings.TrimSpace(settings.ReleaseGroup) == "" {
		settings.ReleaseGroup = "NoGroup"
	}
	if settings.Separator == "" {
		settings.Separator = "."
	}
	if needsProfileMigration(settings.Profile) {
		settings.Profile = currentProfile
	}
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	// Write with restrictive permissions on Unix; Windows honors the user
	// profile ACL in normal installations.
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func needsProfileMigration(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.EqualFold(value, "vmf@1") || strings.Contains(strings.ToLower(value), "vmf compatible")
}

func applyEnvironment(settings *api.Settings) {
	if value := strings.TrimSpace(os.Getenv("TMDB_API_KEY")); value != "" {
		settings.TMDBAPIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("TVDB_API_KEY")); value != "" {
		settings.TVDBAPIKey = value
	}
	if value := strings.TrimSpace(os.Getenv("TVDB_PIN")); value != "" {
		settings.TVDBPIN = value
	}
}
