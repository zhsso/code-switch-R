package main

import (
	"bytes"
	"codeswitch/services"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type rpcFixture struct{}

func (rpcFixture) Add(left, right int) (int, error) {
	return left + right, nil
}

func (rpcFixture) Fail() error {
	return errors.New("fixture failure")
}

func (rpcFixture) Hidden() string {
	return "must not be callable"
}

func requestRPC(t *testing.T, registry *rpcRegistry, method string, args ...any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"method": method, "args": args})
	if err != nil {
		t.Fatalf("marshal RPC request: %v", err)
	}
	recorder := httptest.NewRecorder()
	registry.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/rpc", bytes.NewReader(body)))
	return recorder
}

func TestRPCRegistryInvokesAllowlistedMethod(t *testing.T) {
	registry := newRPCRegistry()
	registry.Register("fixture.Service", rpcFixture{}, "Add", "Fail")

	recorder := requestRPC(t, registry, "fixture.Service.Add", 20, 22)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response rpcResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode RPC response: %v", err)
	}
	if got, ok := response.Result.(float64); !ok || got != 42 {
		t.Fatalf("unexpected result %#v", response.Result)
	}
}

func TestRPCRegistryRejectsUnregisteredMethod(t *testing.T) {
	registry := newRPCRegistry()
	registry.Register("fixture.Service", rpcFixture{}, "Add")

	recorder := requestRPC(t, registry, "fixture.Service.Hidden")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestRPCRegistryReturnsServiceAndArgumentErrors(t *testing.T) {
	registry := newRPCRegistry()
	registry.Register("fixture.Service", rpcFixture{}, "Add", "Fail")

	failed := requestRPC(t, registry, "fixture.Service.Fail")
	if failed.Code != http.StatusBadRequest || !strings.Contains(failed.Body.String(), "fixture failure") {
		t.Fatalf("unexpected service error: %d %s", failed.Code, failed.Body.String())
	}

	badArgs := requestRPC(t, registry, "fixture.Service.Add", "not-an-int", 1)
	if badArgs.Code != http.StatusBadRequest || !strings.Contains(badArgs.Body.String(), "argument 1") {
		t.Fatalf("unexpected argument error: %d %s", badArgs.Code, badArgs.Body.String())
	}
}

func TestRPCRegistryRequiresPost(t *testing.T) {
	registry := newRPCRegistry()
	recorder := httptest.NewRecorder()
	registry.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/rpc", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
}

func TestRegisterWebServicesExposesAutoAvailabilityPolling(t *testing.T) {
	health := &services.HealthCheckService{}
	registry := newRPCRegistry()
	registerWebServices(registry, webServices{health: health})

	recorder := requestRPC(
		t,
		registry,
		"codeswitch/services.HealthCheckService.SetAutoAvailabilityPolling",
		false,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
}
