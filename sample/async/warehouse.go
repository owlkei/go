package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"time"

	"github.com/dangduoc08/ginject"
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

func (client *warehouseClient) quantity(sku string) int {
	return len(sku) * 10
}

type Warehouse struct {
	Client *warehouseClient
}

func (instance Warehouse) NewProvider() ginject.Provider {
	return instance
}

func (instance Warehouse) Quantity(sku string) int {
	return instance.Client.quantity(sku)
}

func NewWarehouseModule(ctx context.Context) (ginject.Module, error) {
	client, err := newWarehouseClient(os.Getenv("WAREHOUSE_URL"), os.Getenv("WAREHOUSE_DELAY"))
	if err != nil {
		return nil, err
	}

	err = ginject.Retry(ctx, &ginject.RetryOptions{InitialDelay: time.Second, MaxDelay: 10 * time.Second}, client.ping)
	if err != nil {
		return nil, err
	}

	return ginject.ModuleBuilder().
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
