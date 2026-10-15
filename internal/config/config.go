// Package config loads and validates Lum Harbor's process-scoped runtime
// configuration.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"lum-harbor-notifier/internal/appdir"
)

const configFileName = "config.json"

// Config is the validated, process-scoped configuration assembled at startup.
// The composition root passes only the relevant nested values to consumers;
// repositories and services do not read configuration files themselves.
type Config struct {
	General       GeneralConfig       `json:"general"`
	Logging       LoggingConfig       `json:"logging"`
	Dashboard     DashboardConfig     `json:"dashboard"`
	Notifications NotificationsConfig `json:"notifications"`
	Plugins       PluginsConfig       `json:"plugins"`
	Background    BackgroundConfig    `json:"background"`
	AI            AIConfig            `json:"ai"`
}

const (
	CloseWindowBehaviorBackground = "background"
	CloseWindowBehaviorQuit       = "quit"
)

// GeneralConfig controls application-wide desktop shell preferences.
type GeneralConfig struct {
	CloseWindowBehavior string `json:"closeWindowBehavior"`
}

// LoggingConfig controls structured console and rotating-file log output.
// An empty Directory uses the shared application log directory.
type LoggingConfig struct {
	Level      string `json:"level"`
	Console    bool   `json:"console"`
	File       bool   `json:"file"`
	Directory  string `json:"directory"`
	FileName   string `json:"fileName"`
	MaxSizeMB  int    `json:"maxSizeMB"`
	MaxBackups int    `json:"maxBackups"`
	MaxAgeDays int    `json:"maxAgeDays"`
	Compress   bool   `json:"compress"`
}

// DashboardConfig contains presentation settings needed by the frontend.
// TodoDisplayLimit bounds only rendered Dashboard rows; backend Current Open
// membership and its full count remain unchanged.
type DashboardConfig struct {
	TodoDisplayLimit int `json:"todoDisplayLimit"`
}

const (
	NotificationPlacementTopLeft      = "TOP_LEFT"
	NotificationPlacementTopCenter    = "TOP_CENTER"
	NotificationPlacementTopRight     = "TOP_RIGHT"
	NotificationPlacementBottomLeft   = "BOTTOM_LEFT"
	NotificationPlacementBottomCenter = "BOTTOM_CENTER"
	NotificationPlacementBottomRight  = "BOTTOM_RIGHT"
)

// NotificationsConfig contains presentation-only preferences. It does not
// change durable Notification, Attention, reminder, or external delivery
// semantics.
type NotificationsConfig struct {
	InApp InAppNotificationConfig `json:"inApp"`
}

// InAppNotificationConfig controls the frontend alert stack. The selected
// duration is retained while auto-dismiss is disabled so re-enabling it can
// restore the user's last choice.
type InAppNotificationConfig struct {
	AutoDismiss        bool   `json:"autoDismiss"`
	AutoDismissSeconds int    `json:"autoDismissSeconds"`
	Placement          string `json:"placement"`
}

// PluginsConfig contains process-level settings used by production composition
// to discover extensions, resolve selected instances, and construct Plugin
// Host. Scheduling and runtime state remain outside this config.
type PluginsConfig struct {
	Directory             string                 `json:"directory"`
	StartupTimeoutSeconds int                    `json:"startupTimeoutSeconds"`
	RequestTimeoutSeconds int                    `json:"requestTimeoutSeconds"`
	Instances             []PluginInstanceConfig `json:"instances,omitempty"`
}

// PluginInstanceConfig is one persisted user selection. Runtime resolution
// converts it to plugin.Instance after discovery validates the referenced
// extension, capability, and manifest-owned configuration schema.
//
// Disabled is a desired-configuration fact, not runtime state: omitted or
// false means enabled, matching every declaration that existed before this
// field was introduced. A disabled declaration keeps its stable ID and
// config so Stop Using/Re-enable never changes Instance identity; it is
// simply excluded from runtime resolution until re-enabled.
type PluginInstanceConfig struct {
	ID           string          `json:"id"`
	ExtensionID  string          `json:"extensionId"`
	CapabilityID string          `json:"capabilityId"`
	Config       json.RawMessage `json:"config,omitempty"`
	Disabled     bool            `json:"disabled,omitempty"`
}

