package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultHubID                 = "public"
	DefaultListenAddr            = ":8080"
	DefaultDatabasePath          = "/data/hub.db"
	DefaultRegistrationTTL       = 24 * time.Hour
	DefaultPeerLease             = 300 * time.Second
	DefaultMaxRegisteredAgents   = 100
	DefaultMaxTasksPerMinute     = 60
	DefaultMaxConcurrentTasks    = 4
	DefaultMaxPayloadBytes       = 1 << 20
	DefaultMaxGroupMembers       = 32
	DefaultMaxGroupFanout        = 32
	DefaultMaxGroupHistoryPage   = 100
	DefaultRegistrationPerMinute = 10
)

type Config struct {
	HubID                 string
	ListenAddr            string
	DatabasePath          string
	PublicBaseURL         string
	RegistrationEnabled   bool
	RegistrationTTL       time.Duration
	PeerLease             time.Duration
	MaxRegisteredAgents   int
	MaxTasksPerMinute     int
	MaxConcurrentTasks    int
	MaxPayloadBytes       int64
	MaxGroupMembers       int
	MaxGroupFanout        int
	MaxGroupHistoryPage   int
	RegistrationPerMinute int
	OperatorToken         string
}

func Load() (Config, error) {
	config := Config{
		HubID:                 valueOr("A2A888_HUB_ID", DefaultHubID),
		ListenAddr:            valueOr("A2A888_HUB_LISTEN_ADDR", DefaultListenAddr),
		DatabasePath:          valueOr("A2A888_HUB_DB_PATH", DefaultDatabasePath),
		PublicBaseURL:         strings.TrimRight(os.Getenv("A2A888_HUB_PUBLIC_URL"), "/"),
		RegistrationEnabled:   envBool("A2A888_HUB_REGISTRATION_ENABLED", true),
		RegistrationTTL:       DefaultRegistrationTTL,
		PeerLease:             DefaultPeerLease,
		MaxRegisteredAgents:   DefaultMaxRegisteredAgents,
		MaxTasksPerMinute:     DefaultMaxTasksPerMinute,
		MaxConcurrentTasks:    DefaultMaxConcurrentTasks,
		MaxPayloadBytes:       DefaultMaxPayloadBytes,
		MaxGroupMembers:       DefaultMaxGroupMembers,
		MaxGroupFanout:        DefaultMaxGroupFanout,
		MaxGroupHistoryPage:   DefaultMaxGroupHistoryPage,
		RegistrationPerMinute: DefaultRegistrationPerMinute,
		OperatorToken:         os.Getenv("A2A888_HUB_OPERATOR_TOKEN"),
	}
	var err error
	if config.RegistrationTTL, err = envDurationSeconds("A2A888_HUB_REGISTRATION_TTL_SECONDS", config.RegistrationTTL); err != nil {
		return Config{}, err
	}
	if config.PeerLease, err = envDurationSeconds("A2A888_HUB_PEER_LEASE_SECONDS", config.PeerLease); err != nil {
		return Config{}, err
	}
	if config.MaxRegisteredAgents, err = envInt("A2A888_HUB_MAX_REGISTERED_AGENTS", config.MaxRegisteredAgents); err != nil {
		return Config{}, err
	}
	if config.MaxTasksPerMinute, err = envInt("A2A888_HUB_MAX_TASKS_PER_MINUTE", config.MaxTasksPerMinute); err != nil {
		return Config{}, err
	}
	if config.MaxConcurrentTasks, err = envInt("A2A888_HUB_MAX_CONCURRENT_TASKS", config.MaxConcurrentTasks); err != nil {
		return Config{}, err
	}
	if config.MaxPayloadBytes, err = envInt64("A2A888_HUB_MAX_PAYLOAD_BYTES", config.MaxPayloadBytes); err != nil {
		return Config{}, err
	}
	if config.RegistrationPerMinute, err = envInt("A2A888_HUB_REGISTRATION_PER_MINUTE", config.RegistrationPerMinute); err != nil {
		return Config{}, err
	}
	if config.MaxGroupMembers, err = envInt("A2A888_HUB_MAX_GROUP_MEMBERS", config.MaxGroupMembers); err != nil {
		return Config{}, err
	}
	if config.MaxGroupFanout, err = envInt("A2A888_HUB_MAX_GROUP_FANOUT", config.MaxGroupFanout); err != nil {
		return Config{}, err
	}
	if config.MaxGroupHistoryPage, err = envInt("A2A888_HUB_MAX_GROUP_HISTORY_PAGE", config.MaxGroupHistoryPage); err != nil {
		return Config{}, err
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) Validate() error {
	if strings.TrimSpace(config.HubID) == "" || strings.TrimSpace(config.ListenAddr) == "" || strings.TrimSpace(config.DatabasePath) == "" {
		return fmt.Errorf("hub id, listen address, and database path are required")
	}
	if config.RegistrationTTL <= 0 || config.PeerLease <= 0 || config.PeerLease > config.RegistrationTTL {
		return fmt.Errorf("peer lease and registration ttl are invalid")
	}
	if config.MaxRegisteredAgents <= 0 || config.MaxTasksPerMinute <= 0 || config.MaxConcurrentTasks <= 0 || config.RegistrationPerMinute <= 0 || config.MaxGroupMembers < 0 || config.MaxGroupFanout < 0 || config.MaxGroupHistoryPage < 0 {
		return fmt.Errorf("agent and rate limits must be positive")
	}
	if (config.MaxGroupMembers != 0 && config.MaxGroupMembers > hubMaxGroupMembers()) || (config.MaxGroupFanout != 0 && config.MaxGroupFanout > hubMaxGroupMembers()) || (config.MaxGroupHistoryPage != 0 && config.MaxGroupHistoryPage > hubMaxGroupHistoryPage()) {
		return fmt.Errorf("group limits exceed safe maximums")
	}
	if config.MaxPayloadBytes <= 0 || config.MaxPayloadBytes > 16<<20 {
		return fmt.Errorf("max payload bytes must be between 1 and 16777216")
	}
	return nil
}

func hubMaxGroupMembers() int { return 32 }

func hubMaxGroupHistoryPage() int { return 100 }

func valueOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDurationSeconds(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer number of seconds: %w", name, err)
	}
	return time.Duration(seconds) * time.Second, nil
}

func envInt(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}

func envInt64(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}
