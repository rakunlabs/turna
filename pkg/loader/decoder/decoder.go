package decoder

import (
	"fmt"
	"io"

	"github.com/rakunlabs/chu/loader"
	"github.com/rakunlabs/chu/loader/loaderfile"
	"github.com/rakunlabs/chu/utils/decoder"
)

type Decoder struct {
	decoders   map[string]loaderfile.Decoder
	mapDecoder *decoder.Map
}

func NewDecoder() *Decoder {
	return &Decoder{
		decoders: decoders(),
		mapDecoder: decoder.New(
			decoder.WithHooks(loader.HookTimeDuration),
		),
	}
}

func (l Decoder) Decode(fileType string, data io.Reader, to any) error {
	decode, ok := l.decoders[fileType]
	if !ok {
		return fmt.Errorf("unsupported file type: %s", fileType)
	}

	var mapping any
	if err := decode(data, &mapping); err != nil {
		return err
	}

	return l.mapDecoder.Decode(mapping, to)
}

func decoders() map[string]loaderfile.Decoder {
	return map[string]loaderfile.Decoder{
		"toml": decoder.DecodeToml,
		"yaml": decoder.DecodeYaml,
		"yml":  decoder.DecodeYaml,
		"json": decoder.DecodeJson,
	}
}
