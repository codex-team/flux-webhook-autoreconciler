package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// Time allowed to read the next pong message from the peer.
	pongWait = 10 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
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

type Subscriber struct {
	id         string
	connection *websocket.Conn
	send       chan SubscribeEventPayload
}

type Handlers struct {
	config      Config
	reconciler  *Reconciler
	validate    *validator.Validate
	upgrader    websocket.Upgrader
	logger      *zap.Logger
	subscribers map[*Subscriber]bool
	m           sync.Mutex
}

func NewHandlers(config Config, reconciler *Reconciler, logger *zap.Logger) *Handlers {
	validate := validator.New(validator.WithRequiredStructEnabled())
	subscribers := make(map[*Subscriber]bool)
	return &Handlers{
		config:      config,
		reconciler:  reconciler,
		validate:    validate,
		upgrader:    websocket.Upgrader{},
		subscribers: subscribers,
		logger:      logger,
	}
}

func (s *Handlers) Subscribe(w http.ResponseWriter, r *http.Request) {
	clientUuid := uuid.New()
	clientId := clientUuid.String()
	s.logger.Info("Handling new subscription", zap.String("clientId", clientId))

	if s.config.SubscribeSecret != "" {
		authSecret := r.URL.Query().Get("authSecret")
		if authSecret != s.config.SubscribeSecret {
			s.logger.Info("Invalid auth secret from subscr", zap.String("clientId", clientId))
			http.Error(w, "Invalid auth secret", http.StatusUnauthorized)
			return
		}
	}

	c, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("Connection upgrading error", zap.Error(err), zap.String("clientId", clientId))
		return
	}
	defer c.Close()

	sendChan := make(chan SubscribeEventPayload)
	subscr := &Subscriber{connection: c, send: sendChan, id: clientId}
	s.RegisterClient(subscr)
	defer func() {
		s.UnregisterClient(subscr)
		s.logger.Info("Unregistered subscr", zap.String("clientId", clientId))
	}()
	s.logger.Info("Registered subscr", zap.String("clientId", clientId))

	if err := c.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		s.logger.Error("SetReadDeadline error", zap.Error(err), zap.String("clientId", clientId))
		return
	}

	c.SetPongHandler(func(pongMsg string) error {
		return c.SetReadDeadline(time.Now().Add(pongWait))
	})

	ticker := time.NewTicker(pingPeriod)

	for {
		select {
		case message, more := <-subscr.send:
			if !more {
				s.logger.Info("Subscriber send channel closed", zap.String("clientId", clientId))
				break
			}

			buff, err := json.Marshal(message)
			if err != nil {
				s.logger.Error("Error marshalling message", zap.Error(err), zap.String("clientId", clientId))
				break
			}

			err = c.WriteMessage(websocket.BinaryMessage, buff)

			if err != nil {
				s.logger.Error("Error writing message", zap.Error(err), zap.String("clientId", clientId))
				break
			}
			s.logger.Info("Sent message", zap.String("clientId", clientId))
		case <-ticker.C:
			if err := c.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				s.logger.Error("Error writing ping message", zap.Error(err), zap.String("clientId", clientId))
				return
			}
		}
	}
}

func (s *Handlers) HandleContainerPushPayload(payload ContainerPushPayload) {
	tag := payload.RegistryPackage.PackageVersion.ContainerMetadata.Tag.Name
	ociUrl := fmt.Sprintf("oci://ghcr.io/%s/%s", payload.RegistryPackage.Namespace, payload.RegistryPackage.Name)

	for subscr := range s.subscribers {
		payload := SubscribeEventPayload{OciUrl: ociUrl, Tag: tag}
		subscr.send <- payload
	}

	s.reconciler.ReconcileSources(ociUrl, tag)
}

func (s *Handlers) Webhook(w http.ResponseWriter, r *http.Request) {
	s.logger.Info("Handling webhook", zap.String("method", r.Method), zap.String("path", r.URL.Path))
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		webhooksHandled.With(prometheus.Labels{"status": "fail"}).Inc()
		return
	}

	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Info("Error reading request body", zap.Error(err))
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		webhooksHandled.With(prometheus.Labels{"status": "fail"}).Inc()
		return
	}

	if s.config.GithubSecret != "" {
		// Get the GitHub signature from the request headers
		githubSignature := r.Header.Get("X-Hub-Signature-256")

		// Verify the signature
		if !verifySignature(githubSignature, body, []byte(s.config.GithubSecret)) {
			s.logger.Info("Signature verification failed")
			http.Error(w, "Signature verification failed", http.StatusUnauthorized)
			webhooksHandled.With(prometheus.Labels{"status": "fail"}).Inc()
			return
		}
	}

	var requestPayload ExpectedPayload

	err = json.Unmarshal(body, &requestPayload)
	if err != nil {
		s.logger.Info("Error unmarshalling request body", zap.Error(err))
		w.WriteHeader(http.StatusBadRequest)
		webhooksHandled.With(prometheus.Labels{"status": "fail"}).Inc()
		return
	}

	switch {
	case s.validate.Struct(requestPayload.ContainerPushPayload) == nil:
		s.HandleContainerPushPayload(requestPayload.ContainerPushPayload)
	case s.validate.Struct(requestPayload.PingEventPayload) == nil:
	default:
	}
	webhooksHandled.With(prometheus.Labels{"status": "success"}).Inc()
}

func (s *Handlers) RegisterClient(subscr *Subscriber) {
	s.m.Lock()
	defer s.m.Unlock()

	s.subscribers[subscr] = true
	clientsConnected.Inc()
}

func (s *Handlers) UnregisterClient(subscr *Subscriber) {
	s.m.Lock()
	defer s.m.Unlock()

	close(subscr.send)
	delete(s.subscribers, subscr)
	clientsConnected.Dec()
}

func verifySignature(signatureHeader string, payload []byte, secret []byte) bool {
	// GitHub sends the signature in the format "sha256=XXXXX..."
	parts := strings.SplitN(signatureHeader, "=", 2)
	if len(parts) != 2 || parts[0] != "sha256" {
		return false
	}

	// Calculate the HMAC
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	expectedMAC := mac.Sum(nil)

	// Decode the provided signature
	providedMAC, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}

	// Compare the calculated HMAC with the provided HMAC
	return hmac.Equal(providedMAC, expectedMAC)
}
