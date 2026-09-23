// Source for fixtures/edge-prebuilt-binary/server.
//
// The fixture is a repository that ships only a compiled binary and a Procfile,
// so the binary is built by `qa.py up` for the Docker host's architecture and
// is not committed.
package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "PANDO-QA edge-prebuilt-binary OK")
	})
	fmt.Println("listening on", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
