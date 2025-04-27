package etcd2cfg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	etcdClient "go.etcd.io/etcd/client/v3"
)

//go:generate mockgen -source $GOFILE -package mocks -destination mocks/mocks.go

type etcdClientAccessor interface {
	Get(ctx context.Context, key string, opts ...etcdClient.OpOption) (*etcdClient.GetResponse, error)
	Put(ctx context.Context, key, val string, opts ...etcdClient.OpOption) (*etcdClient.PutResponse, error)
	Delete(ctx context.Context, key string, opts ...etcdClient.OpOption) (*etcdClient.DeleteResponse, error)
}

// CallbackFn is called when a value whose field name matching field is updated.
//   - key is etcd value path like it is defined in tag
//   - field is a field name of target object
//   - oldValue is a previous value of field
//   - newValue is a new value of field
type CallbackFn func(key string, field string, oldValue, newValue interface{}) error

type config struct {
	tagName       string
	logger        *slog.Logger
	client        etcdClientAccessor
	clientTimeout time.Duration
	clientCache   map[string]string
	disableCache  bool
	runInterval   time.Duration
	callbacks     []CallbackFn
}

// Sync to a target object and bind its fields to etcd values.
// Runs every 5 minutes, can be changed with WithRunInterval option.
func Sync(
	ctx context.Context,
	target sync.Locker,
	client etcdClientAccessor,
	opts ...Option,
) error {
	libCfg := &config{
		tagName:       "etcd",
		logger:        slog.New(slog.DiscardHandler),
		client:        client,
		clientTimeout: 5 * time.Second,
		clientCache:   make(map[string]string),
		disableCache:  false,
		runInterval:   5 * time.Minute,
		callbacks:     []CallbackFn{},
	}
	for _, opt := range opts {
		opt(libCfg)
	}
	ticker := time.NewTicker(libCfg.runInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			libCfg.logger.Debug("binding -> running")
			target.Lock()
			err := bind(libCfg, target)
			if err != nil {
				libCfg.logger.Error(
					"binding -> failed to bind",
					slog.String("error", err.Error()),
				)
			}
			target.Unlock()
		}
	}
}

// Bind the configuration to the given struct.
func Bind(target interface{}, client etcdClientAccessor, opts ...Option) error {
	libCfg := &config{
		tagName:       "etcd",
		logger:        slog.New(slog.DiscardHandler),
		client:        client,
		clientTimeout: 5 * time.Second,
		clientCache:   make(map[string]string),
		disableCache:  false,
		runInterval:   0,
		callbacks:     []CallbackFn{},
	}
	for _, opt := range opts {
		opt(libCfg)
	}
	return bind(libCfg, target)
}