// BackgroundConfig contains the application-wide shutdown budget shared by
// ordered Scheduler and Plugin Host lifecycle composition.
type BackgroundConfig struct {
	ShutdownTimeoutSeconds int `json:"shutdownTimeoutSeconds"`
}

// AIConfig contains non-secret, next-start AI provider configuration.
type AIConfig struct {
	BuiltInOpenAI BuiltInOpenAIConfig `json:"builtInOpenAI"`
}

// BuiltInOpenAIConfig deliberately excludes credentials. Endpoint is an
// OpenAI-compatible API base URL (for example https://api.openai.com/v1).
//
// UseSystemCredentialManager is macOS-only desired credential-store
// selection: true (the default) means the real system Keychain; false means
// Lum Harbor's own application-managed fallback store. Every other platform
// has exactly one production credential store and ignores this value
// entirely — it is never surfaced as a choice outside macOS. Defaulting to
// true (via DefaultConfig) is required for backward compatibility: an
// existing config.json written before this field existed must continue to
// resolve the same system Keychain behavior every current macOS user
// already depends on.
type BuiltInOpenAIConfig struct {
	Endpoint                   string `json:"endpoint"`
	Model                      string `json:"model"`
	RequestTimeoutSeconds      int    `json:"requestTimeoutSeconds"`
	UseSystemCredentialManager bool   `json:"useSystemCredentialManager"`
}

func (c BuiltInOpenAIConfig) Configured() bool {
	return strings.TrimSpace(c.Endpoint) != "" && strings.TrimSpace(c.Model) != ""
}

// DefaultConfig returns a complete, valid configuration. Load applies explicit
// JSON values over this baseline, so omitted sections and nested fields retain
// their defaults.
func DefaultConfig() Config {
	return Config{
		General: GeneralConfig{
			CloseWindowBehavior: CloseWindowBehaviorBackground,
		},
		Logging: LoggingConfig{
			Level:      "info",
			Console:    true,
			File:       true,
			Directory:  "",
			FileName:   "lum-harbor.log",
			MaxSizeMB:  1,
			MaxBackups: 5,
			MaxAgeDays: 7,
			Compress:   false,
		},
		Dashboard: DashboardConfig{TodoDisplayLimit: 6},
		Notifications: NotificationsConfig{InApp: InAppNotificationConfig{
			AutoDismiss:        false,
			AutoDismissSeconds: 600,
			Placement:          NotificationPlacementBottomRight,
		}},
		Plugins: PluginsConfig{
			Directory:             "plugins",
			StartupTimeoutSeconds: 10,
			RequestTimeoutSeconds: 30,
		},
		Background: BackgroundConfig{ShutdownTimeoutSeconds: 10},
		AI: AIConfig{BuiltInOpenAI: BuiltInOpenAIConfig{
			RequestTimeoutSeconds:      30,
			UseSystemCredentialManager: true,
		}},
	}
}

// Load reads the optional config.json from Lum Harbor's shared application
// directory, overlays explicitly supplied fields on DefaultConfig, and
// validates the final result. A missing file is the only read condition that
// falls back to defaults; malformed or invalid files fail startup.
func Load() (Config, error) {
	directory, err := appdir.ConfigDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve runtime config directory: %w", err)
	}
	return loadFile(filepath.Join(directory, configFileName))
}

// Save validates and atomically persists a complete Config to Lum Harbor's
// shared application directory. Callers are expected to have freshly loaded
// the current desired configuration and applied a narrow mutation to it, not
// to reuse a stale in-memory snapshot; Save performs no merge of its own.
func Save(cfg Config) error {
	directory, err := appdir.ConfigDir()
	if err != nil {
		return fmt.Errorf("resolve runtime config directory: %w", err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create runtime config directory %q: %w", directory, err)
	}
	return saveFile(filepath.Join(directory, configFileName), cfg)
}

// saveFile implements the deterministic atomic write boundary used by Save
// and tests. It validates and encodes before touching the filesystem, writes
// to a same-directory temporary file, and publishes with a same-filesystem
// rename so a crash or write failure never leaves a partially written config
// file in place of an existing valid one.
func saveFile(path string, cfg Config) (resultErr error) {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("validate runtime config before save: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime config: %w", err)
	}
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temporary runtime config: %w", err)
	}
	tempPath := temp.Name()
	published := false
	defer func() {
		if !published {
			if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) && resultErr == nil {
				resultErr = fmt.Errorf("clean temporary runtime config %q: %w", tempPath, removeErr)
			}
		}
	}()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary runtime config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary runtime config: %w", err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return fmt.Errorf("set runtime config permissions: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish runtime config %q: %w", path, err)
	}
	published = true
	return nil
}

