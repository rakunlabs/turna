package decoder

import (
	"fmt"
	"io"

	"github.com/rakunlabs/chu/loader"
	"github.com/rakunlabs/chu/loader/fileloader"
	"github.com/rakunlabs/chu/utils/decoderfile"
	"github.com/rakunlabs/chu/utils/decodermap"
)

type Decoder struct {
	decoders   map[string]fileloader.Decoder
	mapDecoder *decodermap.Map
}

func NewDecoder() *Decoder {
	return &Decoder{
		decoders: decoders(),
		mapDecoder: decodermap.New(
			decodermap.WithHooks(loader.HookTimeDuration),
		),
	}
}

func (l Decoder) Decode(fileType string, data io.Reader, to any) error {
	decoder, ok := l.decoders[fileType]
	if !ok {
		return fmt.Errorf("unsupported file type: %s", fileType)
	}

	var mapping interface{}
	if err := decoder.Decode(data, &mapping); err != nil {
		return err
	}

	return l.mapDecoder.Decode(mapping, to)
}

func decoders() map[string]fileloader.Decoder {
	yamlDecoder := &decoderfile.Yaml{}

	return map[string]fileloader.Decoder{
		"toml": &decoderfile.Toml{},
		"yaml": yamlDecoder,
		"yml":  yamlDecoder,
		"json": &decoderfile.Json{},
	}
}
