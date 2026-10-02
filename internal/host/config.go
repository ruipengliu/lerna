package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type DatabaseConfig struct {
	Driver         string `json:"driver"`
	URLEnv         string `json:"url_env,omitempty"`
	Path           string `json:"path,omitempty"`
	MaxConnections int    `json:"max_connections"`
}

// Config is a local assembly configuration, not a harness/1 domain object.
type Config struct {
	Role            string         `json:"role"`
	InstanceID      string         `json:"instance_id"`
	AdminAddr       string         `json:"admin_addr"`
	Root            string         `json:"root"`
	Database        DatabaseConfig `json:"database"`
	ContentRoot     string         `json:"content_root"`
	ManagedRoot     string         `json:"managed_root"`
	ProbeTimeout    string         `json:"probe_timeout"`
	ShutdownTimeout string         `json:"shutdown_timeout"`
}

func LoadConfig(path string) (Config, error) {
	var config Config
	file, err := os.Open(path)
	if err != nil {
		return config, fmt.Errorf("open host config: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, fmt.Errorf("decode host config: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return config, errors.New("host config must contain one JSON object")
	}
	if err := config.validate(); err != nil {
		return config, err
	}
	if !filepath.IsAbs(config.Root) {
		config.Root = filepath.Join(filepath.Dir(path), config.Root)
	}
	config.Root, err = filepath.Abs(config.Root)
	if err != nil {
		return config, err
	}
	for _, destination := range []*string{&config.ContentRoot, &config.ManagedRoot, &config.Database.Path} {
		if *destination != "" && !filepath.IsAbs(*destination) {
			*destination = filepath.Join(config.Root, *destination)
		}
	}
	return config, nil
}

func (config Config) validate() error {
	switch config.Role {
	case "gateway", "application", "worker", "executor", "all":
	default:
		return errors.New("unsupported host role")
	}
	if config.InstanceID == "" || config.Root == "" || config.ContentRoot == "" || config.ManagedRoot == "" {
		return errors.New("host identity and explicit data roots are required")
	}
	host, port, err := net.SplitHostPort(config.AdminAddr)
	ip := net.ParseIP(host)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || ip == nil || !ip.IsLoopback() || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("development admin address must use a loopback IP and fixed port")
	}
	if config.Database.MaxConnections < 1 || config.Database.MaxConnections > 16 {
		return errors.New("database max_connections must be between 1 and 16")
	}
	switch config.Database.Driver {
	case "postgres":
		if config.Database.URLEnv == "" || config.Database.Path != "" {
			return errors.New("postgres requires url_env and no file path")
		}
	case "sqlite":
		if config.Role != "all" || config.Database.Path == "" || config.Database.MaxConnections != 1 {
			return errors.New("sqlite is restricted to the single host with one connection")
		}
	default:
		return errors.New("unsupported database driver")
	}
	for _, duration := range []string{config.ProbeTimeout, config.ShutdownTimeout} {
		parsed, err := time.ParseDuration(duration)
		if err != nil || parsed <= 0 || parsed > 30*time.Second {
			return errors.New("host timeouts must be positive and at most 30s")
		}
	}
	return nil
}
