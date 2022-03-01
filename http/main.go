package main

import (
	"io"
	"net/http"

	"github.com/akrylysov/algnhsa"
)

func main() {
	http.HandleFunc("/500", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "500 error", http.StatusInternalServerError)
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "Hello")
	})

	algnhsa.ListenAndServe(http.DefaultServeMux, nil)
}
