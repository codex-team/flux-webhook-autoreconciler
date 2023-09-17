package main

import (
	sourceController "github.com/fluxcd/source-controller/api/v1beta2"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"os"
	"path/filepath"
)

func getConfig() *rest.Config {
	// Try in-cluster configuration
	config, err := rest.InClusterConfig()
	if err == nil {
		return config
	}

	// Fallback to kubeconfig
	kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatal(err)
	}

	schema := scheme.Scheme
	err = sourceController.AddToScheme(schema)
	if err != nil {
		log.Fatal(err)
	}

	config.GroupVersion = &sourceController.GroupVersion
	config.APIPath = "/apis"

	config.NegotiatedSerializer = serializer.NewCodecFactory(schema)
	//config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme)}

	return config
}

func getClient() *kubernetes.Clientset {
	config := getConfig()
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	return client
}

func getRestClient() *rest.RESTClient {
	config := getConfig()
	client, err := rest.RESTClientFor(config)
	if err != nil {
		log.Fatal(err)
	}

	return client
}

func getDynamicClient() *dynamic.DynamicClient {
	config := getConfig()
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}
	return client
}
