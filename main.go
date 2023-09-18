package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	fluxMeta "github.com/fluxcd/pkg/apis/meta"
	sourceController "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-playground/validator/v10"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"log"
	"net/http"
)

func PrettyEncode(data interface{}) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	//enc.SetIndent("", "    ")
	if err := enc.Encode(data); err != nil {
		log.Fatal(err)
	}
	return buf.String()
}

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

func reconcileSources(ociUrl string, tag string) {
	restClient := getRestClient()

	var res sourceController.OCIRepositoryList
	err := restClient.Get().Resource("ocirepositories").Namespace("").Do(context.Background()).Into(&res)
	if err != nil {
		log.Fatal(err)
	}
	for _, ociRepository := range res.Items {
		if ociRepository.Spec.URL == ociUrl && ociRepository.Spec.Reference.Tag == tag {
			log.Println("Reconciling", ociRepository.Name)
			annotateRepository(ociRepository)
		}
	}
}

func annotateRepository(repository sourceController.OCIRepository) {
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

func HandleContainerPushPayload(payload ContainerPushPayload) {
	tag := payload.RegistryPackage.PackageVersion.ContainerMetadata.Tag.Name
	ociUrl := fmt.Sprintf("oci://ghcr.io/%s/%s", payload.RegistryPackage.Namespace, payload.RegistryPackage.Name)
	log.Println("Handling", ociUrl, tag)
	reconcileSources(ociUrl, tag)
}

func main() {
	//client := getDynamicClient()
	//
	//restClient := getRestClient()
	//
	//var res sourceController.OCIRepositoryList
	//err := restClient.Get().Resource("ocirepositories").Namespace("").Do(context.Background()).Into(&res)
	//if err != nil {
	//	log.Fatal(err)
	//}
	//log.Println(PrettyEncode(res))
	//
	//resource, _ := client.Resource(sourceController.GroupVersion.WithResource("ocirepositories")).Namespace("").List(context.Background(), metav1.ListOptions{})
	//log.Println(PrettyEncode(resource))
	//log.Println(sourceController.OCIRepository{}.ResourceVersion)

	validate := validator.New(validator.WithRequiredStructEnabled())
	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
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
		case validate.Struct(requestPayload.ContainerPushPayload) == nil:
			HandleContainerPushPayload(requestPayload.ContainerPushPayload)
		case validate.Struct(requestPayload.PingEventPayload) == nil:
			log.Println("PingEventPayload")
			log.Println(PrettyEncode(requestPayload.PingEventPayload))
		default:
			log.Println("Unknown payload")
		}
	})
	log.Fatal(http.ListenAndServe(":3400", nil))
}