func bind(libCfg *config, target interface{}) error {
	if libCfg.client == nil {
		return errors.New("etcd client is nil")
	}
	if target == nil {
		return errors.New("target is nil")
	}

	var (
		rt = reflect.TypeOf(target)
		rv = reflect.ValueOf(target)
	)

	if rt.Kind() != reflect.Ptr {
		return errors.New("target must be a pointer")
	}

	rt = rt.Elem()
	rv = rv.Elem()

	for i := 0; i < rt.NumField(); i++ {
		var (
			field = rt.Field(i)
			value = rv.FieldByName(field.Name)
		)

		// skip unexported, will panic @ downstream code otherwise
		if len(field.PkgPath) != 0 {
			continue
		}

		switch field.Type.Kind() {
		case reflect.Struct:
			return bind(libCfg, value.Addr().Interface())
		default:
			if secretPath := field.Tag.Get(libCfg.tagName); len(secretPath) > 0 {
				libCfg.logger.Debug(
					"binding -> field detected",
					slog.String("field", field.Name),
					slog.String("path", secretPath),
				)
				err := processBinding(libCfg, field, value, secretPath)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// processBinding reads the value from etcd and sets it to the field.
// v is a pointer to the reflected field we need to retrieve value for.
// path is a path in etcd
func processBinding(libCfg *config, f reflect.StructField, v reflect.Value, path string) error {
	var etcdValue string
	if _, ok := libCfg.clientCache[path]; !ok || libCfg.disableCache {
		var err error
		etcdValue, err = retrieveData(libCfg.client, libCfg.clientTimeout, path)
		if err != nil {
			return err
		}
		if !libCfg.disableCache {
			libCfg.clientCache[path] = etcdValue
		}
	} else {
		etcdValue = libCfg.clientCache[path]
	}

	libCfg.logger.Debug(
		"binding -> retrieved etcd value",
		slog.String("field", f.Name),
		slog.String("path", path),
		slog.String("value", etcdValue),
	)

	prevValue := v.Interface()

	switch v.Kind() {
	case reflect.Bool:
		val, err := strconv.ParseBool(etcdValue)
		if err != nil {
			libCfg.logger.Error(
				"binding -> failed to parse value",
				slog.String("field", f.Name),
				slog.String("path", path),
				slog.String("value", etcdValue),
				slog.String("error", err.Error()),
			)
			break
		}
		v.SetBool(val)
	case reflect.Float32, reflect.Float64:
		val, err := strconv.ParseFloat(etcdValue, 64)
		if err != nil {
			libCfg.logger.Error(
				"binding -> failed to parse value",
				slog.String("field", f.Name),
				slog.String("path", path),
				slog.String("value", etcdValue),
				slog.String("error", err.Error()),
			)
			break
		}
		v.SetFloat(val)
	case reflect.Int64:
		if f.Type == reflect.TypeOf(time.Duration(0)) {
			duration, err := time.ParseDuration(etcdValue)
			if err != nil {
				libCfg.logger.Error(
					"binding -> failed to parse value",
					slog.String("field", f.Name),
					slog.String("path", path),
					slog.String("value", etcdValue),
					slog.String("error", err.Error()),
				)
				break
			}
			v.SetInt(int64(duration))
		} else {
			val, err := strconv.ParseInt(etcdValue, 10, 64)
			if err != nil {
				libCfg.logger.Error(
					"binding -> failed to parse value",
					slog.String("field", f.Name),
					slog.String("path", path),
					slog.String("value", etcdValue),
					slog.String("error", err.Error()),
				)
				break
			}
			v.SetInt(val)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		val, err := strconv.ParseInt(etcdValue, 10, 64)
		if err != nil {
			libCfg.logger.Error(
				"binding -> failed to parse value",
				slog.String("field", f.Name),
				slog.String("path", path),
				slog.String("value", etcdValue),
				slog.String("error", err.Error()),
			)
			break
		}
		v.SetInt(val)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		val, err := strconv.ParseUint(etcdValue, 10, 64)
		if err != nil {
			libCfg.logger.Error(
				"binding -> failed to parse value",
				slog.String("field", f.Name),
				slog.String("path", path),
				slog.String("value", etcdValue),
				slog.String("error", err.Error()),
			)
			break
		}
		v.SetUint(val)
	case reflect.String:
		v.SetString(etcdValue)
	case reflect.Slice:
		slice := make([]string, 0)
		if len(etcdValue) > 0 {
			slice = append(slice, strings.Split(etcdValue, ",")...)
		}
		v.Set(reflect.ValueOf(slice))
	default:
		return fmt.Errorf("field %s have unsupported type %s", f.Name, v.Type())
	}

	// callback section
	if !reflect.DeepEqual(prevValue, v.Interface()) {
		for _, callback := range libCfg.callbacks {
			libCfg.logger.Debug(
				"binding -> calling callback",
				slog.String("field", f.Name),
				slog.String("path", path),
				slog.String("value", etcdValue),
				slog.Any("fn", callback),
			)
			err := callback(path, f.Name, prevValue, v.Interface())
			if err != nil {
				libCfg.logger.Error(
					"binding -> callback failed",
					slog.String("field", f.Name),
					slog.String("path", path),
					slog.String("value", etcdValue),
					slog.Any("fn", callback),
				)
			}
		}
	}

	libCfg.logger.Debug(
		"binding -> done",
		slog.String("field", f.Name),
		slog.String("path", path),
		slog.Any("value", etcdValue),
	)

	return nil
}

func retrieveData(cl etcdClientAccessor, timeout time.Duration, path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resp, err := cl.Get(ctx, path)
	if err != nil {
		return "", err
	}
	if len(resp.Kvs) == 0 {
		return "", errors.New("no data found")
	}
	return string(resp.Kvs[0].Value), nil
}
