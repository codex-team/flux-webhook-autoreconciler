package main

import (
	"context"
	"encoding/json"
	"strings"

	fluxMeta "github.com/fluxcd/pkg/apis/meta"
	sourceController "github.com/fluxcd/source-controller/api/v1"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

// Annotator defines the interface for annotating Git and OCI repositories
type Annotator interface {
	AnnotateGitRepository(repository sourceController.GitRepository) error
	AnnotateOciRepository(repository sourceController.OCIRepository) error
}

// K8sAnnotator implements Annotator using Kubernetes REST API
type K8sAnnotator struct {
	restClient *rest.RESTClient
}

// NewK8sAnnotator creates a new K8sAnnotator
func NewK8sAnnotator(client *rest.RESTClient) *K8sAnnotator {
	return &K8sAnnotator{
		restClient: client,
	}
}

func (a *K8sAnnotator) AnnotateOciRepository(repository sourceController.OCIRepository) error {
	patch := struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}{}

	patch.Metadata.Annotations = make(map[string]string)

	patch.Metadata.Annotations[fluxMeta.ReconcileRequestAnnotation] = metav1.Now().String()

	patchJson, _ := json.Marshal(patch)

	var res sourceController.OCIRepository
	return a.restClient.
		Patch(types.MergePatchType).
		Resource("ocirepositories").
		Namespace(repository.Namespace).
		Name(repository.Name).
		Body(patchJson).
		Do(context.Background()).
		Into(&res)
}

func (a *K8sAnnotator) AnnotateGitRepository(repository sourceController.GitRepository) error {
	patch := struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}{}

	patch.Metadata.Annotations = make(map[string]string)

	patch.Metadata.Annotations[fluxMeta.ReconcileRequestAnnotation] = metav1.Now().String()

	patchJson, _ := json.Marshal(patch)

	var res sourceController.GitRepository
	return a.restClient.
		Patch(types.MergePatchType).
		Resource("gitrepositories").
		Namespace(repository.Namespace).
		Name(repository.Name).
		Body(patchJson).
		Do(context.Background()).
		Into(&res)
}

// RESTClientGetter defines the interface for getting resources from Kubernetes
type RESTClientGetter interface {
	Get() *rest.Request
}

type Reconciler struct {
	restClient RESTClientGetter
	annotator  Annotator
	logger     *zap.Logger
}

func NewReconciler(client *rest.RESTClient, logger *zap.Logger) *Reconciler {
	return &Reconciler{
		restClient: client,
		annotator:  NewK8sAnnotator(client),
		logger:     logger,
	}
}

// NewReconcilerWithAnnotator creates a Reconciler with a custom Annotator (useful for testing)
func NewReconcilerWithAnnotator(client RESTClientGetter, annotator Annotator, logger *zap.Logger) *Reconciler {
	return &Reconciler{
		restClient: client,
		annotator:  annotator,
		logger:     logger,
	}
}

func (r *Reconciler) ReconcileOciSources(ociUrl string, tag string) {
	var res sourceController.OCIRepositoryList
	err := r.restClient.Get().Resource("ocirepositories").Namespace("").Do(context.Background()).Into(&res)
	if err != nil {
		r.logger.Error("Failed to get OCIRepositories", zap.Error(err))
	}
	for _, ociRepository := range res.Items {
		if ociRepository.Spec.URL == ociUrl && ociRepository.Spec.Reference.Tag == tag {
			r.logger.Info("Reconciling OCIRepository", zap.String("name", ociRepository.Name), zap.String("namespace", ociRepository.Namespace))
			err := r.annotator.AnnotateOciRepository(ociRepository)
			if err != nil {
				r.logger.Error("Failed to annotate OCIRepository", zap.Error(err))
				reconciledCount.With(prometheus.Labels{"name": ociRepository.Name, "status": "fail", "namespace": ociRepository.Namespace}).Inc()
			}
			reconciledCount.With(prometheus.Labels{"name": ociRepository.Name, "status": "success", "namespace": ociRepository.Namespace}).Inc()
		}
	}
}

func (r *Reconciler) ReconcileGitRepositories(repoURL string, ref string) {
	var res sourceController.GitRepositoryList
	err := r.restClient.Get().Resource("gitrepositories").Namespace("").Do(context.Background()).Into(&res)
	if err != nil {
		r.logger.Error("Failed to get GitRepositories", zap.Error(err))
		return
	}

	normalizedTarget := normalizeGitURL(repoURL)
	if normalizedTarget == "" {
		return
	}

	branch := parseBranchFromRef(ref)

	for _, gitRepository := range res.Items {
		normURL := normalizeGitURL(gitRepository.Spec.URL)
		if normURL == "" {
			continue
		}

		if normURL != normalizedTarget {
			continue
		}

		// If GitRepository has a branch reference specified, reconcile only when it matches
		if gitRepository.Spec.Reference != nil && gitRepository.Spec.Reference.Branch != "" && branch != "" && gitRepository.Spec.Reference.Branch != branch {
			continue
		}

		r.logger.Info("Reconciling GitRepository",
			zap.String("name", gitRepository.Name),
			zap.String("namespace", gitRepository.Namespace),
			zap.String("url", gitRepository.Spec.URL),
			zap.String("ref", ref),
		)
		err := r.annotator.AnnotateGitRepository(gitRepository)
		if err != nil {
			r.logger.Error("Failed to annotate GitRepository", zap.Error(err))
			reconciledCount.With(prometheus.Labels{"name": gitRepository.Name, "status": "fail", "namespace": gitRepository.Namespace}).Inc()
		} else {
			reconciledCount.With(prometheus.Labels{"name": gitRepository.Name, "status": "success", "namespace": gitRepository.Namespace}).Inc()
		}
	}
}

func normalizeGitURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	raw = strings.TrimSuffix(raw, ".git")

	// SSH form: git@github.com:owner/repo
	if strings.HasPrefix(raw, "git@") {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			return strings.ToLower(raw)
		}
		host := strings.TrimPrefix(parts[0], "git@")
		path := parts[1]
		return strings.ToLower(host + "/" + strings.TrimPrefix(path, "/"))
	}

	// ssh://git@github.com/owner/repo
	if strings.HasPrefix(raw, "ssh://git@") {
		raw = strings.TrimPrefix(raw, "ssh://git@")
		parts := strings.SplitN(raw, "/", 2)
		if len(parts) != 2 {
			return strings.ToLower(raw)
		}
		host := parts[0]
		path := parts[1]
		return strings.ToLower(host + "/" + strings.TrimPrefix(path, "/"))
	}

	// Strip known URL schemes
	if idx := strings.Index(raw, "://"); idx != -1 {
		raw = raw[idx+3:]
	}

	return strings.ToLower(strings.TrimPrefix(raw, "/"))
}

func parseBranchFromRef(ref string) string {
	const headsPrefix = "refs/heads/"
	if strings.HasPrefix(ref, headsPrefix) {
		return ref[len(headsPrefix):]
	}
	return ""
}
