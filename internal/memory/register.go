package memory

import (
	"database/sql"
	"net/http"

	"github.com/ronigooja/Zoe/internal/control"
	"github.com/ronigooja/Zoe/internal/storage"
)

func Register(s *control.Server, store *storage.Store) error {
	if s == nil || store == nil {
		return errorsf("memory registration requires control server and storage")
	}
	if err := RegisterMigrations(store); err != nil {
		return err
	}
	repository := NewRepositoryWithStore(store)
	service := &Service{repo: repository, db: store.DB()}
	for kind, handler := range map[string]control.CommandHandler{
		"memory.remember":         service.commandRemember,
		"memory.delete":           service.commandDelete,
		"memory.proposal.approve": service.commandApproveProposal,
	} {
		if err := s.RegisterCommand(kind, handler); err != nil {
			return err
		}
	}
	if err := s.RegisterAuthenticatedRoutes(func(mux *http.ServeMux) {
		mux.HandleFunc("GET /v1/memory", service.handleSearch)
		mux.HandleFunc("GET /v1/memory/context", service.handleContext)
	}); err != nil {
		return err
	}
	if err := s.RegisterRunnerRoutes(func(mux *http.ServeMux) {
		mux.HandleFunc("POST /runner/v1/memory/proposals", service.handleRunnerProposal)
	}); err != nil {
		return err
	}
	return nil
}

type Service struct {
	repo *Repository
	db   *sql.DB
}

func errorsf(message string) error { return &registrationError{message: message} }

type registrationError struct{ message string }

func (e *registrationError) Error() string { return e.message }
