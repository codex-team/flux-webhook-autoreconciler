package main

import (
	"encoding/json"
	"github.com/go-playground/validator/v10"
	"github.com/gorilla/websocket"
	"k8s.io/client-go/rest"
	"log"
	"net/http"
)

type Client struct {
	connection *websocket.Conn
}

type Handlers struct {
	k8sClient *rest.RESTClient
	validate  *validator.Validate
	upgrader  websocket.Upgrader
	clients   map[*Client]bool
}

func NewHandlers(k8sClient *rest.RESTClient) *Handlers {
	validate := validator.New(validator.WithRequiredStructEnabled())
	return &Handlers{k8sClient: k8sClient, validate: validate, upgrader: websocket.Upgrader{}}
}

func (s *Handlers) Subscribe(w http.ResponseWriter, r *http.Request) {
	c, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()

	client := &Client{connection: c}
	s.RegisterClient(client)

	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		log.Printf("recv: %s", message)
		err = c.WriteMessage(mt, message)
		if err != nil {
			log.Println("write:", err)
			break
		}
	}
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
		HandleContainerPushPayload(requestPayload.ContainerPushPayload)
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
	delete(s.clients, client)
}
