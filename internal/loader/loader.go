package loader

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"sync"
)

// Call is invoked after a Config finishes loading (and on every dynamic
// reload) with the config name and the accumulated named values.
type Call func(context.Context, string, map[string]interface{})

// clients holds the lazily-connected backends shared across a Load call.
type clients struct {
	consul *consulClient
	vault  *vaultClients
	http   *httpClient
	cloud  *cloudClients
}

// holds collects the named values of all configs of a Load call, so every
// callback sees the values of the other configs too.
type holds struct {
	mu   sync.Mutex
	data map[string]interface{}
}

// set merges values into the collected values and returns a fresh copy;
// the copy is handed to the callback and must not be modified.
func (h *holds) set(values map[string]interface{}) map[string]interface{} {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.data == nil {
		h.data = map[string]interface{}{}
	}

	for k, v := range values {
		h.data[k] = v
	}

	return copyMap(h.data)
}

// Load loads all configs to their export location and in memory.
//
// If a config uses dynamic sources, cancel ctx to stop watching.
func (c Configs) Load(ctx context.Context, wg *sync.WaitGroup, call Call) error {
	if wg == nil {
		wg = &sync.WaitGroup{}
	}

	cl := &clients{
		consul: &consulClient{},
		vault:  &vaultClients{},
		http:   &httpClient{},
		cloud:  &cloudClients{},
	}

	defer cl.cloud.close()

	all := &holds{}

	for _, config := range c {
		if err := config.load(ctx, wg, cl, all, call); err != nil {
			return err
		}
	}

	return nil
}

