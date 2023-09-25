package main

import (
	"context"
	"encoding/json"
	fluxMeta "github.com/fluxcd/pkg/apis/meta"
	sourceController "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

type Reconciler struct {
	restClient *rest.RESTClient
	logger     *zap.Logger
}

func NewReconciler(client *rest.RESTClient, logger *zap.Logger) *Reconciler {
	return &Reconciler{
		restClient: client,
		logger:     logger,
	}
}

func (r *Reconciler) ReconcileSources(ociUrl string, tag string) {
	var res sourceController.OCIRepositoryList
	err := r.restClient.Get().Resource("ocirepositories").Namespace("").Do(context.Background()).Into(&res)
	if err != nil {
		r.logger.Error("Failed to get OCIRepositories", zap.Error(err))
	}
	for _, ociRepository := range res.Items {
		if ociRepository.Spec.URL == ociUrl && ociRepository.Spec.Reference.Tag == tag {
			r.logger.Info("Reconciling OCIRepository", zap.String("name", ociRepository.Name))
			err := r.annotateRepository(ociRepository)
			if err != nil {
				r.logger.Error("Failed to annotate OCIRepository", zap.Error(err))
				reconciledCount.With(prometheus.Labels{"source": ociRepository.Name, "status": "fail"}).Inc()
			}
			reconciledCount.With(prometheus.Labels{"source": ociRepository.Name, "status": "success"}).Inc()
		}
	}
}

func (r *Reconciler) annotateRepository(repository sourceController.OCIRepository) error {
	patch := struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}{}

	patch.Metadata.Annotations = make(map[string]string)

	patch.Metadata.Annotations[fluxMeta.ReconcileRequestAnnotation] = metav1.Now().String()

	patchJson, _ := json.Marshal(patch)

	var res sourceController.OCIRepository
	return r.restClient.
		Patch(types.MergePatchType).
		Resource("ocirepositories").
		Namespace(repository.Namespace).
		Name(repository.Name).
		Body(patchJson).
		Do(context.Background()).
		Into(&res)

}
