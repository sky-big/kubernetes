package util

import (
	"os"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"

	"github.com/golang/glog"
)

func ReadDir(fullPath string) ([]string, error) {
	dir, err := os.Open(fullPath)
	if err != nil {
		return nil, err
	}
	defer dir.Close()

	files, err := dir.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	return files, nil
}

func BuildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

// create apiextensions client
func CreateApiExtensionsClient(kubeconfig string) (apiextensionsclient.Interface, error) {
	// Create the client config. Use kubeconfig if given, otherwise assume in-cluster.
	cfg, err := BuildConfig(kubeconfig)
	if err != nil {
		glog.Error(err)
		return nil, err
	}

	client, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		glog.Error(err)
		return nil, err
	}

	return client, nil
}