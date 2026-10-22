package main

import (
	"github.com/ronigooja/Cylo/internal/capabilities"
	"github.com/ronigooja/Cylo/internal/control"
	"github.com/ronigooja/Cylo/internal/memory"
	"github.com/ronigooja/Cylo/internal/storage"
	"github.com/ronigooja/Cylo/internal/voice"
)

func composeControlServer(store *storage.Store, controlConfig control.Config, voiceConfig voice.Config, capabilityConfig capabilities.Config, memoryConfig memory.DerivedConfig) (*control.Server, error) {
	server, err := control.NewServer(store, controlConfig)
	if err != nil {
		return nil, err
	}
	if err = memory.Register(server, store); err != nil {
		return nil, err
	}
	if err = memory.RegisterDerived(server, store, memoryConfig); err != nil {
		return nil, err
	}
	if err = voice.Register(server, store, voiceConfig); err != nil {
		return nil, err
	}
	if err = capabilities.Register(server, store, capabilityConfig); err != nil {
		return nil, err
	}
	if err = control.RegisterInstanceRoutes(server); err != nil {
		return nil, err
	}
	if err = control.RegisterExportRoutes(server); err != nil {
		return nil, err
	}
	return server, nil
}
