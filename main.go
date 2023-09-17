package main

import (
	"bytes"
	"context"
	"encoding/json"
	sourceController "github.com/fluxcd/source-controller/api/v1beta2"
	"github.com/go-playground/validator/v10"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Name        string `json:"name" validate:"required"`
	Namespace   string `json:"namespace" validate:"required"`
	PackageType string `json:"package_type" validate:"required,eq=CONTAINER"`
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

func HandleContainerPushPayload(payload ContainerPushPayload) {
	log.Println("ContainerPushPayload")
	log.Println(PrettyEncode(payload))
}

func main() {
	client := getDynamicClient()

	//sc, _ := sourceController.SchemeBuilder.Build()
	//sc.

	restClient := getRestClient()

	var res sourceController.OCIRepositoryList
	err := restClient.Get().Resource("ocirepositories").Namespace("").Do(context.Background()).Into(&res)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(PrettyEncode(res))

	resource, _ := client.Resource(sourceController.GroupVersion.WithResource("ocirepositories")).Namespace("").List(context.Background(), metav1.ListOptions{})
	log.Println(PrettyEncode(resource))
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
			log.Println("ContainerPushPayload")
			log.Println(PrettyEncode(requestPayload.ContainerPushPayload))
		case validate.Struct(requestPayload.PingEventPayload) == nil:
			log.Println("PingEventPayload")
			log.Println(PrettyEncode(requestPayload.PingEventPayload))
		default:
			log.Println("Unknown payload")
		}
	})
	log.Fatal(http.ListenAndServe(":3400", nil))
}
