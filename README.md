# etcd2cfg

Configuration binding to etcd data.

Features:

* Logging with `slog` compatible logger (debug+errors)
* Sync mode, which updates configuration periodically
* Callbacks when values are updated
* Supports strings, integers, floats, booleans, string slices and time.Duration

## Install

```bash
~ $ go get github.com/hasansino/etcd2cfg
```

## Examples

### Basic

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hasansino/etcd2cfg"
	etcdClient "go.etcd.io/etcd/client/v3"
)

type Config struct {
	// Bind etcd values to struct fields using tags
	ServerName     string        `etcd:"app/server/name"`
	Port           int           `etcd:"app/server/port"`
	Debug          bool          `etcd:"app/server/debug"`
	RequestTimeout time.Duration `etcd:"app/server/timeout"`
	AllowedOrigins []string      `etcd:"app/server/allowed_origins"` // Comma-separated values
}

func main() {
	// Connect to etcd
	client, err := etcdClient.New(etcdClient.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer client.Close()

	// Create configuration struct
	cfg := &Config{
		// Default values if not found in etcd
		ServerName:     "default-server",
		Port:           8080,
		Debug:          false,
		RequestTimeout: 30 * time.Second,
	}

	// Bind etcd values to the struct
	err = etcd2cfg.Bind(cfg, client)
	if err != nil {
		log.Fatalf("Failed to bind config: %v", err)
	}

	// Use the configuration
	fmt.Printf("Server: %s\n", cfg.ServerName)
	fmt.Printf("Port: %d\n", cfg.Port)
	fmt.Printf("Debug: %t\n", cfg.Debug)
	fmt.Printf("Timeout: %s\n", cfg.RequestTimeout)
	fmt.Printf("Allowed Origins: %v\n", cfg.AllowedOrigins)
}

```

### Auto-Updating Configuration

```go
package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hasansino/etcd2cfg"
	etcdClient "go.etcd.io/etcd/client/v3"
)

// AppConfig implements sync.Locker for thread-safe updates
type AppConfig struct {
	sync.Mutex
	ServerName     string        `etcd:"app/server/name"`
	Port           int           `etcd:"app/server/port"`
	Debug          bool          `etcd:"app/server/debug"`
	RequestTimeout time.Duration `etcd:"app/server/timeout"`
}

func main() {
	// Connect to etcd
	client, err := etcdClient.New(etcdClient.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer client.Close()

	// Create configuration struct
	cfg := &AppConfig{
		ServerName:     "default-server",
		Port:           8080,
		Debug:          false,
		RequestTimeout: 30 * time.Second,
	}

	// Create context for the auto-update process
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start auto-updating in background
	go func() {
		err := etcd2cfg.Sync(
			ctx,
			cfg, // Must implement sync.Locker
			client,
			etcd2cfg.WithRunInterval(1*time.Minute), // Check for updates every minute
			etcd2cfg.WithLogger(nil),                // Use custom logger
		)
		if err != nil {
			log.Fatalf("Auto-update failed: %v", err)
		}
	}()

	// Application logic
	for {
		// Access config safely
		cfg.Lock()
		fmt.Printf("Server: %s, Port: %d, Debug: %t, Timeout: %s\n",
			cfg.ServerName, cfg.Port, cfg.Debug, cfg.RequestTimeout)
		cfg.Unlock()

		time.Sleep(10 * time.Second)
	}
}
```

### Using Callbacks for Configuration Changes

```go
package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hasansino/etcd2cfg"
	etcdClient "go.etcd.io/etcd/client/v3"
)

type DatabaseConfig struct {
	sync.Mutex
	Host     string `etcd:"app/db/host"`
	Port     int    `etcd:"app/db/port"`
	Username string `etcd:"app/db/username"`
	Password string `etcd:"app/db/password"`
	MaxConns int    `etcd:"app/db/max_connections"`
}

