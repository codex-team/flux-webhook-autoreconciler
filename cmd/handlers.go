package main

import (
	"encoding/json"
	"fmt"
	"github.com/go-playground/validator/v10"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"time"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 10 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

type RegistryPackagePayload struct {
	Name           string `json:"name" validate:"required"`
	Namespace      string `json:"namespace" validate:"required"`
	PackageType    string `json:"package_type" validate:"required,eq=CONTAINER"`
	PackageVersion struct {
		ContainerMetadata struct {
			Tag struct {
				Name string `json:"name" validate:"required"`
			} `json:"tag" validate:"required"`
		} `json:"container_metadata" validate:"required"`
	} `json:"package_version" validate:"required"`
}

type ExpectedPayload struct {
	ContainerPushPayload
	PingEventPayload
}

type PingEventPayload struct {
	HookId uint32 `json:"hook_id" validate:"required"`
}

type ContainerPushPayload struct {
	Action          string                 `json:"action" validate:"required,eq=published"`
	RegistryPackage RegistryPackagePayload `json:"registry_package"`
}

type SubscribeEventPayload struct {
	OciUrl string `json:"oci_url"`
	Tag    string `json:"tag"`
}

type Client struct {
	connection *websocket.Conn
	send       chan SubscribeEventPayload
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

	sendChan := make(chan SubscribeEventPayload)
	client := &Client{connection: c, send: sendChan}
	s.RegisterClient(client)
	defer func() {
		s.UnregisterClient(client)
		log.Println("Unregistered client")
	}()
	log.Println("Registered client")

	if err := c.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		log.Println(err)
		return
	}

	c.SetPongHandler(func(pongMsg string) error {
		// Current time + Pong Wait time
		log.Println("pong")
		return c.SetReadDeadline(time.Now().Add(pongWait))
	})

	ticker := time.NewTicker(pingPeriod)

	for {
		select {
		case message, more := <-client.send:
			if !more {
				log.Println("Connection closed")
				break
			}

			buff, err := json.Marshal(message)
			if err != nil {
				log.Println("marshal:", err)
				break
			}

			err = c.WriteMessage(websocket.BinaryMessage, buff)

			if err != nil {
				log.Println("write:", err)
				break
			}
			log.Println("Message sent")
		case <-ticker.C:
			log.Println("ping")
			if err := c.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				log.Println("ping:", err)
				return
			}
		}
	}
}

func (s *Handlers) HandleContainerPushPayload(payload ContainerPushPayload) {
	tag := payload.RegistryPackage.PackageVersion.ContainerMetadata.Tag.Name
	ociUrl := fmt.Sprintf("oci://ghcr.io/%s/%s", payload.RegistryPackage.Namespace, payload.RegistryPackage.Name)
	log.Println("Handling", ociUrl, tag)

	for client := range s.clients {
		payload := SubscribeEventPayload{OciUrl: ociUrl, Tag: tag}
		client.send <- payload
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
	clientsConnected.Inc()
}

func (s *Handlers) UnregisterClient(client *Client) {
	close(client.send)
	delete(s.clients, client)
	clientsConnected.Dec()
}
