package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	"net/url"
	"time"
)

func PrettyEncode(data interface{}) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	//enc.SetIndent("", "    ")
	if err := enc.Encode(data); err != nil {
		log.Fatal(err)
	}
	return buf.String()
}

func setupServer(config Config) {
	reconciler := NewReconciler()
	handlers := NewHandlers(config, reconciler)
	http.HandleFunc("/webhook", handlers.Webhook)
	http.HandleFunc("/subscribe", handlers.Subscribe)
}

func setupClient(config Config, shutdownChan chan struct{}) {
	log.Println("Starting client")
	reconciler := NewReconciler()

	u, err := url.Parse(config.ServerEndpoint)
	query := u.Query()
	query.Set("authSecret", config.SubscribeSecret)
	u.RawQuery = query.Encode()

	if err != nil {
		log.Fatal(err)
	}

	// Number of retry attempts
	maxRetries := 5

	// Time delay between retries
	retryDelay := time.Second * 5

	retry := 0

	go func() {
		for retry < maxRetries {
			log.Printf("connecting to %s", config.ServerEndpoint)

			c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
			if err != nil {
				time.Sleep(retryDelay)
				retry++
				log.Println("dial:", err)
				continue
			}
			defer c.Close()
			log.Println("connected to", config.ServerEndpoint)

			retry = 0

			for {
				messageType, message, err := c.ReadMessage()
				if err != nil {
					log.Println("read:", err)
					break
				}

				if messageType != websocket.BinaryMessage {
					log.Println("Not binary message")
					break
				}

				var payload SubscribeEventPayload
				err = json.Unmarshal(message, &payload)
				if err != nil {
					log.Println("unmarshal:", err)
					break
				}

				reconciler.ReconcileSources(payload.OciUrl, payload.Tag)

				log.Printf("recv: %s", message)
			}

		}
		shutdownChan <- struct{}{}
	}()

}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "Path to config file")
	flag.Parse()

	config, err := LoadConfig(configPath)

	if err != nil {
		log.Fatal(err)
	}

	http.Handle("/metrics", promhttp.Handler())

	shutdownChan := make(chan struct{})

	if config.Mode == "server" {
		setupServer(config)
	} else {
		setupClient(config, shutdownChan)
	}

	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)

	server := &http.Server{Addr: addr}

	go func() {
		log.Println("Starting server on address", addr)
		log.Fatal(server.ListenAndServe())
	}()
	select {
	case <-shutdownChan:
		log.Println("Shutting down server")
		// Received a shutdown signal, so shut down the server gracefully
		if err := server.Shutdown(context.Background()); err != nil {
			log.Fatal(err)
		}
	}
}
