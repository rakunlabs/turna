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

	for _, config := range c {
		if err := config.load(ctx, wg, cl, call); err != nil {
			return err
		}
	}

	return nil
}

func (c Config) load(ctx context.Context, wg *sync.WaitGroup, cl *clients, call Call) error {
	to := Data{}

	for _, static := range c.Statics {
		if err := static.load(ctx, &to, cl); err != nil {
			return err
		}
	}

	if len(c.Dynamics) > 0 {
		for _, dynamic := range c.Dynamics {
			waitCtx, err := dynamic.load(ctx, wg, &to, cl, &c, call)
			if err != nil {
				return err
			}

			// wait for the first load
			if waitCtx != nil {
				<-waitCtx.Done()
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

	if call != nil {
		call(ctx, c.Name, to.Hold)
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

	if c.Raw {
		if c.Map != "" {
			vMap := MapPath(c.Map, data).(map[string]interface{})
			to.Merge(vMap)
			dataProcessed = vMap
		} else {
			to.Raw = data
			dataProcessed = data
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

		innerValue := MapPath(c.Map, InnerPath(c.InnerPath, vMap))
		if m, ok := innerValue.(map[string]interface{}); ok {
			to.Merge(m)
			dataProcessed = innerValue
		} else {
			to.Raw = []byte(fmt.Sprint(innerValue))
			dataProcessed = to.Raw
		}
	}

	if c.Base64 && to.Raw != nil {
		if to.Raw, err = base64.StdEncoding.DecodeString(string(to.Raw)); err != nil {
			return fmt.Errorf("decode base64 error: %w", err)
		}

		dataProcessed = to.Raw
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

func (c ConfigDynamic) load(ctx context.Context, wg *sync.WaitGroup, to *Data, cl *clients, config *Config, call Call) (context.Context, error) {
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

	recordToMap := copyMap(to.Map)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()

		// release the startup wait once the first message has been received,
		// regardless of whether it processed cleanly.
		received := false

		for {
			if received {
				waitCancel()
			}

			select {
			case <-ctx.Done():
				return
			case data := <-ch:
				received = true

				if c.Consul.Template {
					v, err := renderTemplate(string(data), to.Hold)
					if err != nil {
						slog.Warn("failed to execute consul template", "err", err.Error())

						continue
					}

					data = v
				}

				if c.Consul.Raw {
					to.Raw = data
					to.AddHold(c.Consul.Name, data)
				} else {
					var vMap map[string]interface{}
					if err := decodeContent(c.Consul.Codec, data, &vMap); err != nil {
						slog.Warn("failed to load consul data", "err", err.Error())

						continue
					}

					// restore the static base before merging the new value
					to.Map = copyMap(recordToMap)
					to.Merge(vMap)
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

				if call != nil {
					call(ctx, config.Name, to.Hold)
				}
			}
		}
	}()

	return waitContext, nil
}

func (c ConfigDynamic) exportDynamic(config *Config, to *Data) error {
	if to.Raw != nil {
		return writeRaw(config.Export, to.Raw, config.FilePerm, config.FolderPerm)
	}

	return writeCodec(config.Export, to.Map, config.FilePerm, config.FolderPerm)
}
