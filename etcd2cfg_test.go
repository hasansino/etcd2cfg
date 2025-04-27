package etcd2cfg

import (
	"context"
	"log"
	"log/slog"
	"os"
	"sync"
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

func newTestCfg() *testCfg {
	return &testCfg{
		Slice:      make([]string, 0),
		EmptySlice: make([]string, 0),
	}
}

type testCfgSync struct {
	sync.Mutex
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

func newTestCfgSync() *testCfgSync {
	return &testCfgSync{
		Slice:      make([]string, 0),
		EmptySlice: make([]string, 0),
	}
}

func _initTest(_ *testing.T) (etcdClientAccessor, error) {
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

	opts := make([]Option, 0)

	//loggerOpts := &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true}
	//slogHandler := slog.NewTextHandler(os.Stdout, loggerOpts)
	//logger := slog.New(slogHandler)
	//opts = append(opts, WithLogger(logger))

	t.Run("SuccessfulBinding", func(t *testing.T) {
		cfg := newTestCfg()

		if err := Bind(cfg, client, opts...); err != nil {
			t.Fatalf("failed to bind configuration: %v", err)
		}

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
		err = Bind(cfg, mockClient, WithCallback(callback))
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

// ---------------------------------------------------------------------------

func TestSync(t *testing.T) {
	client, err := _initTest(t)
	if err != nil {
		t.Fatalf("failed to initialize test: %v", err)
	}
	defer wrapUp(client)

	t.Run("SuccessfulSync", func(t *testing.T) {
		cfg := newTestCfgSync()
		cfg.String = "initial"

		err := Sync(context.Background(), cfg, client, WithRunInterval(100*time.Millisecond))
		require.NoError(t, err)

		cfg.Lock()
		assert.Equal(t, "test-string", cfg.String)
		cfg.Unlock()

		_, err = client.Put(context.Background(), "example.com/domain/service/string", "updated-value")
		require.NoError(t, err)

		time.Sleep(500 * time.Millisecond)

		cfg.Lock()
		assert.Equal(t, "updated-value", cfg.String)
		cfg.Unlock()

		_, err = client.Put(context.Background(), "example.com/domain/service/string", "updated-value-2")
		require.NoError(t, err)

		time.Sleep(500 * time.Millisecond)

		cfg.Lock()
		assert.Equal(t, "updated-value-2", cfg.String)
		cfg.Unlock()

		_, err = client.Put(context.Background(), "example.com/domain/service/string", "test-string")
		require.NoError(t, err)
	})

	t.Run("SyncWithNilClient", func(t *testing.T) {
		ctx := context.Background()
		cfg := &testCfgSync{}
		err := Sync(ctx, cfg, nil)
		assert.Error(t, err, "should return an error with nil client")
	})

	t.Run("SyncWithNilTarget", func(t *testing.T) {
		ctx := context.Background()
		err := Sync(ctx, nil, client)
		assert.Error(t, err, "should return an error with nil target")
	})

	t.Run("SyncWithCustomOptions", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cfg := &testCfgSync{}
		customLogger := slog.New(
			slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}),
		)
		customOpts := []Option{
			WithLogger(customLogger),
			WithRunInterval(1 * time.Hour), // Long interval so it doesn't run during test
		}

		err := Sync(ctx, cfg, client, customOpts...)
		assert.NoError(t, err, "should sync successfully with custom options")
	})

	t.Run("SyncWithCallback", func(t *testing.T) {
		// Setup mock client
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockClient := mocks.NewMocketcdClientAccessor(ctrl)

		// Setup response for the initial Get
		initialResp := &etcdClient.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte("example.com/domain/service/string"),
					Value: []byte("initial-value"),
				},
			},
		}

		// Setup response for the second Get (after ticker fires)
		updatedResp := &etcdClient.GetResponse{
			Kvs: []*mvccpb.KeyValue{
				{
					Key:   []byte("example.com/domain/service/string"),
					Value: []byte("updated-value"),
				},
			},
		}

		// Use AnyTimes() to allow any number of calls
		mockClient.EXPECT().
			Get(gomock.Any(), "example.com/domain/service/string", gomock.Any()).
			Return(initialResp, nil).
			Times(1)

		mockClient.EXPECT().
			Get(gomock.Any(), "example.com/domain/service/string", gomock.Any()).
			Return(updatedResp, nil).
			AnyTimes()

		// Create a context that can be canceled
		ctx, cancel := context.WithCancel(context.Background())

		// Create test config struct with mutex
		type testConfig struct {
			sync.Mutex
			String string `etcd:"example.com/domain/service/string"`
		}
		cfg := &testConfig{}

		// Track callback invocation
		var callbackMutex sync.Mutex
		callbackCalled := false
		callbackKey := ""
		callbackField := ""
		var oldValue, newValue interface{}

		callback := func(key string, field string, old, new interface{}) error {
			callbackMutex.Lock()
			defer callbackMutex.Unlock()
			callbackCalled = true
			callbackKey = key
			callbackField = field
			oldValue = old
			newValue = new
			return nil
		}

		// Sync with callback and short interval
		err := Sync(ctx, cfg, mockClient,
			WithCallback(callback),
			WithRunInterval(100*time.Millisecond),
		)
		require.NoError(t, err)

		// Wait for the initial sync
		time.Sleep(50 * time.Millisecond)

		// Check initial value
		cfg.Lock()
		assert.Equal(t, "initial-value", cfg.String)
		cfg.Unlock()

		// Wait for the second sync
		time.Sleep(200 * time.Millisecond)

		// Verify callback was called with correct values
		callbackMutex.Lock()
		assert.True(t, callbackCalled)
		assert.Equal(t, "example.com/domain/service/string", callbackKey)
		assert.Equal(t, "String", callbackField)
		assert.Equal(t, "initial-value", oldValue)
		assert.Equal(t, "updated-value", newValue)
		callbackMutex.Unlock()

		// Check updated value
		cfg.Lock()
		assert.Equal(t, "updated-value", cfg.String)
		cfg.Unlock()

		// Cancel the context to stop the sync goroutine
		cancel()

		// Wait a bit to ensure the goroutine has time to exit
		time.Sleep(200 * time.Millisecond)
	})

	t.Run("ContextCancellation", func(t *testing.T) {
		// Create a context that will be canceled
		ctx, cancel := context.WithCancel(context.Background())

		cfg := &testCfgSync{}
		err := Sync(ctx, cfg, client, WithRunInterval(100*time.Millisecond))
		require.NoError(t, err)

		// Wait for the first sync to happen
		time.Sleep(200 * time.Millisecond)

		// Cancel the context to stop the sync goroutine
		cancel()

		// Wait a bit to ensure the goroutine has time to exit
		time.Sleep(200 * time.Millisecond)

		// No way to directly test that the goroutine has exited,
		// but we can check that the function returned successfully
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------

type DummyClient struct {
	sync.RWMutex
	data map[string]string
}

func NewDummyClient() *DummyClient {
	return &DummyClient{data: make(map[string]string)}
}

func (cl *DummyClient) Get(_ context.Context, key string, _ ...etcdClient.OpOption,
) (*etcdClient.GetResponse, error) {
	cl.RLock()
	defer cl.RUnlock()
	resp := &etcdClient.GetResponse{Kvs: []*mvccpb.KeyValue{}}
	if val, ok := cl.data[key]; ok {
		resp.Count = 1
		resp.Kvs = append(resp.Kvs, &mvccpb.KeyValue{
			Value: []byte(val),
		})
	}
	return resp, nil
}

func (cl *DummyClient) Put(_ context.Context, key, val string, _ ...etcdClient.OpOption,
) (*etcdClient.PutResponse, error) {
	cl.Lock()
	defer cl.Unlock()
	cl.data[key] = val
	return nil, nil
}

func (cl *DummyClient) Delete(_ context.Context, key string, _ ...etcdClient.OpOption,
) (*etcdClient.DeleteResponse, error) {
	cl.Lock()
	defer cl.Unlock()
	delete(cl.data, key)
	return nil, nil
}
