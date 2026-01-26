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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// Annotator defines the interface for annotating Git and OCI repositories
type Annotator interface {
	AnnotateGitRepository(repository sourceController.GitRepository) error
	AnnotateOciRepository(repository sourceController.OCIRepository) error
}

var (
	gitRepositoryGVR = schema.GroupVersionResource{
		Group:    "source.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "gitrepositories",
	}
	ociRepositoryGVR = schema.GroupVersionResource{
		Group:    "source.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "ocirepositories",
	}
)

// K8sAnnotator implements Annotator using Kubernetes Dynamic Client
type K8sAnnotator struct {
	dynamicClient dynamic.Interface
}

// NewK8sAnnotator creates a new K8sAnnotator
func NewK8sAnnotator(client dynamic.Interface) *K8sAnnotator {
	return &K8sAnnotator{
		dynamicClient: client,
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

	patchJson, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	_, err = a.dynamicClient.Resource(ociRepositoryGVR).
		Namespace(repository.Namespace).
		Patch(context.Background(), repository.Name, types.MergePatchType, patchJson, metav1.PatchOptions{})

	return err
}

func (a *K8sAnnotator) AnnotateGitRepository(repository sourceController.GitRepository) error {
	patch := struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}{}

	patch.Metadata.Annotations = make(map[string]string)
	patch.Metadata.Annotations[fluxMeta.ReconcileRequestAnnotation] = metav1.Now().String()

	patchJson, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	_, err = a.dynamicClient.Resource(gitRepositoryGVR).
		Namespace(repository.Namespace).
		Patch(context.Background(), repository.Name, types.MergePatchType, patchJson, metav1.PatchOptions{})

	return err
}

type Reconciler struct {
	dynamicClient dynamic.Interface
	annotator     Annotator
	logger        *zap.Logger
}

func NewReconciler(client dynamic.Interface, logger *zap.Logger) *Reconciler {
	return &Reconciler{
		dynamicClient: client,
		annotator:     NewK8sAnnotator(client),
		logger:        logger,
	}
}

// NewReconcilerWithAnnotator creates a Reconciler with a custom Annotator (useful for testing)
func NewReconcilerWithAnnotator(client dynamic.Interface, annotator Annotator, logger *zap.Logger) *Reconciler {
	return &Reconciler{
		dynamicClient: client,
		annotator:     annotator,
		logger:        logger,
	}
}

func (r *Reconciler) ReconcileOciSources(ociUrl string, tag string) {
	unstructuredList, err := r.dynamicClient.Resource(ociRepositoryGVR).
		Namespace("").
		List(context.Background(), metav1.ListOptions{})
	if err != nil {
		r.logger.Error("Failed to get OCIRepositories", zap.Error(err))
		return
	}

	for _, item := range unstructuredList.Items {
		var ociRepository sourceController.OCIRepository
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &ociRepository)
		if err != nil {
			r.logger.Error("Failed to convert unstructured to OCIRepository", zap.Error(err))
			continue
		}

		if ociRepository.Spec.URL == ociUrl && ociRepository.Spec.Reference != nil && ociRepository.Spec.Reference.Tag == tag {
			r.logger.Info("Reconciling OCIRepository", zap.String("name", ociRepository.Name), zap.String("namespace", ociRepository.Namespace))
			err := r.annotator.AnnotateOciRepository(ociRepository)
			if err != nil {
				r.logger.Error("Failed to annotate OCIRepository", zap.Error(err))
				reconciledCount.With(prometheus.Labels{"name": ociRepository.Name, "status": "fail", "namespace": ociRepository.Namespace}).Inc()
			} else {
				reconciledCount.With(prometheus.Labels{"name": ociRepository.Name, "status": "success", "namespace": ociRepository.Namespace}).Inc()
			}
		}
	}
}

func (r *Reconciler) ReconcileGitRepositories(repoURL string, ref string) {
	unstructuredList, err := r.dynamicClient.Resource(gitRepositoryGVR).
		Namespace("").
		List(context.Background(), metav1.ListOptions{})
	if err != nil {
		r.logger.Error("Failed to get GitRepositories", zap.Error(err))
		return
	}

	normalizedTarget := normalizeGitURL(repoURL)
	if normalizedTarget == "" {
		return
	}

	branch := parseBranchFromRef(ref)

	for _, item := range unstructuredList.Items {
		var gitRepository sourceController.GitRepository
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &gitRepository)
		if err != nil {
			r.logger.Error("Failed to convert unstructured to GitRepository", zap.Error(err))
			continue
		}

		normURL := normalizeGitURL(gitRepository.Spec.URL)
		if normURL == "" {
			continue
		}

		if normURL != normalizedTarget {
			continue
		}

		// Get repository branch (defaults to "master" per Flux docs)
		repoBranch := "master"
		if gitRepository.Spec.Reference != nil && gitRepository.Spec.Reference.Branch != "" {
			repoBranch = gitRepository.Spec.Reference.Branch
		}

		// Reconcile only when branches match
		if repoBranch != branch {
			continue
		}

		r.logger.Info("Reconciling GitRepository",
			zap.String("name", gitRepository.Name),
			zap.String("namespace", gitRepository.Namespace),
			zap.String("url", gitRepository.Spec.URL),
			zap.String("ref", ref),
		)
		err = r.annotator.AnnotateGitRepository(gitRepository)
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
