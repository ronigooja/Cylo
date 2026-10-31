package ops

import (
	"context"
	"fmt"

	"github.com/ronigooja/Zoe/internal/control"
	"github.com/ronigooja/Zoe/internal/storage"
)

type ClientPolicyOptions struct{ DatabasePath string }

func ReadClientPolicy(ctx context.Context, options ClientPolicyOptions) (control.ClientPolicy, error) {
	store, err := openPolicyStore(ctx, options)
	if err != nil {
		return control.ClientPolicy{}, err
	}
	defer store.Close()
	return control.ReadClientPolicy(ctx, store.DB())
}

func UpdateClientPolicy(ctx context.Context, options ClientPolicyOptions, level string, interval int) (control.ClientPolicy, error) {
	store, err := openPolicyStore(ctx, options)
	if err != nil {
		return control.ClientPolicy{}, err
	}
	defer store.Close()
	return control.UpdateClientPolicy(ctx, store.DB(), level, interval)
}

func openPolicyStore(ctx context.Context, options ClientPolicyOptions) (*storage.Store, error) {
	if options.DatabasePath == "" {
		return nil, fmt.Errorf("database path is required")
	}
	store, err := storage.Open(ctx, storage.Config{Path: options.DatabasePath})
	if err != nil {
		return nil, err
	}
	if err = registerMigrations(store); err != nil {
		store.Close()
		return nil, err
	}
	if err = store.Migrate(ctx); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}
