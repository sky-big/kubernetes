package client

import (
	"fmt"
	"reflect"
	"time"

	"common"
	disktype "resource/disk/types"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	apiv1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	// Uncomment the following line to load the gcp plugin (only required to authenticate against GKE clusters).
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const diskCRDName = disktype.DiskResourcePlural + "." + common.GroupName

// Create crd
func CreateCustomResourceDefinition(clientset apiextensionsclient.Interface) (*apiextensionsv1beta1.CustomResourceDefinition, error) {
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: diskCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   common.GroupName,
			Version: disktype.SchemeGroupVersion.Version,
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: disktype.DiskResourcePlural,
				Kind:   reflect.TypeOf(disktype.Disk{}).Name(),
			},
		},
	}
	_, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	if err != nil {
		return crd, err
	}

	// wait for CRD being established
	err = wait.Poll(500*time.Millisecond, 60*time.Second, func() (bool, error) {
		crd, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(diskCRDName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextensionsv1beta1.Established:
				if cond.Status == apiextensionsv1beta1.ConditionTrue {
					return true, err
				}
			case apiextensionsv1beta1.NamesAccepted:
				if cond.Status == apiextensionsv1beta1.ConditionFalse {
					fmt.Printf("Name conflict: %v\n", cond.Reason)
				}
			}
		}
		return false, err
	})
	if err != nil {
		deleteErr := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(diskCRDName, nil)
		if deleteErr != nil {
			return nil, errors.NewAggregate([]error{err, deleteErr})
		}
		return nil, err
	}
	return crd, nil
}

// Delete crd
func DeleteCustomResourceDefinition(apiextensionsclientset *apiextensionsclient.Clientset, crd *apiextensionsv1beta1.CustomResourceDefinition) error {
	err := apiextensionsclientset.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(crd.Name, nil)

	return err
}

// create
func InsertDiskInstance(diskClient *rest.RESTClient, diskInstance *disktype.Disk) error {
	var result disktype.Disk

	err := diskClient.Post().
		Resource(disktype.DiskResourcePlural).
		Namespace(apiv1.NamespaceDefault).
		Body(diskInstance).
		Do().Into(&result)

	if err == nil {
		fmt.Printf("CREATED: %#v\n", result)
		return nil
	} else if apierrors.IsAlreadyExists(err) {
		fmt.Printf("ALREADY EXISTS: %#v\n", result)
		return err
	} else {
		return err
	}
}

// put
func PutDiskInstance(diskClient *rest.RESTClient, diskInstance *disktype.Disk) error {
	err := diskClient.Put().
		Name(diskInstance.ObjectMeta.Name).
		Namespace(diskInstance.ObjectMeta.Namespace).
		Resource(disktype.DiskResourcePlural).
		Body(diskInstance).
		Do().
		Error()

	if err != nil {
		fmt.Printf("ERROR updating status: %v\n", err)
	} else {
		fmt.Printf("UPDATED status: %#v\n", diskInstance)
	}

	return nil
}

// get
func GetDiskInstance(diskClient *rest.RESTClient, name string) (*disktype.Disk, error) {
	var disk disktype.Disk

	err := diskClient.Get().
		Resource(disktype.DiskResourcePlural).
		Namespace(apiv1.NamespaceDefault).
		Name(name).
		Do().Into(&disk)
	if err != nil {
		return nil, err
	}

	return &disk, nil
}

// delete
func DeleteDiskInstance(diskClient *rest.RESTClient, name string) error {
	err := diskClient.Delete().
		Resource(disktype.DiskResourcePlural).
		Namespace(apiv1.NamespaceDefault).
		Name(name).
		Do().
		Error()
	if err != nil {
		return err
	}

	return nil
}

// sync wait
func WaitForDiskInstanceRunning(diskClient *rest.RESTClient, name string) error {
	return wait.Poll(100*time.Millisecond, 10*time.Second, func() (bool, error) {
		var disk disktype.Disk
		err := diskClient.Get().
			Resource(disktype.DiskResourcePlural).
			Namespace(apiv1.NamespaceDefault).
			Name(name).
			Do().Into(&disk)

		if err == nil && disk.Status.State == disktype.DiskStateRunning {
			return true, nil
		}

		return false, err
	})
}