// loadFile implements the deterministic file boundary used by Load and tests.
// Pointer fields in configOverride distinguish omission from explicit zero or
// false values, which preserves defaults during partial nested overrides.
func loadFile(path string) (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("read runtime config %q: %w", path, err)
	}

	var override configOverride
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&override); err != nil {
		return Config{}, fmt.Errorf("decode runtime config %q: %w", path, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Config{}, fmt.Errorf("decode runtime config %q: %w", path, err)
	}

	override.apply(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate runtime config %q: %w", path, err)
	}
	return cfg, nil
}

// Validate rejects invalid runtime values with field-qualified errors. Values
// are never clamped or silently replaced after an explicit user override.
func (c Config) Validate() error {
	switch strings.ToLower(c.General.CloseWindowBehavior) {
	case CloseWindowBehaviorBackground, CloseWindowBehaviorQuit:
	default:
		return fmt.Errorf("general.closeWindowBehavior must be %q or %q", CloseWindowBehaviorBackground, CloseWindowBehaviorQuit)
	}
	switch strings.ToLower(c.Logging.Level) {
	case "debug", "info", "warn", "warning", "error":
	default:
		return fmt.Errorf("logging.level must be debug, info, warn, warning, or error")
	}
	if !c.Logging.Console && !c.Logging.File {
		return fmt.Errorf("logging must enable at least one of console or file")
	}
	if c.Logging.File {
		if strings.TrimSpace(c.Logging.FileName) == "" {
			return fmt.Errorf("logging.fileName must not be empty when file logging is enabled")
		}
		if c.Logging.MaxSizeMB <= 0 {
			return fmt.Errorf("logging.maxSizeMB must be greater than 0")
		}
		if c.Logging.MaxBackups < 0 {
			return fmt.Errorf("logging.maxBackups must be at least 0")
		}
		if c.Logging.MaxAgeDays < 0 {
			return fmt.Errorf("logging.maxAgeDays must be at least 0")
		}
	}
	if c.Dashboard.TodoDisplayLimit < 1 || c.Dashboard.TodoDisplayLimit > 20 {
		return fmt.Errorf("dashboard.todoDisplayLimit must be between 1 and 20")
	}
	switch c.Notifications.InApp.AutoDismissSeconds {
	case 10, 30, 60, 300, 600, 900, 1800:
	default:
		return fmt.Errorf("notifications.inApp.autoDismissSeconds must be 10, 30, 60, 300, 600, 900, or 1800")
	}
	switch c.Notifications.InApp.Placement {
	case NotificationPlacementTopLeft,
		NotificationPlacementTopCenter,
		NotificationPlacementTopRight,
		NotificationPlacementBottomLeft,
		NotificationPlacementBottomCenter,
		NotificationPlacementBottomRight:
	default:
		return fmt.Errorf("notifications.inApp.placement must be a supported placement")
	}
	if strings.TrimSpace(c.Plugins.Directory) == "" {
		return fmt.Errorf("plugins.directory must not be empty")
	}
	if c.Plugins.StartupTimeoutSeconds <= 0 {
		return fmt.Errorf("plugins.startupTimeoutSeconds must be greater than 0")
	}
	if c.Plugins.RequestTimeoutSeconds <= 0 {
		return fmt.Errorf("plugins.requestTimeoutSeconds must be greater than 0")
	}
	if c.Background.ShutdownTimeoutSeconds <= 0 {
		return fmt.Errorf("background.shutdownTimeoutSeconds must be greater than 0")
	}
	aiConfig := c.AI.BuiltInOpenAI
	endpointSet := strings.TrimSpace(aiConfig.Endpoint) != ""
	modelSet := strings.TrimSpace(aiConfig.Model) != ""
	if endpointSet != modelSet {
		return fmt.Errorf("ai.builtInOpenAI.endpoint and model must either both be configured or both be empty")
	}
	if aiConfig.RequestTimeoutSeconds <= 0 || aiConfig.RequestTimeoutSeconds > 300 {
		return fmt.Errorf("ai.builtInOpenAI.requestTimeoutSeconds must be between 1 and 300")
	}
	if endpointSet {
		parsed, err := url.Parse(aiConfig.Endpoint)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("ai.builtInOpenAI.endpoint must be an absolute http(s) base URL without credentials, query, or fragment")
		}
		if strings.TrimSpace(aiConfig.Model) != aiConfig.Model {
			return fmt.Errorf("ai.builtInOpenAI.model must not have leading or trailing whitespace")
		}
	}
	return nil
}

