package etcd2cfg

import (
	"log/slog"
	"time"
)

type Option func(cfg *config)

// WithTagName sets a tag name for etcd values.
func WithTagName(name string) Option {
	return func(cfg *config) {
		cfg.tagName = name
	}
}

// WithLogger sets a logger for the library.
func WithLogger(logger *slog.Logger) Option {
	return func(cfg *config) {
		cfg.logger = logger
	}
}

// WithClientTimeout sets a timeout for etcd client requests.
func WithClientTimeout(timeout time.Duration) Option {
	return func(cfg *config) {
		cfg.clientTimeout = timeout
	}
}

// DisableCache disables caching of etcd requests.
func DisableCache() Option {
	return func(cfg *config) {
		cfg.disableCache = true
	}
}

// WithRunInterval sets a run interval for Sync pattern.
func WithRunInterval(interval time.Duration) Option {
	return func(cfg *config) {
		cfg.runInterval = interval
	}
}

// WithCallback adds a callback function to be called when a value is updated.
func WithCallback(callback CallbackFn) Option {
	return func(cfg *config) {
		cfg.callbacks = append(cfg.callbacks, callback)
	}
}
