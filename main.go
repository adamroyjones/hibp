package main

import (
	"archive/tar"
	"bytes"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	base    = "http://localhost:8009/range"
	workers = 64
)

type hibp struct {
	prefixes int
	manual   bool
	client   http.Client
	bufs     []*bytes.Buffer
	tarBuf   *bytes.Buffer
}

func main() {
	var prefixes int
	flag.IntVar(&prefixes, "p", 0, "The number of prefixes to handle")
	var profile, manual bool
	flag.BoolVar(&manual, "manual", false, "Manually invoke the GC?")
	flag.BoolVar(&profile, "profile", false, "Collect a memory profile and a trace?")
	flag.Parse()
	assert(prefixes > 0, "the number of prefixes must be positive")

	slog.Info("Starting", slog.Int("prefixes", prefixes), slog.Bool("profile", profile), slog.Bool("manual", manual))

	if profile {
		tr, err := os.Create("./trace.out")
		assert(err == nil, "creating a trace file: %v", err)
		defer tr.Close()
		err = trace.Start(tr)
		assert(err == nil, "starting a trace: %v", err)
		defer trace.Stop()

		runtime.MemProfileRate = 1 // Record every allocation.
		f, err := os.Create("./memprof.out")
		assert(err == nil, "creating a memory profile file: %v", err)
		defer f.Close()

		defer func() {
			runtime.GC()
			err = pprof.WriteHeapProfile(f)
			assert(err == nil, "writing the heap profile: %v", err)
		}()
	}

	bufs := make([]*bytes.Buffer, 0x1000)
	for i := range bufs {
		bs := make([]byte, 0, 48_000) // A loose per-request upper bound.
		bufs[i] = bytes.NewBuffer(bs)
	}

	bs := make([]byte, 0, 160_000_000) // A loose upper bound for the tar.
	tarBuf := bytes.NewBuffer(bs)

	hibp := &hibp{
		prefixes: prefixes,
		manual:   manual,
		client:   http.Client{Timeout: time.Duration(30 * time.Second)},
		bufs:     bufs,
		tarBuf:   tarBuf,
	}
	err := hibp.run()
	assert(err == nil, "failed to finish running: %v", err)
}

func assert(b bool, msg string, args ...any) {
	if !b {
		panic("assertion failed: " + fmt.Sprintf(msg, args...))
	}
}

func (d *hibp) run() error {
	for i := 0; i < d.prefixes; i++ {
		chunkPrefix := fmt.Sprintf("%02x", i)
		slog.Info("Fetching a hash chunk", slog.String("prefix", chunkPrefix))
		if err := d.getChunk(i); err != nil {
			return fmt.Errorf("getting chunk with prefix %s, %w", chunkPrefix, err)
		}

		for _, buf := range d.bufs {
			buf.Reset()
		}
		d.tarBuf.Reset()
		if d.manual {
			runtime.GC()
		}
	}

	return nil
}

func (d *hibp) getChunk(two int) error {
	var eg errgroup.Group
	eg.SetLimit(workers)
	for j := 0x000; j <= 0xfff; j++ {
		three := j
		eg.Go(func() error {
			five := two*0x1000 + three
			if err := d.getOne(five, three); err != nil {
				return fmt.Errorf("fetching hashes for prefix %02x: %w", five, err)
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	if err := d.tar(two); err != nil {
		return fmt.Errorf("handling tar file (prefix: %03x): %w", two, err)
	}
	return nil
}

func (d *hibp) getOne(five, three int) error {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/%05x", base, five), nil)
	if err != nil {
		return err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code (%d != 200)", resp.StatusCode)
	}

	_, err = io.Copy(d.bufs[three], resp.Body)
	return err
}

func (d *hibp) tar(two int) error {
	// This isn't necessary, as we know a priori that 160MB will be enough, but...
	cap := 0
	for _, buf := range d.bufs {
		cap += 512       // The header.
		cap += buf.Len() // The body.
	}
	cap += 1024 // The two trailing 512-byte zero blocks.
	if diff := cap - d.tarBuf.Cap(); diff > 0 {
		d.tarBuf.Grow(diff)
	}

	tw := tar.NewWriter(d.tarBuf)
	hdr := tar.Header{Mode: 0o600}
	for three, buf := range d.bufs {
		hdr.Name = fmt.Sprintf("%05x", two*0x1000+three)
		hdr.Size = int64(buf.Len())
		if err := tw.WriteHeader(&hdr); err != nil {
			return err
		}
		if _, err := tw.Write(buf.Bytes()); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}

	_, err := io.Copy(io.Discard, d.tarBuf)
	return err
}
