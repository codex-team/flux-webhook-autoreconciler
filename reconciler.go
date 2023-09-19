package main

import (
	"context"
	"encoding/json"
	fluxMeta "github.com/fluxcd/pkg/apis/meta"
	sourceController "github.com/fluxcd/source-controller/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"log"
)

type Reconciler struct {
	restClient *rest.RESTClient
}

func NewReconciler() *Reconciler {
	return &Reconciler{
		restClient: getRestClient(),
	}
}

func (r *Reconciler) ReconcileSources(ociUrl string, tag string) {
	restClient := getRestClient()

	var res sourceController.OCIRepositoryList
	err := restClient.Get().Resource("ocirepositories").Namespace("").Do(context.Background()).Into(&res)
	if err != nil {
		log.Fatal(err)
	}
	for _, ociRepository := range res.Items {
		if ociRepository.Spec.URL == ociUrl && ociRepository.Spec.Reference.Tag == tag {
			log.Println("Reconciling", ociRepository.Name)
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
		log.Fatal(err)
	}
}
