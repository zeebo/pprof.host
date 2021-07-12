package main

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/pprof/driver"
	"github.com/google/pprof/profile"
	"github.com/zeebo/errs/v2"
	"github.com/zeebo/hmux"
)

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
		if _, err := profile.ParseData(data); err != nil {
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