// configOverride is the file representation used to apply top-level partial
// overrides without replacing complete nested default structs.
type configOverride struct {
	General       *generalOverride       `json:"general"`
	Logging       *loggingOverride       `json:"logging"`
	Dashboard     *dashboardOverride     `json:"dashboard"`
	Notifications *notificationsOverride `json:"notifications"`
	Plugins       *pluginsOverride       `json:"plugins"`
	Background    *backgroundOverride    `json:"background"`
	AI            *aiOverride            `json:"ai"`
}

type generalOverride struct {
	CloseWindowBehavior      *string `json:"closeWindowBehavior"`
	CloseWindowBehaviorSnake *string `json_key:"close_window_behavior" json:"close_window_behavior"`
}

func (o generalOverride) apply(cfg *GeneralConfig) {
	if o.CloseWindowBehavior != nil && strings.TrimSpace(*o.CloseWindowBehavior) != "" {
		cfg.CloseWindowBehavior = strings.ToLower(strings.TrimSpace(*o.CloseWindowBehavior))
	} else if o.CloseWindowBehaviorSnake != nil && strings.TrimSpace(*o.CloseWindowBehaviorSnake) != "" {
		cfg.CloseWindowBehavior = strings.ToLower(strings.TrimSpace(*o.CloseWindowBehaviorSnake))
	}
}

// loggingOverride uses pointers so false and zero remain explicit user values.
type loggingOverride struct {
	Level      *string `json:"level"`
	Console    *bool   `json:"console"`
	File       *bool   `json:"file"`
	Directory  *string `json:"directory"`
	FileName   *string `json:"fileName"`
	MaxSizeMB  *int    `json:"maxSizeMB"`
	MaxBackups *int    `json:"maxBackups"`
	MaxAgeDays *int    `json:"maxAgeDays"`
	Compress   *bool   `json:"compress"`
}

// dashboardOverride applies only explicitly present Dashboard fields.
type dashboardOverride struct {
	TodoDisplayLimit *int `json:"todoDisplayLimit"`
}

type notificationsOverride struct {
	InApp *inAppNotificationOverride `json:"inApp"`
}

type inAppNotificationOverride struct {
	AutoDismiss        *bool `json:"autoDismiss"`
	AutoDismissSeconds *int  `json:"autoDismissSeconds"`
	// AutoDismissMinutes accepts the immediately preceding preference shape so
	// an existing config.json migrates on its next ordinary save.
	AutoDismissMinutes *int    `json:"autoDismissMinutes"`
	Placement          *string `json:"placement"`
}

// pluginsOverride applies only explicitly present Plugin Host fields.
type pluginsOverride struct {
	Directory             *string                 `json:"directory"`
	StartupTimeoutSeconds *int                    `json:"startupTimeoutSeconds"`
	RequestTimeoutSeconds *int                    `json:"requestTimeoutSeconds"`
	Instances             *[]PluginInstanceConfig `json:"instances"`
}

// backgroundOverride applies only explicitly present scheduler lifecycle fields.
type backgroundOverride struct {
	ShutdownTimeoutSeconds *int `json:"shutdownTimeoutSeconds"`
}

type aiOverride struct {
	BuiltInOpenAI *builtInOpenAIOverride `json:"builtInOpenAI"`
}

type builtInOpenAIOverride struct {
	Endpoint                   *string `json:"endpoint"`
	Model                      *string `json:"model"`
	RequestTimeoutSeconds      *int    `json:"requestTimeoutSeconds"`
	UseSystemCredentialManager *bool   `json:"useSystemCredentialManager"`
}

