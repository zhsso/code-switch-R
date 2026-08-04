package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
)

const maxRPCBodyBytes = 4 << 20

var errorType = reflect.TypeOf((*error)(nil)).Elem()

type rpcMethod struct {
	name string
	call reflect.Value
}

type rpcRegistry struct {
	mu      sync.RWMutex
	methods map[string]rpcMethod
}

type rpcRequest struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

type rpcResponse struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

func newRPCRegistry() *rpcRegistry {
	return &rpcRegistry{methods: make(map[string]rpcMethod)}
}

// Register exposes only the named methods. The browser cannot reach service
// lifecycle methods or arbitrary exported helpers through reflection.
func (r *rpcRegistry) Register(serviceName string, service any, methods ...string) {
	value := reflect.ValueOf(service)
	for _, methodName := range methods {
		method := value.MethodByName(methodName)
		if !method.IsValid() {
			panic(fmt.Sprintf("RPC method %s.%s does not exist", serviceName, methodName))
		}
		fullName := serviceName + "." + methodName
		r.mu.Lock()
		if _, exists := r.methods[fullName]; exists {
			r.mu.Unlock()
			panic("duplicate RPC method: " + fullName)
		}
		r.methods[fullName] = rpcMethod{name: fullName, call: method}
		r.mu.Unlock()
	}
}

func (r *rpcRegistry) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeRPCError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	request.Body = http.MaxBytesReader(w, request.Body, maxRPCBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var payload rpcRequest
	if err := decoder.Decode(&payload); err != nil {
		writeRPCError(w, http.StatusBadRequest, "invalid RPC request: "+err.Error())
		return
	}
	payload.Method = strings.TrimSpace(payload.Method)
	if payload.Method == "" {
		writeRPCError(w, http.StatusBadRequest, "method is required")
		return
	}

	r.mu.RLock()
	method, ok := r.methods[payload.Method]
	r.mu.RUnlock()
	if !ok {
		writeRPCError(w, http.StatusNotFound, "unknown RPC method")
		return
	}

	result, err := invokeRPC(method, payload.Args)
	if err != nil {
		writeRPCError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rpcResponse{Result: result})
}

func invokeRPC(method rpcMethod, args []json.RawMessage) (result any, err error) {
	methodType := method.call.Type()
	if len(args) != methodType.NumIn() {
		return nil, fmt.Errorf("%s expects %d arguments, received %d", method.name, methodType.NumIn(), len(args))
	}

	inputs := make([]reflect.Value, len(args))
	for index, raw := range args {
		targetType := methodType.In(index)
		target := reflect.New(targetType)
		if err := json.Unmarshal(raw, target.Interface()); err != nil {
			return nil, fmt.Errorf("argument %d for %s: %w", index+1, method.name, err)
		}
		inputs[index] = target.Elem()
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%s panicked: %v\n%s", method.name, recovered, debug.Stack())
			result = nil
		}
	}()
	outputs := method.call.Call(inputs)
	if len(outputs) > 0 && methodType.Out(len(outputs)-1).Implements(errorType) {
		errorValue := outputs[len(outputs)-1]
		outputs = outputs[:len(outputs)-1]
		if !errorValue.IsNil() {
			return nil, errorValue.Interface().(error)
		}
	}

	switch len(outputs) {
	case 0:
		return nil, nil
	case 1:
		return outputs[0].Interface(), nil
	default:
		values := make([]any, len(outputs))
		for index, output := range outputs {
			values[index] = output.Interface()
		}
		return values, nil
	}
}

func writeRPCError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, rpcResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		return
	}
}
