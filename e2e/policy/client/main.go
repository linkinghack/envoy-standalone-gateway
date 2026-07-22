package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	url := flag.String("url", "", "request URL")
	header := flag.String("header", "", "optional Name:Value request header")
	flag.Parse()
	if *url == "" {
		fmt.Fprintln(os.Stderr, "-url is required")
		os.Exit(2)
	}
	req, err := http.NewRequest(http.MethodGet, *url, nil)
	if err != nil {
		fatal(err)
	}
	if *header != "" {
		name, value, ok := strings.Cut(*header, ":")
		if !ok || name == "" {
			fatal(fmt.Errorf("invalid -header %q", *header))
		}
		req.Header.Set(name, value)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		fatal(err)
	}
	fmt.Println(resp.StatusCode)
	fmt.Print(string(body))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
