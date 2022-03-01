package main

import (
	"io"
	"net/http"

	"github.com/a-h/awsapigatewayv2handler"
)

func main() {
	http.HandleFunc("/500", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "500 error", http.StatusInternalServerError)
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "Hello")
	})

	awsapigatewayv2handler.ListenAndServe(http.DefaultServeMux)
}