func (c Config) load(ctx context.Context, wg *sync.WaitGroup, cl *clients, all *holds, call Call) error {
	to := Data{}

	for _, static := range c.Statics {
		if err := static.load(ctx, &to, cl); err != nil {
			return err
		}
	}

	if len(c.Dynamics) > 0 {
		state := &dynamicState{
			to:     &to,
			base:   copyMap(to.Map),
			values: make([]interface{}, len(c.Dynamics)),
		}

		for i, dynamic := range c.Dynamics {
			waitCtx, err := dynamic.load(ctx, wg, state, i, cl, &c, all, call)
			if err != nil {
				return err
			}

			// wait for the first load
			if waitCtx != nil {
				select {
				case <-waitCtx.Done():
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}

		return nil
	}

	if to.Raw != nil {
		to.AddHold(c.Name, to.Raw)
	} else {
		to.AddHold(c.Name, to.Map)
	}

	if c.Export != "" {
		if to.Raw != nil {
			if err := writeRaw(c.Export, to.Raw, c.FilePerm, c.FolderPerm); err != nil {
				return err
			}
		} else {
			if err := writeCodec(c.Export, to.Map, c.FilePerm, c.FolderPerm); err != nil {
				return err
			}
		}
	}

	data := all.set(to.Hold)
	if call != nil {
		call(ctx, c.Name, data)
	}

	return nil
}

// source is a static source that produces raw bytes; the shared
// post-processing (template, raw, codec, inner_path, map, base64) is applied
// by loadSource.
type source interface {
	// fetch returns the raw content and an optional codec hint used when
	// codec is not set.
	fetch(ctx context.Context, cl *clients) ([]byte, string, error)
	common() *ConfigCommon
}

func (c *ConfigCommon) common() *ConfigCommon { return c }

func (c ConfigStatic) sources() []source {
	var sources []source

	if c.File != nil {
		sources = append(sources, c.File)
	}

	if c.HTTP != nil {
		sources = append(sources, c.HTTP)
	}

	if c.Content != nil {
		sources = append(sources, c.Content)
	}

	if c.AWSSecrets != nil {
		sources = append(sources, c.AWSSecrets)
	}

	if c.AWSSSM != nil {
		sources = append(sources, c.AWSSSM)
	}

	if c.AzureKeyVault != nil {
		sources = append(sources, c.AzureKeyVault)
	}

	if c.GCPSecret != nil {
		sources = append(sources, c.GCPSecret)
	}

	if c.GCPParameter != nil {
		sources = append(sources, c.GCPParameter)
	}

	return sources
}

func (c ConfigStatic) load(ctx context.Context, to *Data, cl *clients) error {
	if c.Consul != nil {
		if err := loadSource(ctx, c.Consul, to, cl); err != nil {
			return err
		}
	}

	if c.Vault != nil {
		if err := loadSource(ctx, c.Vault, to, cl); err != nil {
			return err
		}
	}

	for _, src := range c.sources() {
		if err := loadSource(ctx, src, to, cl); err != nil {
			return err
		}
	}

	return nil
}

func loadSource(ctx context.Context, src source, to *Data, cl *clients) error {
	data, codecHint, err := src.fetch(ctx, cl)
	if err != nil {
		return err
	}

	c := src.common()

	if c.Template {
		v, err := renderTemplate(string(data), to.Hold)
		if err != nil {
			return err
		}

		data = v
	}

	var dataProcessed interface{}

	// raw content of this source, base64 must not decode an earlier one
	var raw []byte

	if c.Raw {
		if c.Map != "" {
			vMap := MapPath(c.Map, data).(map[string]interface{})
			to.Merge(vMap)
			dataProcessed = vMap
		} else {
			raw = data
		}
	} else {
		codecName := c.Codec
		if codecName == "" {
			codecName = codecHint
		}

		var vMap map[string]interface{}
		if err := decodeContent(codecName, data, &vMap); err != nil {
			return err
		}

		inner := InnerPath(c.InnerPath, vMap)
		if inner == nil && c.InnerPath != "" {
			slog.Warn("load inner_path not found", "name", c.Name, "inner_path", c.InnerPath)

			return nil
		}

		innerValue := MapPath(c.Map, inner)
		if m, ok := innerValue.(map[string]interface{}); ok {
			to.Merge(m)
			dataProcessed = innerValue
		} else {
			raw = []byte(fmt.Sprint(innerValue))
		}
	}

	if raw != nil {
		if c.Base64 {
			if raw, err = base64.StdEncoding.DecodeString(string(raw)); err != nil {
				return fmt.Errorf("decode base64 error: %w", err)
			}
		}

		to.Raw = raw
		dataProcessed = raw
	}

	to.AddHold(c.Name, dataProcessed)

	return nil
}

func (c *ConfigConsul) fetch(ctx context.Context, cl *clients) ([]byte, string, error) {
	data, err := cl.consul.loadRaw(ctx, path.Join(c.PathPrefix, c.Path))

	return data, "", err
}

func (c *ConfigFile) fetch(_ context.Context, _ *clients) ([]byte, string, error) {
	data, err := loadFileRaw(c.Path)
	if err != nil {
		return nil, "", err
	}

	return data, codecNameByExt(filepath.Ext(c.Path)), nil
}

func (c *ConfigHTTP) fetch(ctx context.Context, cl *clients) ([]byte, string, error) {
	data, contentType, err := cl.http.loadRaw(ctx, c)
	if err != nil {
		return nil, "", err
	}

	return data, codecNameByContentType(contentType), nil
}

func (c *ConfigContent) fetch(_ context.Context, _ *clients) ([]byte, string, error) {
	return []byte(c.Content), "", nil
}

// dynamicState is the state of a config shared by its dynamic sources.
type dynamicState struct {
	mu sync.Mutex
	to *Data
	// base is the map of the static sources
	base map[string]interface{}
	// values are the latest map values of each dynamic source
	values []interface{}
}

func (c ConfigDynamic) load(ctx context.Context, wg *sync.WaitGroup, state *dynamicState, index int, cl *clients, config *Config, all *holds, call Call) (context.Context, error) {
	if wg == nil {
		wg = &sync.WaitGroup{}
	}

	if c.Consul == nil {
		return nil, nil
	}

	contentPath := path.Join(c.Consul.PathPrefix, c.Consul.Path)

	ch, cancel, err := cl.consul.dynamicValue(ctx, wg, contentPath)
	if err != nil {
		return nil, err
	}

	waitContext, waitCancel := context.WithCancel(ctx)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		defer waitCancel()

		for {
			var data []byte
			var ok bool

			select {
			case <-ctx.Done():
				return
			case data, ok = <-ch:
				if !ok {
					return
				}
			}

			hold := c.apply(state, index, data, config)

			// release the startup wait once the first message has been
			// received, regardless of whether it processed cleanly
			waitCancel()

			if hold == nil {
				continue
			}

			values := all.set(hold)
			if call != nil {
				call(ctx, config.Name, values)
			}
		}
	}()

	return waitContext, nil
}

// apply processes a new value of the dynamic source and returns the named
// values to publish, nil when the value is rejected.
func (c ConfigDynamic) apply(state *dynamicState, index int, data []byte, config *Config) map[string]interface{} {
	state.mu.Lock()
	defer state.mu.Unlock()

	to := state.to

	if c.Consul.Template {
		v, err := renderTemplate(string(data), copyMap(to.Hold))
		if err != nil {
			slog.Warn("failed to execute consul template", "err", err.Error())

			return nil
		}

		data = v
	}

	if c.Consul.Raw {
		to.Raw = data
		to.AddHold(c.Consul.Name, data)
	} else {
		var vMap map[string]interface{}
		if len(data) > 0 {
			if err := decodeContent(c.Consul.Codec, data, &vMap); err != nil {
				slog.Warn("failed to load consul data", "err", err.Error())

				return nil
			}
		}

		state.values[index] = vMap

		// rebuild from the static base and the latest value of every source
		to.Map = copyMap(state.base)
		for _, v := range state.values {
			if m, ok := v.(map[string]interface{}); ok {
				to.Merge(copyMap(m))
			}
		}

		to.AddHold(c.Consul.Name, vMap)
	}

	if to.Raw != nil {
		to.AddHold(config.Name, to.Raw)
	} else {
		to.AddHold(config.Name, to.Map)
	}

	if config.Export != "" {
		if err := c.exportDynamic(config, to); err != nil {
			slog.Warn("failed to save dynamic consul data to file", "filePath", config.Export, "err", err.Error())
		}
	}

	return copyMap(to.Hold)
}

func (c ConfigDynamic) exportDynamic(config *Config, to *Data) error {
	if to.Raw != nil {
		return writeRaw(config.Export, to.Raw, config.FilePerm, config.FolderPerm)
	}

	return writeCodec(config.Export, to.Map, config.FilePerm, config.FolderPerm)
}
