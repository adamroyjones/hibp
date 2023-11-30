package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
)

func main() {
	var dir, port string
	flag.StringVar(&dir, "d", "", "The directory containing files to serve")
	flag.StringVar(&port, "p", "8009", "The port on which to serve (localhost)")
	flag.Parse()
	_, err := os.Stat(dir)
	assert(err == nil, "the directory %q must exist: %v", dir, err)

	slog.Info("Serving", slog.String("port", port), slog.String("dir", dir))
	http.Handle("/", http.FileServer(http.Dir(dir)))
	err = http.ListenAndServe(":"+port, nil)
	assert(err == nil, "the server produced an error: %v", err)
}

func assert(b bool, msg string, args ...any) {
	if !b {
		panic("assertion failed: " + fmt.Sprintf(msg, args...))
	}
}
