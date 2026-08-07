package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

const (
	defaultBindHost  = "127.0.0.1"
	defaultWebPort   = 8080
	defaultRelayPort = 18100
)

type serverConfig struct {
	BindHost  string
	WebPort   int
	RelayPort int
}

func loadServerConfig() (serverConfig, error) {
	bindHost, err := envBindHost("CODESWITCH_BIND_HOST", defaultBindHost)
	if err != nil {
		return serverConfig{}, err
	}
	webPort, err := envPort("CODESWITCH_WEB_PORT", defaultWebPort)
	if err != nil {
		return serverConfig{}, err
	}
	relayPort, err := envPort("CODESWITCH_RELAY_PORT", defaultRelayPort)
	if err != nil {
		return serverConfig{}, err
	}
	if webPort == relayPort {
		return serverConfig{}, fmt.Errorf("WebUI and relay ports must differ")
	}
	return serverConfig{BindHost: bindHost, WebPort: webPort, RelayPort: relayPort}, nil
}

func envBindHost(name string, fallback string) (string, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	if net.ParseIP(raw) == nil {
		return "", fmt.Errorf("%s must be an IP address", name)
	}
	return raw, nil
}

func envPort(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s must be an integer between 1 and 65535", name)
	}
	return port, nil
}

func (c serverConfig) WebAddr() string {
	return net.JoinHostPort(c.BindHost, strconv.Itoa(c.WebPort))
}

func (c serverConfig) RelayAddr() string {
	return net.JoinHostPort(c.BindHost, strconv.Itoa(c.RelayPort))
}
