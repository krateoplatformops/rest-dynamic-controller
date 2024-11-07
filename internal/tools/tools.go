package tools

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
)

type UpdateOptions struct {
	DiscoveryClient *discovery.DiscoveryClient
	DynamicClient   dynamic.Interface
}

func Update(ctx context.Context, el *unstructured.Unstructured, opts UpdateOptions) error {
	gvr, err := GVKtoGVR(opts.DiscoveryClient, el.GroupVersionKind())
	if err != nil {
		return err
	}

	_, err = opts.DynamicClient.Resource(gvr).
		Namespace(el.GetNamespace()).
		Update(ctx, el, metav1.UpdateOptions{
			FieldValidation: "Ignore",
		})

	return err
}

func UpdateStatus(ctx context.Context, el *unstructured.Unstructured, opts UpdateOptions) error {
	gvr, err := GVKtoGVR(opts.DiscoveryClient, el.GroupVersionKind())
	if err != nil {
		return err
	}

	_, err = opts.DynamicClient.Resource(gvr).
		Namespace(el.GetNamespace()).
		UpdateStatus(ctx, el, metav1.UpdateOptions{})

	return err
}

func GVKtoGVR(dc *discovery.DiscoveryClient, gvk schema.GroupVersionKind) (schema.GroupVersionResource, error) {
	groupResources, err := restmapper.GetAPIGroupResources(dc)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}

	mapper := restmapper.NewDiscoveryRESTMapper(groupResources)
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}

	return mapping.Resource, nil
}
