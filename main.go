package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/handlers"
	_ "github.com/mattn/go-sqlite3"

	"github.com/google/pprof/driver"
	"github.com/google/pprof/profile"
	"github.com/zeebo/errs/v2"
	"github.com/zeebo/hmux"
	"golang.org/x/crypto/acme/autocert"
)

const domain = "pprof.host"

func main() {

	db, err := sql.Open("sqlite3", "file:profiles.db?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	store := &dbStore{db: db}
	if err := store.Init(context.Background()); err != nil {
		panic(err)
	}

	certManager := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domain),
		Cache:      autocert.DirCache("certs"),
	}

	handler := handlers.LoggingHandler(os.Stdout,
		certManager.HTTPHandler(hmux.Dir{
			"/": hmux.Method{
				"GET":  uiHandler{store: store},
				"POST": createHandler{domain: domain, store: store},
			},
			"*": hmux.Arg("name").Capture(pprofHandler{store: store}),
		}))

	lis, err := net.Listen("tcp", ":443")
	if err != nil {
		panic(err)
	}
	defer lis.Close()

	srv := &http.Server{
		Handler: handler,
		TLSConfig: &tls.Config{
			MinVersion:             tls.VersionTLS12,
			SessionTicketsDisabled: true,
			GetCertificate:         certManager.GetCertificate,
		},
	}

	go func() { panic(srv.ServeTLS(lis, "", "")) }()
	go func() { panic(http.ListenAndServe(":80", handler)) }()

	select {}
}

type createHandler struct {
	domain string
	store  *dbStore
}

func (h createHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if err := h.serveHTTP(rw, req); err != nil {
		http.Error(rw, fmt.Sprintf("%+v", err), http.StatusInternalServerError)
	}
}

func (h createHandler) serveHTTP(rw http.ResponseWriter, req *http.Request) error {
	mr, err := req.MultipartReader()
	if err != nil {
		return errs.Wrap(err)
	}
	for {
		part, err := mr.NextRawPart()
		if err == io.EOF {
			return errs.Errorf("invalid post: no profile found")
		}
		if part.FormName() != "profile" {
			continue
		}
		rc := http.MaxBytesReader(rw, part, 1<<20)
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return errs.Wrap(err)
		}
		name, err := h.store.Save(req.Context(), data)
		if err != nil {
			return errs.Wrap(err)
		}
		rw.Header().Set("Location", "/"+name+"/")
		rw.WriteHeader(http.StatusFound)
		fmt.Fprintf(rw, "https://%s/%s/\n", h.domain, name)
		return nil
	}
}

type uiHandler struct {
	store *dbStore
}

func (h uiHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if err := h.serveHTTP(rw, req); err != nil {
		http.Error(rw, fmt.Sprintf("%+v", err), http.StatusInternalServerError)
	}
}

func (h uiHandler) serveHTTP(rw http.ResponseWriter, req *http.Request) error {
	recs, err := h.store.Recent(req.Context(), 10)
	if err != nil {
		return errs.Wrap(err)
	}
	fmt.Fprintln(rw, `<style type="text/css">`)
	fmt.Fprintln(rw, `  td { border: 1px solid black; padding: 0.25em }`)
	fmt.Fprintln(rw, `  tt { background-color: #eeeeee; padding: 0.25em }`)
	fmt.Fprintln(rw, `</style>`)
	fmt.Fprintln(rw, `<table style="border-collapse: collapse">`)
	fmt.Fprintln(rw, "  <tr><td>Uploaded</td><td>Size</td><td>Name</td></tr>")
	for _, rec := range recs {
		fmt.Fprintf(rw, `  <tr><td>%s</td><td>%d</td><td><a href="/%s/">%s</a></td></tr>`,
			rec.created.Format(time.Stamp), rec.size, rec.name, rec.name,
		)
		fmt.Fprintln(rw)
	}
	fmt.Fprintln(rw, "</table>")
	fmt.Fprintln(rw, "<hr>")
	fmt.Fprintln(rw, "<h2>Upload</h2>")
	fmt.Fprintln(rw, `<form action="/" method="POST" enctype="multipart/form-data">`)
	fmt.Fprintln(rw, `  <input type="file" name="profile">`)
	fmt.Fprintln(rw, `  <input type="submit" value="Upload">`)
	fmt.Fprintln(rw, `</form>`)
	fmt.Fprintln(rw, "<p>command line: <tt>curl -F profile=@&lt;filename&gt; https://pprof.host</tt></p>")
	fmt.Fprintln(rw, `<p><a href="https://github.com/zeebo/pprof.host">pull requests welcome</a></p>`)
	return nil
}

type pprofHandler struct {
	store *dbStore
}

func (h pprofHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if err := h.serveHTTP(rw, req); err != nil {
		http.Error(rw, fmt.Sprintf("%+v", err), http.StatusInternalServerError)
	}
}

func (h pprofHandler) serveHTTP(rw http.ResponseWriter, req *http.Request) error {
	name := hmux.Arg("name").Value(req.Context())
	data, err := h.store.Load(req.Context(), name)
	if err != nil {
		return errs.Wrap(err)
	}
	p, err := profile.ParseData(data)
	if err != nil {
		return errs.Wrap(err)
	}
	redactSource(p)

	server := func(args *driver.HTTPServerArgs) error {
		args.Handlers["*"] = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			http.Redirect(rw, req, "/"+name+"/", http.StatusFound)
		})
		hmux.Dir(args.Handlers).ServeHTTP(rw, req)
		return nil
	}
	return errs.Wrap(driver.PProf(&driver.Options{
		Flagset: &flagset{args: []string{
			"--http", "localhost:0",
			"--symbolize", "none",
			"--source_path", "/dev/null",
		}},
		UI:         nullUI{},
		Fetch:      (*fetcherProfile)(p),
		HTTPServer: server,
	}))
}

type nullUI struct{}

func (nullUI) ReadLine(prompt string) (string, error)       { return "", io.EOF }
func (nullUI) Print(...interface{})                         {}
func (nullUI) PrintErr(...interface{})                      {}
func (nullUI) IsTerminal() bool                             { return false }
func (nullUI) WantBrowser() bool                            { return false }
func (nullUI) SetAutoComplete(complete func(string) string) {}

type fetcherProfile profile.Profile

func (f *fetcherProfile) Fetch(_ string, _, _ time.Duration) (*profile.Profile, string, error) {
	return (*profile.Profile)(f), "", nil
}

type flagset struct {
	flag.FlagSet
	args []string
}

func (p *flagset) AddExtraUsage(eu string) {}
func (p *flagset) ExtraUsage() string      { return "" }
func (p *flagset) StringList(name string, def string, usage string) *[]*string {
	return new([]*string)
}

func (p *flagset) Parse(usage func()) []string {
	_ = p.FlagSet.Parse(p.args)
	return []string{""}
}

func redactSource(p *profile.Profile) {
	redactLocations(p.Location)
	for _, sample := range p.Sample {
		redactLocations(sample.Location)
	}
	redactFunctions(p.Function)
}

func redactLocations(locs []*profile.Location) {
	for _, loc := range locs {
		redactLocation(loc)
	}
}

func redactLocation(loc *profile.Location) {
	for i := range loc.Line {
		redactLine(&loc.Line[i])
	}
}

func redactLine(line *profile.Line) {
	line.Line = 0
	redactFunction(line.Function)
}

func redactFunctions(fns []*profile.Function) {
	for _, fn := range fns {
		redactFunction(fn)
	}
}

func redactFunction(fn *profile.Function) {
	fn.Filename = ""
}
