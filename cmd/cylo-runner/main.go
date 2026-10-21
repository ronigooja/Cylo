// cylo-runner executes server-authorized runs through AO. Credentials and
// route configuration are deployment-provided environment values.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ronigooja/Cylo/contracts"
	"github.com/ronigooja/Cylo/internal/agents/ao"
	"github.com/ronigooja/Cylo/internal/runner"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Printf("cylo-runner stopped: %v", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("cylo-runner", flag.ContinueOnError)
	once := flags.Bool("once", false, "reconcile current runs and perform one claim poll")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: cylo-runner [--once]")
	}
	baseURL := requiredEnv("CYLO_CONTROL_URL")
	controlToken := requiredEnv("CYLO_RUNNER_TOKEN")
	aoURL := requiredEnv("CYLO_AO_URL")
	aoToken := requiredEnv("CYLO_AO_TOKEN")
	journalPath := requiredEnv("CYLO_RUNNER_JOURNAL_PATH")
	routes, err := loadRoutes(os.Getenv("CYLO_RUNNER_ROUTES"))
	if err != nil {
		return err
	}
	control, err := runner.NewControlClient(baseURL, controlToken, nil)
	if err != nil {
		return fmt.Errorf("configure control API: %w", err)
	}
	agent, err := ao.New(ao.Config{BaseURL: aoURL, Token: aoToken})
	if err != nil {
		return fmt.Errorf("configure AO gateway: %w", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	deploymentLock, err := runner.OpenDeploymentHostLock(runner.DeploymentHostLockPath)
	if err != nil {
		return err
	}
	defer deploymentLock.Close()
	deploymentAuthority, err := runner.OpenDeploymentAuthority(ctx, runner.DeploymentAuthorityPath)
	if err != nil {
		return err
	}
	defer deploymentAuthority.Close()
	journal, err := runner.OpenJournal(ctx, journalPath)
	if err != nil {
		return err
	}
	defer journal.Close()
	docker := runner.ExecDockerCommand{Binary: optionalEnv("CYLO_DOCKER_BINARY", "docker")}
	codexImage := strings.TrimSpace(os.Getenv("AO_CODEX_CONTAINER_IMAGE"))
	engine := &runner.Engine{
		Control:        control,
		Instance:       control,
		AO:             agent,
		Containers:     runner.DockerAOContainerSupervisor{Docker: docker},
		Journal:        journal,
		Routes:         routes,
		Isolation:      runner.SupervisedCodexIsolation{Docker: docker, Image: codexImage},
		PollInterval:   durationEnv("CYLO_RUNNER_POLL_INTERVAL", 2*time.Second),
		ObserveTimeout: durationEnv("CYLO_RUNNER_OBSERVE_TIMEOUT", 15*time.Second),
	}
	deployments := &runner.DeploymentWorker{Control: control, Journal: journal, Executor: &runner.DeploymentExecutor{Docker: docker, Fences: deploymentAuthority}}
	return serve(ctx, engine, deployments, *once, durationEnv("CYLO_RUNNER_IDLE_POLL_INTERVAL", 2*time.Second))
}

func serve(ctx context.Context, engine *runner.Engine, deployments *runner.DeploymentWorker, once bool, idle time.Duration) error {
	// Current claims include work already assigned to this runner. Replay those
	// exact journaled requests before accepting another claim after restart.
	current, err := engine.Control.Current(ctx)
	if err != nil {
		return fmt.Errorf("reconcile current claims: %w", err)
	}
	if once && deployments != nil {
		if err := deployments.Run(ctx, true, idle); err != nil && !errors.Is(err, runner.ErrNoDeployment) {
			return err
		}
	}
	var wg sync.WaitGroup
	var deploymentDone chan struct{}
	if !once && deployments != nil {
		deploymentDone = make(chan struct{})
		go func() {
			defer close(deploymentDone)
			if err := deployments.Run(ctx, false, idle); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("deployment worker polling failed: %v", err)
			}
		}()
	}
	shutdown := func() {
		wg.Wait()
		if deploymentDone != nil {
			<-deploymentDone
		}
	}
	active := make(chan struct{}, 4)
	start := func(claim contracts.JobClaim, recovery bool) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-active }()
			if err := engine.Process(ctx, claim); err != nil && !errors.Is(err, runner.ErrOutcomePending) {
				label := "runner iteration failed"
				if recovery {
					label = "run reconciliation remains pending"
				}
				log.Printf("%s for run %s: %v", label, safeID(claim.RunID), err)
			}
		}()
	}
	for _, claim := range current {
		if len(active) == cap(active) {
			log.Printf("more current claims than the runner's four-run local limit; leaving run %s for another runner", safeID(claim.RunID))
			continue
		}
		select {
		case active <- struct{}{}:
			start(claim, true)
		case <-ctx.Done():
			shutdown()
			return nil
		}
	}
	if once {
		select {
		case active <- struct{}{}:
			err := engine.RunOnce(ctx)
			<-active
			shutdown()
			if errors.Is(err, runner.ErrNoClaim) {
				return nil
			}
			return err
		case <-ctx.Done():
			shutdown()
			return nil
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			shutdown()
			return nil
		}
		select {
		case active <- struct{}{}:
		case <-ctx.Done():
			shutdown()
			return nil
		}
		claim, err := engine.Control.Claim(ctx)
		switch {
		case err == nil:
			if claim == nil {
				<-active
			} else {
				start(*claim, false)
			}
		case errors.Is(err, runner.ErrNoClaim):
			<-active
		case errors.Is(err, runner.ErrOutcomePending):
			<-active
		default:
			<-active
			log.Printf("runner iteration failed; retrying after idle interval: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(idle):
		}
	}
}

func loadRoutes(raw string) (map[string]runner.Route, error) {
	routes := map[string]runner.Route{}
	if strings.TrimSpace(raw) == "" {
		return routes, errors.New("CYLO_RUNNER_ROUTES is required; it must match server-selected profiles")
	}
	if err := json.Unmarshal([]byte(raw), &routes); err != nil || len(routes) == 0 {
		return nil, errors.New("CYLO_RUNNER_ROUTES must be a non-empty JSON object keyed by profile ID")
	}
	for id, route := range routes {
		if id == "" || route.ProfileID != id || route.RuntimeRouteID == "" || route.ModelRouteID == "" || route.IsolationPolicy == "" || route.Harness == "" || route.Model == "" {
			return nil, fmt.Errorf("runner route %q is incomplete or has a mismatched profile id", id)
		}
	}
	return routes, nil
}

func requiredEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return ""
	}
	return value
}

func optionalEnv(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func safeID(id string) string {
	if len(id) > 64 {
		return id[:64]
	}
	return id
}
