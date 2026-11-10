package citysim

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"hydroledger-prototype/hydroledger/verifier"
	"hydroledger-prototype/hydroledger/verifier/identity"
	"hydroledger-prototype/migrations"
	"hydroledger-prototype/service"
)

// Stack is a complete, running VarunaSetu backend with a virtual clock: the
// real service.Server (identity, verifier, repository, ledger) behind a real
// HTTP listener, plus the virtual city that drives it.
type Stack struct {
	World  *World
	Clock  *Clock
	DB     *sql.DB
	Server *service.Server
	HTTP   *http.Server
	Base   string
	Runner *Runner
	Mux    *http.ServeMux
	Repo   *service.Repository
	VCfg   verifier.Config
}

// ApplyMigrations runs every embedded migration (all are idempotent).
func ApplyMigrations(ctx context.Context, db *sql.DB) error {
	ents, err := migrations.FS.ReadDir(".")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := migrations.FS.ReadFile(n)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, string(b)); err != nil {
			return fmt.Errorf("migration %s: %w", n, err)
		}
	}
	return nil
}

// StartStack opens the database, applies migrations, builds the world, and
// starts the service listening on addr ("127.0.0.1:0" picks a free port).
func StartStack(ctx context.Context, dbURL, addr string, wcfg WorldConfig, verbose bool) (*Stack, error) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	if err := ApplyMigrations(ctx, db); err != nil {
		return nil, err
	}
	w, err := NewWorld(wcfg)
	if err != nil {
		return nil, err
	}
	clock := NewClock(time.Now().UTC())
	v, err := verifier.New(verifier.DefaultConfig())
	if err != nil {
		return nil, err
	}
	var out io.Writer = io.Discard
	if verbose {
		out = nil
	}
	var logger *slog.Logger
	if out == nil {
		logger = slog.Default()
	} else {
		logger = slog.New(slog.NewTextHandler(out, nil))
	}
	repo := &service.Repository{DB: db}
	srv, err := service.New(service.Config{
		Verifier: v, KeyStore: repo, Revoked: repo, IdentityConfig: identity.DefaultConfig(),
		Repository: repo, Logger: logger, Now: clock.Now, CORSAllowOrigin: "*",
		RequestTimeout: 15 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/", srv.Handler())
	st := &Stack{World: w, Clock: clock, DB: db, Server: srv, Mux: mux, Repo: repo, VCfg: v.Config(), Base: "http://" + ln.Addr().String()}
	st.HTTP = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = st.HTTP.Serve(ln) }()
	st.Runner = NewRunner(w, NewAPI(st.Base), db, clock, srv.Sweep)
	return st, nil
}

// Close stops the listener and the database handle.
func (s *Stack) Close() {
	s.Server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.HTTP.Shutdown(ctx)
	_ = s.DB.Close()
}
