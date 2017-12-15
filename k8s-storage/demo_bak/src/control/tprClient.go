package main

import (
	"k8s.io/client-go/pkg/api"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/runtime"
	"k8s.io/client-go/pkg/runtime/schema"
	"k8s.io/client-go/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
)

type podToServiceClient struct {
	rest *rest.RESTClient
}

func (c *podToServiceClient) Create(body *PodToService) (*PodToService, error) {
	var ret PodToService
	err := c.rest.Post().
		Resource("podtoservices").
		Namespace(api.NamespaceDefault).
		Body(body).
		Do().Into(&ret)
	return &ret, err
}

func (c *podToServiceClient) Update(body *PodToService) (*PodToService, error) {
	var ret PodToService
	err := c.rest.Put().
		Resource("podtoservices").
		Namespace(api.NamespaceDefault).
		Name(body.Metadata.Name).
		Body(body).
		Do().Into(&ret)
	return &ret, err
}

func (c *podToServiceClient) Get(name string) (*PodToService, error) {
	var ret PodToService
	err := c.rest.Get().
		Resource("podtoservices").
		Namespace(api.NamespaceDefault).
		Name(name).
		Do().Into(&ret)
	return &ret, err
}

func configureClient(config *rest.Config) {
	groupversion := schema.GroupVersion{
		Group:   "caesarxuchao.io",
		Version: "v1",
	}

	config.GroupVersion = &groupversion
	config.APIPath = "/apis"
	// Currently TPR only supports JSON
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: api.Codecs}

	schemeBuilder := runtime.NewSchemeBuilder(
		func(scheme *runtime.Scheme) error {
			scheme.AddKnownTypes(
				groupversion,
				&PodToService{},
				&PodToServiceList{},
				&v1.ListOptions{},
				&v1.DeleteOptions{},
			)
			return nil
		})
	schemeBuilder.AddToScheme(api.Scheme)
}

func getTPRClientOrDie(config *rest.Config) *podToServiceClient {
	configureClient(config)
	restClient, err := rest.RESTClientFor(config)
	if err != nil {
		panic(err)
	}
	return &podToServiceClient{restClient}
}
