package main

import (
	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
	"os"
)

type Config struct {
	Mode            string `yaml:"mode" validate:"required,oneof=server client"`
	GithubSecret    string `yaml:"githubSecret"`
	Host            string `yaml:"host"`
	Port            string `yaml:"port"`
	ServerEndpoint  string `yaml:"serverEndpoint"`
	SubscribeSecret string `yaml:"subscribeSecret"`
	Metrics         struct {
		Enabled bool   `yaml:"enabled"`
		Host    string `yaml:"host"`
		Port    string `yaml:"port"`
	} `yaml:"metrics"`
}

func LoadConfig(configPath string) (Config, error) {
	var config Config

	file, err := os.Open(configPath)
	if err != nil {
		return config, err
	}
	defer file.Close()

	// Init new YAML decode
	d := yaml.NewDecoder(file)

	// Start YAML decoding from file
	if err := d.Decode(&config); err != nil {
		return config, err
	}

	if config.Host == "" {
		config.Host = "localhost"
	}

	if config.Port == "" {
		config.Port = "3400"
	}

	if config.ServerEndpoint == "" {
		config.ServerEndpoint = "ws://localhost:3400/subscribe"
	}

	if os.Getenv("GITHUB_WEBHOOK_SECRET") != "" {
		config.GithubSecret = os.Getenv("GITHUB_WEBHOOK_SECRET")
	}

	if os.Getenv("SUBSCRIBE_SECRET") != "" {
		config.SubscribeSecret = os.Getenv("SUBSCRIBE_SECRET")
	}

	validate := validator.New(validator.WithRequiredStructEnabled())

	if err := validate.Struct(config); err != nil {
		return config, err
	}

	return config, nil
}
