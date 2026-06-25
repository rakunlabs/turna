package loader

import "github.com/rakunlabs/mapx"

// Data holds the accumulated state while loading a single Config.
//
//   - Map is the merged map of all non-raw sources.
//   - Raw is the last raw value (used when a source produces non-map content).
//   - Hold keeps named values exposed to templates and the load callback.
type Data struct {
	Map  map[string]interface{}
	Raw  []byte
	Hold map[string]interface{}
}

// AddHold stores a named value in Hold. Empty keys are ignored.
func (d *Data) AddHold(k string, v interface{}) {
	if k == "" {
		return
	}

	if d.Hold == nil {
		d.Hold = map[string]interface{}{}
	}

	d.Hold[k] = v
}

// Merge merges v into the running Map.
func (d *Data) Merge(v map[string]interface{}) {
	if d.Map == nil {
		d.Map = map[string]interface{}{}
	}

	mapx.Merge(v, d.Map)
}

func copyMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		if vm, ok := v.(map[string]interface{}); ok {
			out[k] = copyMap(vm)
		} else {
			out[k] = v
		}
	}

	return out
}
