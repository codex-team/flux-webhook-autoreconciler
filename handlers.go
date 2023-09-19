package main

import (
	"encoding/json"
	"fmt"
	"github.com/go-playground/validator/v10"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
)

type Client struct {
	connection *websocket.Conn
	send       chan string
}

type Handlers struct {
	reconciler *Reconciler
	validate   *validator.Validate
	upgrader   websocket.Upgrader
	clients    map[*Client]bool
}

func NewHandlers(reconciler *Reconciler) *Handlers {
	validate := validator.New(validator.WithRequiredStructEnabled())
	clients := make(map[*Client]bool)
	return &Handlers{reconciler: reconciler, validate: validate, upgrader: websocket.Upgrader{}, clients: clients}
}

func (s *Handlers) Subscribe(w http.ResponseWriter, r *http.Request) {
	c, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	log.Println("Handle new client")
	defer c.Close()

	sendChan := make(chan string, 5)
	client := &Client{connection: c, send: sendChan}
	s.RegisterClient(client)
	defer func() {
		s.UnregisterClient(client)
		log.Println("Unregistered client")
	}()
	log.Println("Registered client")

	for {
		message, more := <-client.send
		if !more {
			log.Println("Connection closed")
			break
		}

		err := c.WriteMessage(websocket.TextMessage, []byte(message))

		if err != nil {
			log.Println("write:", err)
			break
		}
		log.Println("Message sent")
	}
}

func (s *Handlers) HandleContainerPushPayload(payload ContainerPushPayload) {
	tag := payload.RegistryPackage.PackageVersion.ContainerMetadata.Tag.Name
	ociUrl := fmt.Sprintf("oci://ghcr.io/%s/%s", payload.RegistryPackage.Namespace, payload.RegistryPackage.Name)
	log.Println("Handling", ociUrl, tag)

	for client := range s.clients {
		client.send <- fmt.Sprintf("%s:%s", ociUrl, tag)
		log.Println("Sent to client")
	}

	s.reconciler.ReconcileSources(ociUrl, tag)
}

func (s *Handlers) Webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var requestPayload ExpectedPayload

	err := json.NewDecoder(r.Body).Decode(&requestPayload)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	log.Println(PrettyEncode(requestPayload))

	switch {
	case s.validate.Struct(requestPayload.ContainerPushPayload) == nil:
		s.HandleContainerPushPayload(requestPayload.ContainerPushPayload)
	case s.validate.Struct(requestPayload.PingEventPayload) == nil:
		log.Println("PingEventPayload")
		log.Println(PrettyEncode(requestPayload.PingEventPayload))
	default:
		log.Println("Unknown payload")
	}
}

func (s *Handlers) RegisterClient(client *Client) {
	s.clients[client] = true
}

func (s *Handlers) UnregisterClient(client *Client) {
	close(client.send)
	delete(s.clients, client)
}
