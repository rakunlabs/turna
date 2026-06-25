package loader

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"sync"
)

// Call is invoked after a Config finishes loading (and on every dynamic
// reload) with the config name and the accumulated named values.
type Call func(context.Context, string, map[string]interface{})

// clients holds the lazily-connected backends shared across a Load call.
type clients struct {
	consul *consulClient
	vault  *vaultClient
	http   *httpClient
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
		vault:  &vaultClient{},
		http:   &httpClient{},
	}

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

func (c ConfigStatic) load(ctx context.Context, to *Data, cl *clients) error {
	if c.Consul != nil {
		if err := c.loadConsul(ctx, to, cl); err != nil {
			return err
		}
	}

	if c.Vault != nil {
		if err := c.loadVault(ctx, to, cl); err != nil {
			return err
		}
	}

	if c.File != nil {
		if err := c.loadFile(to); err != nil {
			return err
		}
	}

	if c.HTTP != nil {
		if err := c.loadHTTP(ctx, to, cl); err != nil {
			return err
		}
	}

	if c.Content != nil {
		if err := c.loadContent(to); err != nil {
			return err
		}
	}

	return nil
}

func (c ConfigStatic) loadConsul(ctx context.Context, to *Data, cl *clients) error {
	contentPath := path.Join(c.Consul.PathPrefix, c.Consul.Path)

	data, err := cl.consul.loadRaw(ctx, contentPath)
	if err != nil {
		return err
	}

	if c.Consul.Template {
		v, err := renderTemplate(string(data), to.Hold)
		if err != nil {
			return err
		}

		data = v
	}

	var dataProcessed interface{}

	if c.Consul.Raw {
		if c.Consul.Map != "" {
			vMap := MapPath(c.Consul.Map, data).(map[string]interface{})
			to.Merge(vMap)
			dataProcessed = vMap
		} else {
			to.Raw = data
			dataProcessed = data
		}
	} else {
		var vMap map[string]interface{}
		if err := decodeContent(c.Consul.Codec, data, &vMap); err != nil {
			return err
		}

		innerValue := MapPath(c.Consul.Map, InnerPath(c.Consul.InnerPath, vMap))
		if m, ok := innerValue.(map[string]interface{}); ok {
			to.Merge(m)
			dataProcessed = innerValue
		} else {
			to.Raw = []byte(fmt.Sprint(innerValue))
			dataProcessed = to.Raw
		}
	}

	if c.Consul.Base64 && to.Raw != nil {
		if to.Raw, err = base64.StdEncoding.DecodeString(string(to.Raw)); err != nil {
			return fmt.Errorf("consul decode base64 error: %w", err)
		}

		dataProcessed = to.Raw
	}

	to.AddHold(c.Consul.Name, dataProcessed)

	return nil
}

func (c ConfigStatic) loadVault(ctx context.Context, to *Data, cl *clients) error {
	cl.vault.appRoleBasePath = c.Vault.AppRoleBasePath

	vMap, err := cl.vault.loadMap(ctx, c.Vault.PathPrefix, c.Vault.Path)
	if err != nil {
		return err
	}

	if c.Vault.Template {
		data, err := json.Marshal(vMap)
		if err != nil {
			return err
		}

		vRendered, err := renderTemplate(string(data), to.Hold)
		if err != nil {
			return err
		}

		var vX map[string]interface{}
		if err := json.Unmarshal(vRendered, &vX); err != nil {
			return err
		}

		vMap = vX
	}

	var dataProcessed interface{}
	innerValue := MapPath(c.Vault.Map, InnerPath(c.Vault.InnerPath, vMap))
	if m, ok := innerValue.(map[string]interface{}); ok {
		to.Merge(m)
		dataProcessed = innerValue
	} else {
		to.Raw = []byte(fmt.Sprint(innerValue))
		dataProcessed = to.Raw
	}

	if c.Vault.Base64 && to.Raw != nil {
		if to.Raw, err = base64.StdEncoding.DecodeString(string(to.Raw)); err != nil {
			return fmt.Errorf("vault decode base64 error: %w", err)
		}

		dataProcessed = to.Raw
	}

	to.AddHold(c.Vault.Name, dataProcessed)

	return nil
}

