package etcd2cfg

import (
	"context"
	"log"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/mvccpb"
	etcdClient "go.etcd.io/etcd/client/v3"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/hasansino/etcd2cfg/mocks"
)

const (
	runIntegrationTests = false
	etcdReqTimeout      = 5 * time.Second
)

var etcdTestData = map[string]string{
	// Basic types
	"example.com/domain/service/string":     "test-string",
	"example.com/domain/service/bool_true":  "true",
	"example.com/domain/service/bool_false": "false",
	"example.com/domain/service/int":        "42",
	"example.com/domain/service/int8":       "8",
	"example.com/domain/service/int16":      "16",
	"example.com/domain/service/int32":      "32",
	"example.com/domain/service/int64":      "64",
	"example.com/domain/service/uint":       "42",
	"example.com/domain/service/uint8":      "8",
	"example.com/domain/service/uint16":     "16",
	"example.com/domain/service/uint32":     "32",
	"example.com/domain/service/uint64":     "64",
	"example.com/domain/service/float32":    "3.14",
	"example.com/domain/service/float64":    "6.28",
	"example.com/domain/service/slice":      "one,two,three",
	"example.com/domain/service/duration":   "5s",
	"example.com/domain/service/duration2":  "1h30m",

	// Corner cases
	"example.com/domain/service/empty_string":  "",
	"example.com/domain/service/zero_int":      "0",
	"example.com/domain/service/empty_slice":   "",
	"example.com/domain/service/max_int64":     "9223372036854775807",
	"example.com/domain/service/min_int64":     "-9223372036854775808",
	"example.com/domain/service/zero_duration": "0s",

	// Invalid values (for error testing)
	"example.com/domain/service/invalid_bool":     "not-a-bool",
	"example.com/domain/service/invalid_int":      "not-an-int",
	"example.com/domain/service/invalid_float":    "not-a-float",
	"example.com/domain/service/invalid_duration": "not-a-duration",
	"example.com/domain/service/overflow_int8":    "1000", // Too large for int8
}

type testCfg struct {
	String    string        `etcd:"example.com/domain/service/string"`
	BoolTrue  bool          `etcd:"example.com/domain/service/bool_true"`
	BoolFalse bool          `etcd:"example.com/domain/service/bool_false"`
	Int       int           `etcd:"example.com/domain/service/int"`
	Int8      int8          `etcd:"example.com/domain/service/int8"`
	Int16     int16         `etcd:"example.com/domain/service/int16"`
	Int32     int32         `etcd:"example.com/domain/service/int32"`
	Int64     int64         `etcd:"example.com/domain/service/int64"`
	Uint      uint          `etcd:"example.com/domain/service/uint"`
	Uint8     uint8         `etcd:"example.com/domain/service/uint8"`
	Uint16    uint16        `etcd:"example.com/domain/service/uint16"`
	Uint32    uint32        `etcd:"example.com/domain/service/uint32"`
	Uint64    uint64        `etcd:"example.com/domain/service/uint64"`
	Float32   float32       `etcd:"example.com/domain/service/float32"`
	Float64   float64       `etcd:"example.com/domain/service/float64"`
	Slice     []string      `etcd:"example.com/domain/service/slice"`
	Duration  time.Duration `etcd:"example.com/domain/service/duration"`
	Duration2 time.Duration `etcd:"example.com/domain/service/duration2"`

	EmptyString  string        `etcd:"example.com/domain/service/empty_string"`
	ZeroInt      int           `etcd:"example.com/domain/service/zero_int"`
	EmptySlice   []string      `etcd:"example.com/domain/service/empty_slice"`
	MaxInt64     int64         `etcd:"example.com/domain/service/max_int64"`
	MinInt64     int64         `etcd:"example.com/domain/service/min_int64"`
	ZeroDuration time.Duration `etcd:"example.com/domain/service/zero_duration"`
}

