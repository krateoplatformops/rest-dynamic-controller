//go:build integration
// +build integration

package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/clientcmd"
)

func TestGVKtoGVR(t *testing.T) {
	cfg, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		t.Fatal(err)
	}

	dis, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	gvr, err := GVKtoGVR(dis, schema.FromAPIVersionAndKind("dummy-charts.krateo.io/v0-2-0", "DummyChart"))
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, "dummycharts", gvr.Resource)
}
