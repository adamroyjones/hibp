package main

import (
	"crypto/rand"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path"
	"runtime"

	"golang.org/x/sync/errgroup"
)

func main() {
	var prefixes int
	var dir string
	flag.IntVar(&prefixes, "p", 16, "Number of 2-digit prefixes to generate")
	flag.StringVar(&dir, "d", ".", "Directory to write the ranges")
	flag.Parse()
	assert(prefixes > 0 && prefixes <= 256, "1..256 prefixes should be generated")

	slog.Info("Generating prefixes", slog.String("dir", dir), slog.Int("prefixes", prefixes))
	err := generate(dir, prefixes)
	assert(err == nil, "failed to generate data: %v", err)
	slog.Info("Finished generating prefixes")
}

func assert(b bool, msg string, args ...any) {
	if !b {
		panic("assertion failed: " + fmt.Sprintf(msg, args...))
	}
}

func generate(dir string, prefixes int) error {
	if err := os.MkdirAll(path.Join(dir, "range"), 0o755); err != nil {
		return fmt.Errorf("%q could not created: %w", path.Join(dir, "range"), err)
	}

	var eg errgroup.Group
	eg.SetLimit(runtime.GOMAXPROCS(0))
	for i := 0; i < prefixes; i++ {
		i := i
		eg.Go(func() error {
			size := 32_000
			bs := make([]byte, size*0x1000)
			if _, err := rand.Read(bs); err != nil { // As math/rand.Read is deprecated(!).
				return err
			}

			for i := range bs {
				if i > 0 && (i+1)%50 == 0 { // 49, 99, etc.
					bs[i] = '\n'
				} else {
					bs[i] = 97 + bs[i]%26 // i.e., a..z, so that curl produces readable output.
				}
			}

			for j := 0x000; j <= 0xfff; j++ {
				f, err := os.Create(path.Join(dir, "range", fmt.Sprintf("%02x%03x", i, j)))
				if err != nil {
					return err
				}
				if _, err := f.Write(bs[j:(j + size)]); err != nil {
					return err
				}
			}

			return nil
		})
	}

	return eg.Wait()
}