// Config for missing key testing
type missingKeyCfg struct {
	Missing string `etcd:"example.com/domain/service/non_existent_key"`
}

func _initTest(t *testing.T) (etcdClientAccessor, error) {
	var client etcdClientAccessor
	if runIntegrationTests {
		config := etcdClient.Config{
			Endpoints:   []string{"localhost:2379"}, // Default etcd endpoint
			DialTimeout: etcdReqTimeout,
			Logger:      zap.NewNop(),
		}
		var err error
		client, err = etcdClient.New(config)
		if err != nil {
			log.Fatalf("failed to create etcd client: %v", err)
		}
	} else {
		client = NewDummyClient()
	}

	for k, v := range etcdTestData {
		err := func() error {
			ctx, cancel := context.WithTimeout(context.Background(), etcdReqTimeout)
			defer cancel()
			if _, err := client.Put(ctx, k, v); err != nil {
				return err
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
	}

	return client, nil
}

func wrapUp(client etcdClientAccessor) {
	for k := range etcdTestData {
		_ = func() error {
			ctx, cancel := context.WithTimeout(context.Background(), etcdReqTimeout)
			defer cancel()
			if _, err := client.Delete(ctx, k); err != nil {
				return err
			}
			return nil
		}()
	}
}

func TestBind(t *testing.T) {
	client, err := _initTest(t)
	if err != nil {
		t.Fatalf("failed to initialize test: %v", err)
	}
	defer wrapUp(client)

	loggerOpts := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	slogHandler := slog.NewTextHandler(os.Stdout, loggerOpts)
	logger := slog.New(slogHandler)
	opts := []Option{WithLogger(logger)}
	opts = nil // enable/disable logging

	t.Run("SuccessfulBinding", func(t *testing.T) {
		cfg := new(testCfg)
		if err := Bind(cfg, client, opts...); err != nil {
			t.Fatalf("failed to bind configuration: %v", err)
		}

		// Table-driven test cases
		testCases := []struct {
			name     string
			actual   interface{}
			expected interface{}
		}{
			// Basic types
			{"String", cfg.String, "test-string"},
			{"BoolTrue", cfg.BoolTrue, true},
			{"BoolFalse", cfg.BoolFalse, false},
			{"Int", cfg.Int, 42},
			{"Int8", cfg.Int8, int8(8)},
			{"Int16", cfg.Int16, int16(16)},
			{"Int32", cfg.Int32, int32(32)},
			{"Int64", cfg.Int64, int64(64)},
			{"Uint", cfg.Uint, uint(42)},
			{"Uint8", cfg.Uint8, uint8(8)},
			{"Uint16", cfg.Uint16, uint16(16)},
			{"Uint32", cfg.Uint32, uint32(32)},
			{"Uint64", cfg.Uint64, uint64(64)},
			{"Float32", cfg.Float32, float32(3.14)},
			{"Float64", cfg.Float64, 6.28},
			{"Slice", cfg.Slice, []string{"one", "two", "three"}},
			{"Duration", cfg.Duration, 5 * time.Second},
			{"Duration2", cfg.Duration2, 90 * time.Minute},

			// Corner cases
			{"EmptyString", cfg.EmptyString, ""},
			{"ZeroInt", cfg.ZeroInt, 0},
			{"EmptySlice", cfg.EmptySlice, []string{}},
			{"MaxInt64", cfg.MaxInt64, int64(9223372036854775807)},
			{"MinInt64", cfg.MinInt64, int64(-9223372036854775808)},
			{"ZeroDuration", cfg.ZeroDuration, time.Duration(0)},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, tc.actual, "values should be equal")
			})
		}
	})

	t.Run("ErrorHandling", func(t *testing.T) {
		errorCases := []struct {
			name        string
			cfg         interface{}
			expectedErr bool
		}{
			{"MissingKey", &missingKeyCfg{}, true},
			{"NilConfig", nil, true},
			{"NonPointerConfig", testCfg{}, true},
		}

		for _, tc := range errorCases {
			t.Run(tc.name, func(t *testing.T) {
				err := Bind(tc.cfg, client, opts...)
				if tc.expectedErr {
					assert.Error(t, err, "should return an error")
				} else {
					assert.NoError(t, err, "should not return an error")
				}
			})
		}
	})

	t.Run("NilClient", func(t *testing.T) {
		cfg := new(testCfg)
		err := Bind(cfg, nil, opts...)
		assert.Error(t, err, "should return an error with nil client")
	})

	t.Run("CustomOptions", func(t *testing.T) {
		cfg := new(testCfg)
		// Test with a custom logger
		customLogger := slog.New(
			slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}),
		)
		customOpts := []Option{WithLogger(customLogger)}

		err := Bind(cfg, client, customOpts...)
		assert.NoError(t, err, "should bind successfully with custom options")
	})

	t.Run("Callback is triggered when value changes", func(t *testing.T) {
		// Setup mock client
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMocketcdClientAccessor(ctrl)

		// Setup initial response
		initialResp := &etcdClient.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte("example.com/domain/service/string"),
					Value: []byte("initial"),
				},
			},
		}

		// Setup updated response for second call
		updatedResp := &etcdClient.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte("example.com/domain/service/string"),
					Value: []byte("updated"),
				},
			},
		}

		// Configure mock to return different values on consecutive calls
		mockClient.EXPECT().
			Get(gomock.Any(), "example.com/domain/service/string", gomock.Any()).
			Return(initialResp, nil)

		mockClient.EXPECT().
			Get(gomock.Any(), "example.com/domain/service/string", gomock.Any()).
			Return(updatedResp, nil)

		// Create test config struct
		type testConfig struct {
			String string `etcd:"example.com/domain/service/string"`
		}
		cfg := &testConfig{}

		// Track callback invocation
		callbackCalled := false
		callbackKey := ""
		callbackField := ""
		var oldValue, newValue interface{}

		callback := func(key string, field string, old, new interface{}) error {
			callbackCalled = true
			callbackKey = key
			callbackField = field
			oldValue = old
			newValue = new
			return nil
		}

		// First bind to set initial value
		err := Bind(cfg, mockClient, WithCallback(callback))
		require.NoError(t, err)
		assert.Equal(t, "initial", cfg.String)

		// Second bind to trigger update and callback
		err = Bind(cfg, mockClient, WithCallback(callback), DisableCache())
		require.NoError(t, err)

		// Verify callback was called with correct values
		assert.True(t, callbackCalled)
		assert.Equal(t, "example.com/domain/service/string", callbackKey)
		assert.Equal(t, "String", callbackField)
		assert.Equal(t, "initial", oldValue)
		assert.Equal(t, "updated", newValue)
		assert.Equal(t, "updated", cfg.String)
	})
}

type DummyClient struct {
	data map[string]string
}

func NewDummyClient() *DummyClient {
	return &DummyClient{data: make(map[string]string)}
}

func (cl *DummyClient) Get(
	_ context.Context, key string, _ ...etcdClient.OpOption,
) (*etcdClient.GetResponse, error) {
	resp := &etcdClient.GetResponse{
		Kvs: []*mvccpb.KeyValue{},
	}
	if val, ok := cl.data[key]; ok {
		resp.Kvs = append(resp.Kvs, &mvccpb.KeyValue{
			Value: []byte(val),
		})

	}
	return resp, nil
}

func (cl *DummyClient) Put(
	_ context.Context, key, val string, _ ...etcdClient.OpOption,
) (*etcdClient.PutResponse, error) {
	cl.data[key] = val
	return nil, nil
}

func (cl *DummyClient) Delete(
	_ context.Context, key string, _ ...etcdClient.OpOption,
) (*etcdClient.DeleteResponse, error) {
	delete(cl.data, key)
	return nil, nil
}
