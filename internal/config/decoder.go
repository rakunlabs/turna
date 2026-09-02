package config

import "github.com/rakunlabs/gofret"

var decoder = gofret.New()

func Decode(input, output any) error {
	if input == nil {
		return nil
	}

	return decoder.ToInto(input, output)
}
