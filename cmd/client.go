package main

import (
	"context"
	"encoding/json"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
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

func (r *Client) Run(ctx context.Context) {
	for r.retry < maxRetries {
		r.logger.Info("Connecting to server")

		connectionAttempts.Inc()
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

		doneChan := make(chan struct{})

		go func() {
			defer close(doneChan)
			for {
				messageType, message, err := c.ReadMessage()
				if err != nil {
					r.logger.Error("Error reading message", zap.Error(err))
					processedMessages.With(prometheus.Labels{"status": "fail"}).Inc()
					break
				}

				if messageType != websocket.BinaryMessage {
					r.logger.Info("Received non-binary message", zap.String("message", string(message)))
					processedMessages.With(prometheus.Labels{"status": "fail"}).Inc()
					continue
				}

				var payload SubscribeEventPayload
				err = json.Unmarshal(message, &payload)
				if err != nil {
					r.logger.Error("Error unmarshalling message", zap.Error(err))
					processedMessages.With(prometheus.Labels{"status": "fail"}).Inc()
					break
				}

				r.logger.Info("Received message",
					zap.String("ociUrl", payload.OciUrl),
					zap.String("tag", payload.Tag),
					zap.String("gitRepo", payload.GitRepo),
					zap.String("ref", payload.Ref),
				)

				// Reconcile OCI repositories when OCI info is present
				if payload.OciUrl != "" && payload.Tag != "" {
					r.reconciler.ReconcileOciSources(payload.OciUrl, payload.Tag)
				}

				// Reconcile GitRepository resources when git info is present
				if payload.GitRepo != "" || payload.Ref != "" {
					// For client-originated git events we only have the repo full name and ref;
					// ReconcileGitRepositories will typically be driven from the server side,
					// but we still call it here to keep behavior consistent across modes.
					// Note: client mode doesn't have the full repo URL, so this will only work
					// if GitRepository resources match by ref/branch filtering
					if payload.GitRepo != "" {
						r.reconciler.ReconcileGitRepositories(payload.GitRepo, payload.Ref)
					}
				}
				processedMessages.With(prometheus.Labels{"status": "success"}).Inc()
			}
		}()

		select {
		case <-ctx.Done():
			r.logger.Debug("Context done, exiting client")
			return
		case <-doneChan:
			r.logger.Debug("Client done, retrying connection")
			continue
		}
	}
}
