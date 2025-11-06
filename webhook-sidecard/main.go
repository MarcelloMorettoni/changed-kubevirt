package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
)

func main() {
	var (
		addr      = flag.String("addr", ":8443", "address to listen on")
		tlsCert   = flag.String("tls-cert", os.Getenv("TLS_CERT_FILE"), "path to TLS certificate")
		tlsKey    = flag.String("tls-key", os.Getenv("TLS_KEY_FILE"), "path to TLS private key")
		allowHTTP = flag.Bool("allow-http", false, "allow serving webhook over plain HTTP (for development only)")
	)
	flag.Parse()

	mutator := &podSecurityMutator{}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/mutate", func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			http.Error(w, "empty request", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var review AdmissionReview
		if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
			log.Printf("failed to decode admission review: %v", err)
			http.Error(w, "invalid admission review", http.StatusBadRequest)
			return
		}

		response := mutator.Handle(review.Request)
		if response == nil {
			response = &AdmissionResponse{Allowed: true}
		}
		if review.Request != nil {
			response.UID = review.Request.UID
		}

		responseReview := AdmissionReview{
			APIVersion: review.APIVersion,
			Kind:       review.Kind,
			Response:   response,
		}

		writeResponse(w, &responseReview)
	})

	server := &http.Server{
		Addr:    *addr,
		Handler: loggingMiddleware(mux),
	}

	if *allowHTTP {
		log.Printf("starting webhook server on %s without TLS (development only)", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server exited with error: %v", err)
		}
		return
	}

	if *tlsCert == "" || *tlsKey == "" {
		log.Fatal("TLS is required (provide --tls-cert and --tls-key or set TLS_CERT_FILE/TLS_KEY_FILE)")
	}

	certificate, err := tls.LoadX509KeyPair(*tlsCert, *tlsKey)
	if err != nil {
		log.Fatalf("failed to load TLS certificate: %v", err)
	}

	server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{certificate}}

	log.Printf("starting webhook server on %s", *addr)
	if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server exited with error: %v", err)
	}
}

func writeResponse(w http.ResponseWriter, review *AdmissionReview) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(review); err != nil {
		log.Printf("failed to encode admission review response: %v", err)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
