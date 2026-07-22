package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/hold", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = fmt.Fprintln(w, "backend-held-ok")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, "backend-ok")
	})
	server := &http.Server{Addr: ":18080", Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	log.Fatal(server.ListenAndServe())
}
