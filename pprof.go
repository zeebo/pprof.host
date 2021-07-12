package main

import (
	"flag"
	"io"
	"time"

	"github.com/google/pprof/profile"
)

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
	redactSamples(p.Sample)
	redactFunctions(p.Function)
}

func redactSamples(ss []*profile.Sample) {
	for _, s := range ss {
		redactSample(s)
	}
}

func redactSample(s *profile.Sample) {
	redactLocations(s.Location)
}

func redactLocations(locs []*profile.Location) {
	for _, loc := range locs {
		redactLocation(loc)
	}
}

func redactLocation(loc *profile.Location) {
	loc.Address = 0
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
