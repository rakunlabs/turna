package config

import "github.com/worldline-go/struct2"

func Decode(input, output any) error {
	if input == nil {
		return nil
	}

	decoder := struct2.Decoder{
		TagName:               "cfg",
		WeaklyTypedInput:      true,
		WeaklyIgnoreSeperator: true,
		WeaklyDashUnderscore:  true,
	}

	return decoder.Decode(input, output)
}