// apply merges each present file section into an existing default Config.
func (o configOverride) apply(cfg *Config) {
	if o.General != nil {
		o.General.apply(&cfg.General)
	}
	if o.Logging != nil {
		o.Logging.apply(&cfg.Logging)
	}
	if o.Dashboard != nil && o.Dashboard.TodoDisplayLimit != nil {
		cfg.Dashboard.TodoDisplayLimit = *o.Dashboard.TodoDisplayLimit
	}
	if o.Notifications != nil && o.Notifications.InApp != nil {
		o.Notifications.InApp.apply(&cfg.Notifications.InApp)
	}
	if o.Plugins != nil {
		o.Plugins.apply(&cfg.Plugins)
	}
	if o.Background != nil && o.Background.ShutdownTimeoutSeconds != nil {
		cfg.Background.ShutdownTimeoutSeconds = *o.Background.ShutdownTimeoutSeconds
	}
	if o.AI != nil && o.AI.BuiltInOpenAI != nil {
		o.AI.BuiltInOpenAI.apply(&cfg.AI.BuiltInOpenAI)
	}
}

func (o inAppNotificationOverride) apply(cfg *InAppNotificationConfig) {
	if o.AutoDismiss != nil {
		cfg.AutoDismiss = *o.AutoDismiss
	}
	if o.AutoDismissSeconds != nil {
		cfg.AutoDismissSeconds = *o.AutoDismissSeconds
	} else if o.AutoDismissMinutes != nil {
		cfg.AutoDismissSeconds = *o.AutoDismissMinutes * 60
	}
	if o.Placement != nil {
		cfg.Placement = *o.Placement
	}
}

func (o builtInOpenAIOverride) apply(cfg *BuiltInOpenAIConfig) {
	if o.Endpoint != nil {
		cfg.Endpoint = *o.Endpoint
	}
	if o.Model != nil {
		cfg.Model = *o.Model
	}
	if o.RequestTimeoutSeconds != nil {
		cfg.RequestTimeoutSeconds = *o.RequestTimeoutSeconds
	}
	if o.UseSystemCredentialManager != nil {
		cfg.UseSystemCredentialManager = *o.UseSystemCredentialManager
	}
}

// apply preserves every omitted nested Logging value from DefaultConfig.
func (o loggingOverride) apply(cfg *LoggingConfig) {
	if o.Level != nil {
		cfg.Level = *o.Level
	}
	if o.Console != nil {
		cfg.Console = *o.Console
	}
	if o.File != nil {
		cfg.File = *o.File
	}
	if o.Directory != nil {
		cfg.Directory = *o.Directory
	}
	if o.FileName != nil {
		cfg.FileName = *o.FileName
	}
	if o.MaxSizeMB != nil {
		cfg.MaxSizeMB = *o.MaxSizeMB
	}
	if o.MaxBackups != nil {
		cfg.MaxBackups = *o.MaxBackups
	}
	if o.MaxAgeDays != nil {
		cfg.MaxAgeDays = *o.MaxAgeDays
	}
	if o.Compress != nil {
		cfg.Compress = *o.Compress
	}
}

// apply preserves every omitted nested Plugin value from DefaultConfig.
func (o pluginsOverride) apply(cfg *PluginsConfig) {
	if o.Directory != nil {
		cfg.Directory = *o.Directory
	}
	if o.StartupTimeoutSeconds != nil {
		cfg.StartupTimeoutSeconds = *o.StartupTimeoutSeconds
	}
	if o.RequestTimeoutSeconds != nil {
		cfg.RequestTimeoutSeconds = *o.RequestTimeoutSeconds
	}
	if o.Instances != nil {
		cfg.Instances = clonePluginInstanceConfigs(*o.Instances)
	}
}

func clonePluginInstanceConfigs(instances []PluginInstanceConfig) []PluginInstanceConfig {
	cloned := make([]PluginInstanceConfig, len(instances))
	copy(cloned, instances)
	for index := range cloned {
		cloned[index].Config = append(json.RawMessage(nil), instances[index].Config...)
	}
	return cloned
}

// ensureJSONEOF rejects trailing JSON values after the configuration object.
func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("config file must contain exactly one JSON object")
}
