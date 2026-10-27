package capabilities

import (
	"context"
	"testing"

	"github.com/ronigooja/Zoe/internal/control"
	"github.com/ronigooja/Zoe/internal/storage"
)

func TestRegisterRejectsUnsupportedRuntimeBackend(t *testing.T) {
	store, err := storage.Open(context.Background(), storage.Config{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server, err := control.NewServer(store, control.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Register(server, store, Config{RuntimeBackend: "fixture"}); err == nil {
		t.Fatal("unknown capability runtime backend was accepted")
	}
}
