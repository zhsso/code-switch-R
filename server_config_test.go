package main

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestLoadServerConfigDefaultsToLoopback(t *testing.T) {
	t.Setenv("CODESWITCH_WEB_PORT", "")
	t.Setenv("CODESWITCH_RELAY_PORT", "")

	config, err := loadServerConfig()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if config.WebPort != defaultWebPort || config.RelayPort != defaultRelayPort {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	for name, address := range map[string]string{
		"web":   config.WebAddr(),
		"relay": config.RelayAddr(),
	} {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			t.Fatalf("parse %s address %q: %v", name, address, err)
		}
		if host != "127.0.0.1" {
			t.Fatalf("%s must bind to loopback, got %q", name, host)
		}
	}
}

func TestLoadServerConfigUsesEnvironmentPorts(t *testing.T) {
	t.Setenv("CODESWITCH_WEB_PORT", "9080")
	t.Setenv("CODESWITCH_RELAY_PORT", "19100")

	config, err := loadServerConfig()
	if err != nil {
		t.Fatalf("load environment ports: %v", err)
	}
	if config.WebAddr() != "127.0.0.1:9080" || config.RelayAddr() != "127.0.0.1:19100" {
		t.Fatalf("unexpected addresses: web=%s relay=%s", config.WebAddr(), config.RelayAddr())
	}
}

func TestLoadServerConfigRejectsInvalidPorts(t *testing.T) {
	for _, value := range []string{"abc", "0", "65536", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("CODESWITCH_WEB_PORT", value)
			t.Setenv("CODESWITCH_RELAY_PORT", strconv.Itoa(defaultRelayPort))
			if _, err := loadServerConfig(); err == nil || !strings.Contains(err.Error(), "CODESWITCH_WEB_PORT") {
				t.Fatalf("expected a web port validation error for %q, got %v", value, err)
			}
		})
	}
}

func TestLoadServerConfigRejectsSharedPort(t *testing.T) {
	t.Setenv("CODESWITCH_WEB_PORT", "18080")
	t.Setenv("CODESWITCH_RELAY_PORT", "18080")
	if _, err := loadServerConfig(); err == nil {
		t.Fatal("expected equal ports to be rejected")
	}
}
