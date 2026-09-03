package exchange

import (
	"fmt"
	"os"
	"strings"
)

type AdapterOptions struct {
	ExchangeEnv string
}

type AdapterFactory func(AdapterOptions) (Adapter, error)

var adapterFactories = map[string]AdapterFactory{
	"simulated": func(AdapterOptions) (Adapter, error) {
		return NewSimulatedAdapter(), nil
	},
}

func RegisterAdapter(name string, factory AdapterFactory) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || factory == nil {
		return
	}
	adapterFactories[name] = factory
}

func NewAdapter(name string) (Adapter, error) {
	return NewAdapterWithOptions(name, AdapterOptions{
		ExchangeEnv: os.Getenv("EXCHANGE_ENV"),
	})
}

func NewAdapterWithOptions(name string, options AdapterOptions) (Adapter, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "simulated"
	}
	factory, ok := adapterFactories[name]
	if !ok {
		return nil, fmt.Errorf("exchange adapter %q is not registered", name)
	}
	return factory(options)
}
