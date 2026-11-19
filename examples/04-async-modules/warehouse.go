package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"time"

	"go.owlky.com/owlky"
)

const defaultWarehouseURL = "http://warehouse.local"

var errWarehouseStarting = errors.New("warehouse: connection refused")

type warehouseClient struct {
	url     string
	readyAt time.Time
}

func (client *warehouseClient) ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if time.Now().Before(client.readyAt) {
		return errWarehouseStarting
	}
	return nil
}

var shelves = map[string]int{"green-tea": 30, "black-tea": 12, "teapot": 4}

func (client *warehouseClient) quantity(sku string) (int, bool) {
	n, ok := shelves[sku]
	return n, ok
}

type Warehouse struct {
	Client *warehouseClient
}

func (w Warehouse) NewProvider() owlky.Provider {
	return w
}

func (w Warehouse) Quantity(sku string) (int, bool) {
	return w.Client.quantity(sku)
}

// NewWarehouseModule connects to the warehouse. A bad setting is a permanent error: it is returned
// at once and the module fails. A refused connection is transient: Retry tries again with backoff
// until it works or the app shuts down.
func NewWarehouseModule(ctx context.Context) (owlky.Module, error) {
	client, err := newWarehouseClient(os.Getenv("WAREHOUSE_URL"), os.Getenv("WAREHOUSE_DELAY"))
	if err != nil {
		return nil, err
	}

	err = owlky.Retry(ctx, &owlky.RetryOptions{InitialDelay: time.Second, MaxDelay: 10 * time.Second}, client.ping)
	if err != nil {
		return nil, err
	}

	return owlky.ModuleBuilder().
		Providers(Warehouse{Client: client}).
		Build(), nil
}

func newWarehouseClient(rawURL, rawDelay string) (*warehouseClient, error) {
	if rawURL == "" {
		rawURL = defaultWarehouseURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errors.New("warehouse: WAREHOUSE_URL must be an http or https URL")
	}

	delay := 5 * time.Second
	if rawDelay != "" {
		delay, err = time.ParseDuration(rawDelay)
		if err != nil {
			return nil, errors.New("warehouse: WAREHOUSE_DELAY must be a duration such as 5s")
		}
	}

	return &warehouseClient{url: parsed.String(), readyAt: time.Now().Add(delay)}, nil
}
