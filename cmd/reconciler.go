package main

import (
	"context"
	"encoding/json"
	fluxMeta "github.com/fluxcd/pkg/apis/meta"
	sourceController "github.com/fluxcd/source-controller/api/v1beta2"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

type Reconciler struct {
	restClient *rest.RESTClient
	logger     *zap.Logger
}

func NewReconciler(logger *zap.Logger) *Reconciler {
	return &Reconciler{
		restClient: getRestClient(),
		logger:     logger,
	}
}

func (r *Reconciler) ReconcileSources(ociUrl string, tag string) {
	restClient := getRestClient()

	var res sourceController.OCIRepositoryList
	err := restClient.Get().Resource("ocirepositories").Namespace("").Do(context.Background()).Into(&res)
	if err != nil {
		r.logger.Fatal("Failed to get OCIRepositories", zap.Error(err))
	}
	for _, ociRepository := range res.Items {
		if ociRepository.Spec.URL == ociUrl && ociRepository.Spec.Reference.Tag == tag {
			r.logger.Info("Reconciling OCIRepository", zap.String("name", ociRepository.Name))
			r.annotateRepository(ociRepository)
		}
	}
}

func (r *Reconciler) annotateRepository(repository sourceController.OCIRepository) {
	restClient := getRestClient()

	patch := struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}{}

	patch.Metadata.Annotations = make(map[string]string)

	patch.Metadata.Annotations[fluxMeta.ReconcileRequestAnnotation] = metav1.Now().String()

	patchJson, _ := json.Marshal(patch)

	var res sourceController.OCIRepository
	err := restClient.
		Patch(types.MergePatchType).
		Resource("ocirepositories").
		Namespace(repository.Namespace).
		Name(repository.Name).
		Body(patchJson).
		Do(context.Background()).
		Into(&res)
	if err != nil {
		r.logger.Fatal("Failed to patch OCIRepository", zap.String("name", repository.Name), zap.Error(err))
	}
}
