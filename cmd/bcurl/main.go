package main

import (
	"fmt"
	"os"
	"strings"

	"bhttp/pkg/bhttp"
)

func main() {
	verbose := false
	var urls []string

	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "-v" || arg == "--verbose" {
			verbose = true
		} else if strings.HasPrefix(arg, "-") {
			fmt.Fprintf(os.Stderr, "Unknown flag: %s\n", arg)
			os.Exit(2)
		} else {
			urls = append(urls, arg)
		}
	}

	if len(urls) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: %s [-v] <host:port/path...>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s -v localhost:9000/index.html\n", os.Args[0])
		os.Exit(2)
	}

	client := bhttp.NewClient(verbose)
	defer client.Close()

	hasError := false

	// Iterate across requested URLs.
	// BHTTP INVARIANT: Reuses the single TCP connection; never opens a second connection!
	for _, target := range urls {
		hostPort, path, err := bhttp.ParseURL(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing target %q: %v\n", target, err)
			hasError = true
			continue
		}

		resp, err := client.Get(hostPort, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error requesting %s%s: %v\n", hostPort, path, err)
			hasError = true
			continue
		}

		// Body goes to stdout
		_, _ = os.Stdout.Write(resp.Body)

		// SPEC MANDATE: Exit non-zero on 4xx / 5xx
		if resp.StatusCode >= 400 {
			hasError = true
		}
	}

	if hasError {
		os.Exit(1)
	}
}