func (c ConfigStatic) loadFile(to *Data) error {
	data, err := loadFileRaw(c.File.Path)
	if err != nil {
		return err
	}

	if c.File.Template {
		v, err := renderTemplate(string(data), to.Hold)
		if err != nil {
			return err
		}

		data = v
	}

	var dataProcessed interface{}

	if c.File.Raw {
		if c.File.Map != "" {
			vMap := MapPath(c.File.Map, data).(map[string]interface{})
			to.Merge(vMap)
			dataProcessed = vMap
		} else {
			to.Raw = data
			dataProcessed = data
		}
	} else {
		var vMap map[string]interface{}
		if err := loadFileMap(c.File.Path, &vMap); err != nil {
			return err
		}

		innerValue := MapPath(c.File.Map, InnerPath(c.File.InnerPath, vMap))
		if m, ok := innerValue.(map[string]interface{}); ok {
			to.Merge(m)
			dataProcessed = innerValue
		} else {
			to.Raw = []byte(fmt.Sprint(innerValue))
			dataProcessed = to.Raw
		}
	}

	if c.File.Base64 && to.Raw != nil {
		if to.Raw, err = base64.StdEncoding.DecodeString(string(to.Raw)); err != nil {
			return fmt.Errorf("file decode base64 error: %w", err)
		}

		dataProcessed = to.Raw
	}

	to.AddHold(c.File.Name, dataProcessed)

	return nil
}

func (c ConfigStatic) loadHTTP(ctx context.Context, to *Data, cl *clients) error {
	data, contentType, err := cl.http.loadRaw(ctx, c.HTTP)
	if err != nil {
		return err
	}

	if c.HTTP.Template {
		v, err := renderTemplate(string(data), to.Hold)
		if err != nil {
			return err
		}

		data = v
	}

	var dataProcessed interface{}

	if c.HTTP.Raw {
		if c.HTTP.Map != "" {
			vMap := MapPath(c.HTTP.Map, data).(map[string]interface{})
			to.Merge(vMap)
			dataProcessed = vMap
		} else {
			to.Raw = data
			dataProcessed = data
		}
	} else {
		codecName := c.HTTP.Codec
		if codecName == "" {
			codecName = codecNameByContentType(contentType)
		}

		var vMap map[string]interface{}
		if err := decodeContent(codecName, data, &vMap); err != nil {
			return err
		}

		innerValue := MapPath(c.HTTP.Map, InnerPath(c.HTTP.InnerPath, vMap))
		if m, ok := innerValue.(map[string]interface{}); ok {
			to.Merge(m)
			dataProcessed = innerValue
		} else {
			to.Raw = []byte(fmt.Sprint(innerValue))
			dataProcessed = to.Raw
		}
	}

	if c.HTTP.Base64 && to.Raw != nil {
		if to.Raw, err = base64.StdEncoding.DecodeString(string(to.Raw)); err != nil {
			return fmt.Errorf("http decode base64 error: %w", err)
		}

		dataProcessed = to.Raw
	}

	to.AddHold(c.HTTP.Name, dataProcessed)

	return nil
}

func (c ConfigStatic) loadContent(to *Data) error {
	content := c.Content.Content
	if c.Content.Template {
		v, err := renderTemplate(content, to.Hold)
		if err != nil {
			return err
		}

		content = string(v)
	}

	var dataProcessed interface{}

	if c.Content.Raw {
		if c.Content.Map != "" {
			vMap := MapPath(c.Content.Map, []byte(content)).(map[string]interface{})
			to.Merge(vMap)
			dataProcessed = vMap
		} else {
			to.Raw = []byte(content)
			dataProcessed = []byte(content)
		}
	} else {
		var vMap map[string]interface{}
		if err := decodeContent(c.Content.Codec, []byte(content), &vMap); err != nil {
			return err
		}

		innerValue := MapPath(c.Content.Map, InnerPath(c.Content.InnerPath, vMap))
		if m, ok := innerValue.(map[string]interface{}); ok {
			to.Merge(m)
			dataProcessed = innerValue
		} else {
			to.Raw = []byte(fmt.Sprint(innerValue))
			dataProcessed = to.Raw
		}
	}

	if c.Content.Base64 && to.Raw != nil {
		var err error
		if to.Raw, err = base64.StdEncoding.DecodeString(string(to.Raw)); err != nil {
			return fmt.Errorf("content decode base64 error: %w", err)
		}

		dataProcessed = to.Raw
	}

	to.AddHold(c.Content.Name, dataProcessed)

	return nil
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
