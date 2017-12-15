package client

import (
	disktype "resource/disk/types"
	"util"

	"github.com/golang/glog"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
)

func NewClient(extensionsClient apiextensionsclient.Interface, kubeConfig string) (*rest.RESTClient, *runtime.Scheme, *apiextensionsv1beta1.CustomResourceDefinition, error) {
	// Create the client config. Use kubeconfig if given, otherwise assume in-cluster.
	cfg, err := util.BuildConfig(kubeConfig)
	if err != nil {
		glog.Error(err)
		return nil, nil, nil, err
	}

	// initialize custom resource using a CustomResourceDefinition if it does not exist
	crd, err := CreateCustomResourceDefinition(extensionsClient)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		glog.Error(err)
		return nil, nil, nil, err
	}
	scheme := runtime.NewScheme()
	if err := disktype.AddToScheme(scheme); err != nil {
		return nil, nil, nil, err
	}

	config := *cfg
	config.GroupVersion = &disktype.SchemeGroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(scheme)}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, nil, nil, err
	}

	return client, scheme, crd, nil
}
