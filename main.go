package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gorilla/handlers"
	"github.com/zeebo/errs/v2"
	"github.com/zeebo/hmux"
	"golang.org/x/crypto/acme/autocert"
)

const domain = "pprof.host"

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("%+v", err)
	}
}

func run(ctx context.Context) error {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?%s",
		statePath("profiles.db"),
		url.Values{
			"_busy_timeout": {"5000"},
			"_journal_mode": {"WAL"},
			"_synchronous":  {"NORMAL"},
		}.Encode()))
	if err != nil {
		return errs.Wrap(err)
	}
	defer db.Close()

	store := &dbStore{db: db}
	if err := store.Init(ctx); err != nil {
		return errs.Wrap(err)
	}

	certManager := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domain),
		Cache:      autocert.DirCache(statePath("certs")),
	}

	handler := handlers.LoggingHandler(os.Stdout,
		certManager.HTTPHandler(hmux.Dir{
			"/": hmux.Method{
				"GET":  uiHandler{store: store},
				"POST": createHandler{domain: domain, store: store},
			},
			"*": hmux.Arg("name").Capture(pprofHandler{store: store}),
		}))

	lis443, lis80, err := listen(ctx)
	if err != nil {
		return errs.Wrap(err)
	}
	defer lis443.Close()
	defer lis80.Close()

	srv := &http.Server{
		Handler: handler,
		TLSConfig: &tls.Config{
			MinVersion:             tls.VersionTLS12,
			SessionTicketsDisabled: true,
			GetCertificate:         certManager.GetCertificate,
		},
	}

	go func() { panic(srv.ServeTLS(lis443, "", "")) }()
	go func() { panic(srv.Serve(lis80)) }()

	select {}
}

func statePath(path string) string {
	return filepath.Join(os.Getenv("STATE_DIRECTORY"), path)
}
