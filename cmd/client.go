package main

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"net/url"
	"time"
)

const (
	// Number of retry attempts
	maxRetries = 5

	// Time delay between retries
	retryDelay = time.Second * 5
)

type Client struct {
	serverEndpoint *url.URL
	logger         *zap.Logger
	reconciler     *Reconciler
	retry          int
}

func NewClient(serverEndpoint *url.URL, reconciler *Reconciler, logger *zap.Logger) *Client {
	return &Client{
		serverEndpoint: serverEndpoint,
		reconciler:     reconciler,
		logger:         logger,
	}
}

func (r *Client) Run() {
	for r.retry < maxRetries {
		r.logger.Info("Connecting to server")

		c, _, err := websocket.DefaultDialer.Dial(r.serverEndpoint.String(), nil)
		if err != nil {
			time.Sleep(retryDelay)
			r.retry++
			r.logger.Error("Failed to connect to server", zap.Error(err), zap.Int("retry", r.retry))
			continue
		}
		defer c.Close()
		r.logger.Info("Connected to server")

		r.retry = 0

		for {
			messageType, message, err := c.ReadMessage()
			if err != nil {
				r.logger.Error("Error reading message", zap.Error(err))
				break
			}

			if messageType != websocket.BinaryMessage {
				r.logger.Info("Received non-binary message", zap.String("message", string(message)))
				continue
			}

			var payload SubscribeEventPayload
			err = json.Unmarshal(message, &payload)
			if err != nil {
				r.logger.Error("Error unmarshalling message", zap.Error(err))
				break
			}

			r.logger.Info("Received message", zap.String("ociUrl", payload.OciUrl), zap.String("tag", payload.Tag))
			r.reconciler.ReconcileSources(payload.OciUrl, payload.Tag)
		}

	}
}