func main() {
	// Connect to etcd
	client, err := etcdClient.New(etcdClient.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer client.Close()

	// Create configuration struct
	dbConfig := &DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		Username: "postgres",
		Password: "postgres",
		MaxConns: 10,
	}

	// Define callback function to handle configuration changes
	configChangeCallback := func(key string, field string, oldValue, newValue interface{}) error {
		fmt.Printf("Config changed: %s (%s) changed from %v to %v\n",
			field, key, oldValue, newValue)

		// Perform actions based on specific field changes
		if field == "MaxConns" {
			fmt.Println("Adjusting connection pool size...")
			// Adjust your connection pool here
		}

		if field == "Host" || field == "Port" {
			fmt.Println("Database connection details changed, reconnecting...")
			// Reconnect to database here
		}

		return nil
	}

	// Create context for the auto-update process
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start auto-updating in background with callback
	go func() {
		err := etcd2cfg.Sync(
			ctx,
			dbConfig,
			client,
			etcd2cfg.WithRunInterval(30*time.Second),
			etcd2cfg.WithCallbacks(configChangeCallback),
			etcd2cfg.WithDisableCache(true), // Always fetch fresh values
		)
		if err != nil {
			log.Fatalf("Auto-update failed: %v", err)
		}
	}()

	// Application logic
	select {}
}
```

### All options

```go
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/hasansino/etcd2cfg"
	etcdClient "go.etcd.io/etcd/client/v3"
)

type CompleteConfig struct {
	sync.Mutex

	// Server settings
	ServerName     string        `etcd:"app/server/name"`
	Port           int           `etcd:"app/server/port"`
	Debug          bool          `etcd:"app/server/debug"`
	RequestTimeout time.Duration `etcd:"app/server/timeout"`

	// Database settings
	DBHost     string `etcd:"app/db/host"`
	DBPort     int    `etcd:"app/db/port"`
	DBUsername string `etcd:"app/db/username"`
	DBPassword string `etcd:"app/db/password"`
	DBMaxConns int    `etcd:"app/db/max_connections"`

	// Feature flags
	EnableNewFeature bool `etcd:"app/features/new_feature"`
	EnableBetaAccess bool `etcd:"app/features/beta_access"`

	// Limits
	RateLimit      int           `etcd:"app/limits/rate"`
	SessionTimeout time.Duration `etcd:"app/limits/session"`

	// Custom settings with different tag name
	CustomSetting string `mycustomtag:"app/custom/setting"`
}

func main() {
	// Connect to etcd
	client, err := etcdClient.New(etcdClient.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to connect to etcd: %v", err)
	}
	defer client.Close()

	// Create configuration struct with defaults
	cfg := &CompleteConfig{
		ServerName:     "api-server",
		Port:           8080,
		Debug:          false,
		RequestTimeout: 30 * time.Second,
		DBHost:         "localhost",
		DBPort:         5432,
		DBUsername:     "postgres",
		DBPassword:     "postgres",
		DBMaxConns:     10,
		RateLimit:      100,
		SessionTimeout: 24 * time.Hour,
	}

	// Setup logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	// Define multiple callbacks
	serverConfigCallback := func(key string, field string, oldValue, newValue interface{}) error {
		logger.Info("Server config changed",
			"field", field,
			"old", oldValue,
			"new", newValue)
		return nil
	}

	dbConfigCallback := func(key string, field string, oldValue, newValue interface{}) error {
		logger.Info("Database config changed",
			"field", field,
			"old", oldValue,
			"new", newValue)
		return nil
	}

	// Create context for the auto-update process
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start auto-updating in background with all options
	go func() {
		err := etcd2cfg.Sync(
			ctx,
			cfg,
			client,
			etcd2cfg.WithRunInterval(1*time.Minute),
			etcd2cfg.WithLogger(logger),
			etcd2cfg.WithCallbacks(serverConfigCallback, dbConfigCallback),
			etcd2cfg.WithDisableCache(false),
			etcd2cfg.WithTagName("mycustomtag"), // Use custom tag for some fields
			etcd2cfg.WithClientTimeout(3*time.Second),
		)
		if err != nil {
			log.Fatalf("Auto-update failed: %v", err)
		}
	}()

	// One-time binding with all options
	err = etcd2cfg.Bind(
		cfg,
		client,
		etcd2cfg.WithLogger(logger),
		etcd2cfg.WithCallbacks(serverConfigCallback),
		etcd2cfg.WithDisableCache(true),
		etcd2cfg.WithTagName("etcd"),
		etcd2cfg.WithClientTimeout(3*time.Second),
	)
	if err != nil {
		log.Fatalf("Failed to bind config: %v", err)
	}

	// Application logic
	fmt.Println("Application running with configuration:")
	fmt.Printf("Server: %s:%d (debug: %t)\n", cfg.ServerName, cfg.Port, cfg.Debug)
	fmt.Printf("Database: %s@%s:%d\n", cfg.DBUsername, cfg.DBHost, cfg.DBPort)
	fmt.Printf("Features: NewFeature=%t, BetaAccess=%t\n", cfg.EnableNewFeature, cfg.EnableBetaAccess)

	select {}
}

```