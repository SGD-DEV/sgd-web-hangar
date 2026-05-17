package config

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketApp            = []byte("app")
	bucketServices       = []byte("services")
	bucketPHP            = []byte("php")
	bucketProjects       = []byte("projects")
	bucketSSL            = []byte("ssl")
	bucketCustomPackages = []byte("custom_packages")

	keyAppConfig = []byte("config")
)

type AppConfig struct {
	ProjectsRoot   string            `json:"projects_root"`
	ActivePHP      string            `json:"active_php"`
	ActiveNode     string            `json:"active_node"`
	PHPPerProject  map[string]string `json:"php_per_project"`
	ActiveVersions map[string]string `json:"active_versions"`
	ApachePort     int               `json:"apache_port"`
	NginxPort      int               `json:"nginx_port"`
	MySQLPort      int               `json:"mysql_port"`
	PostgreSQLPort int               `json:"postgresql_port"`
	MCPPort        int               `json:"mcp_port"`
	CLIPort        int               `json:"cli_port"`
	DNSPort        int               `json:"dns_port"`
	AutoStartAll   bool              `json:"auto_start_all"`
	ApacheEnabled  bool              `json:"apache_enabled"`
	NginxEnabled   bool              `json:"nginx_enabled"`
	SSLEnabled     bool              `json:"ssl_enabled"`
	Theme          string            `json:"theme"`
}

type ServiceConfig struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	InstallPath string `json:"install_path"`
	Port        int    `json:"port"`
	DataDir     string `json:"data_dir"`
	Enabled     bool   `json:"enabled"`
}

type Store struct {
	db *bolt.DB
}

func NewStore(dbPath string) (*Store, error) {
	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		return nil, fmt.Errorf("config: opening database: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketApp, bucketServices, bucketPHP, bucketProjects, bucketSSL, bucketCustomPackages} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return fmt.Errorf("config: creating bucket %s: %w", string(b), err)
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, err
	}

	s := &Store{db: db}

	if _, err := s.GetAppConfig(); err != nil {
		defaults := DefaultAppConfig()
		if err := s.SaveAppConfig(defaults); err != nil {
			db.Close()
			return nil, fmt.Errorf("config: saving defaults: %w", err)
		}
	}

	return s, nil
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) GetAppConfig() (AppConfig, error) {
	var cfg AppConfig
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketApp)
		data := b.Get(keyAppConfig)
		if data == nil {
			return fmt.Errorf("config: no app config found")
		}
		return json.Unmarshal(data, &cfg)
	})
	return cfg, err
}

func (s *Store) SaveAppConfig(cfg AppConfig) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketApp)
		data, err := json.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("config: marshaling app config: %w", err)
		}
		return b.Put(keyAppConfig, data)
	})
}

func (s *Store) GetServiceConfig(name string) (ServiceConfig, error) {
	var cfg ServiceConfig
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketServices)
		data := b.Get([]byte(name))
		if data == nil {
			return fmt.Errorf("config: service %q not found", name)
		}
		return json.Unmarshal(data, &cfg)
	})
	return cfg, err
}

func (s *Store) SaveServiceConfig(name string, cfg ServiceConfig) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketServices)
		data, err := json.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("config: marshaling service config %q: %w", name, err)
		}
		return b.Put([]byte(name), data)
	})
}

func (s *Store) GetAllServiceConfigs() (map[string]ServiceConfig, error) {
	result := make(map[string]ServiceConfig)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketServices)
		return b.ForEach(func(k, v []byte) error {
			var cfg ServiceConfig
			if err := json.Unmarshal(v, &cfg); err != nil {
				return err
			}
			result[string(k)] = cfg
			return nil
		})
	})
	return result, err
}

func (s *Store) SetPHPVersion(key, version string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPHP)
		return b.Put([]byte(key), []byte(version))
	})
}

func (s *Store) GetPHPVersion(key string) (string, error) {
	var version string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPHP)
		data := b.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("config: php version for %q not found", key)
		}
		version = string(data)
		return nil
	})
	return version, err
}

func (s *Store) SaveProject(name string, data []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketProjects)
		return b.Put([]byte(name), data)
	})
}

func (s *Store) GetProject(name string) ([]byte, error) {
	var data []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketProjects)
		v := b.Get([]byte(name))
		if v == nil {
			return fmt.Errorf("config: project %q not found", name)
		}
		data = make([]byte, len(v))
		copy(data, v)
		return nil
	})
	return data, err
}

func (s *Store) DeleteProject(name string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketProjects)
		return b.Delete([]byte(name))
	})
}

func (s *Store) GetAllProjects() (map[string][]byte, error) {
	result := make(map[string][]byte)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketProjects)
		return b.ForEach(func(k, v []byte) error {
			data := make([]byte, len(v))
			copy(data, v)
			result[string(k)] = data
			return nil
		})
	})
	return result, err
}

func (s *Store) SaveSSLCert(domain string, data []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSSL)
		return b.Put([]byte(domain), data)
	})
}

func (s *Store) GetSSLCert(domain string) ([]byte, error) {
	var data []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSSL)
		v := b.Get([]byte(domain))
		if v == nil {
			return fmt.Errorf("config: ssl cert for %q not found", domain)
		}
		data = make([]byte, len(v))
		copy(data, v)
		return nil
	})
	return data, err
}

func (s *Store) GetAllSSLCerts() (map[string][]byte, error) {
	result := make(map[string][]byte)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSSL)
		return b.ForEach(func(k, v []byte) error {
			data := make([]byte, len(v))
			copy(data, v)
			result[string(k)] = data
			return nil
		})
	})
	return result, err
}

// --- Custom packages (user-added entries) ---
//
// These live alongside the built-in registry so a user can add a new PHP
// release the moment it ships, without waiting for an app update. Stored
// JSON-encoded under bucketCustomPackages, keyed by "<name>-<version>" to
// match the convention used elsewhere.

func (s *Store) SaveCustomPackage(key string, data []byte) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketCustomPackages).Put([]byte(key), data)
	})
}

func (s *Store) DeleteCustomPackage(key string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketCustomPackages).Delete([]byte(key))
	})
}

func (s *Store) GetAllCustomPackages() (map[string][]byte, error) {
	result := make(map[string][]byte)
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketCustomPackages)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			data := make([]byte, len(v))
			copy(data, v)
			result[string(k)] = data
			return nil
		})
	})
	return result, err
}
