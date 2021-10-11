package main

import (
	"os"

	"github.com/kovetskiy/ko"
)

type Config struct {
	Token   string `env:"MARK_USERNAME" toml:"token"`
	BaseURL string `env:"MARK_BASE_URL" toml:"base_url"`
}

func LoadConfig(path string) (*Config, error) {
	config := &Config{}
	err := ko.Load(path, config)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}

		return nil, err
	}

	return config, nil
}
