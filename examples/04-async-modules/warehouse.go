package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"go.owlkei.com/owlkei"
)

var errConnectionRefused = errors.New("connection refused")

// warehouseClient stands in for a real client: it refuses connections until readyAt, then answers
// from a fixed stock list.
type warehouseClient struct {
	url     string
	readyAt time.Time
	stock   map[string]int
}

func (wc *warehouseClient) ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if time.Now().Before(wc.readyAt) {
		return fmt.Errorf("warehouse %s: %w", wc.url, errConnectionRefused)
	}
	return nil
}

func (wc *warehouseClient) quantity(sku string) (int, bool) {
	n, ok := wc.stock[sku]
	return n, ok
}

type Warehouse struct {
	Client *warehouseClient
}

func (w Warehouse) NewProvider() owlkei.Provider {
	return w
}

func (w Warehouse) Quantity(sku string) (int, bool) {
	return w.Client.quantity(sku)
}

// NewWarehouseModule connects to the warehouse. A bad setting is a permanent error: it is returned
// at once and the module fails. A refused connection is transient: Retry tries again with backoff
// until it works or the app shuts down.
func NewWarehouseModule(ctx context.Context) (owlkei.Module, error) {
	client, err := newWarehouseClient(os.Getenv("WAREHOUSE_URL"), os.Getenv("WAREHOUSE_DELAY"))
	if err != nil {
		return nil, err
	}

	backoff := &owlkei.RetryOptions{InitialDelay: time.Second, MaxDelay: 10 * time.Second}
	if err := owlkei.Retry(ctx, backoff, client.ping); err != nil {
		return nil, fmt.Errorf("connect to the warehouse: %w", err)
	}

	return owlkei.ModuleBuilder().
		Providers(Warehouse{Client: client}).
		Build(), nil
}

func newWarehouseClient(rawURL, rawDelay string) (*warehouseClient, error) {
	if rawURL == "" {
		rawURL = "http://warehouse.local"
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("WAREHOUSE_URL %q: %w", rawURL, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("WAREHOUSE_URL %q: want an http or https URL", rawURL)
	}

	delay := 5 * time.Second
	if rawDelay != "" {
		if delay, err = time.ParseDuration(rawDelay); err != nil {
			return nil, fmt.Errorf("WAREHOUSE_DELAY %q: %w", rawDelay, err)
		}
	}

	return &warehouseClient{
		url:     parsed.String(),
		readyAt: time.Now().Add(delay),
		stock:   map[string]int{"green-tea": 30, "black-tea": 12, "teapot": 4},
	}, nil
}
