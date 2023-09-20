package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
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

func setupServer() {
	reconciler := NewReconciler()
	handlers := NewHandlers(reconciler)
	http.HandleFunc("/webhook", handlers.Webhook)
	http.HandleFunc("/subscribe", handlers.Subscribe)
}

func setupClient(config Config) {
	log.Println("Starting client")
	reconciler := NewReconciler()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	u, err := url.Parse(config.ServerEndpoint)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}

	done := make(chan struct{})

	go func() {
		defer close(done)
		defer c.Close()
		for {
			messageType, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}

			if messageType != websocket.BinaryMessage {
				log.Println("Not binary message")
				return
			}

			var payload SubscribeEventPayload
			err = json.Unmarshal(message, &payload)
			if err != nil {
				log.Println("unmarshal:", err)
				return
			}

			reconciler.ReconcileSources(payload.OciUrl, payload.Tag)

			log.Printf("recv: %s", message)
		}
	}()
	//
	//ticker := time.NewTicker(time.Second)
	//defer ticker.Stop()
	//
	//for {
	//	select {
	//	case <-done:
	//		return
	//	//case t := <-ticker.C:
	//	//	err := c.WriteMessage(websocket.TextMessage, []byte(t.String()))
	//	//	if err != nil {
	//	//		log.Println("write:", err)
	//	//		return
	//	//	}
	//	case <-interrupt:
	//		log.Println("interrupt")
	//
	//		// Cleanly close the connection by sending a close message and then
	//		// waiting (with timeout) for the server to close the connection.
	//		err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	//		if err != nil {
	//			log.Println("write close:", err)
	//			return
	//		}
	//		select {
	//		case <-done:
	//		case <-time.After(time.Second):
	//		}
	//		return
	//	}
	//}
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

	if config.Mode == "server" {
		setupServer()
	} else {
		setupClient(config)
	}

	addr := fmt.Sprintf("%s:%s", config.Host, config.Port)
	log.Println("Starting server on address", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
