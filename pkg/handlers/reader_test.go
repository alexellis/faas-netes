package handlers

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
)

func Test_getServiceList_ReturnsOnlyLabeledFunctions(t *testing.T) {
	stopCh := make(chan struct{})
	defer func() {
		stopCh <- struct{}{}
	}()

	fnNamespace := "functionNamespace"
	fn1 := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "function1",
			Namespace: fnNamespace,
			Labels: map[string]string{
				functionLabel: "function1",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: nil,
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Image: "function1:latest",
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: 1,
		},
	}
	fn2 := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "function2",
			Namespace: fnNamespace,
			Labels: map[string]string{
				functionLabel: "function2",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: nil,
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Image: "function2:latest",
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: 1,
		},
	}
	otherThing := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-deployment",
			Namespace: fnNamespace,
		},
	}
	otherNS := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-deployment",
			Namespace: "not-" + fnNamespace,
		},
	}

	kubeClient := fake.NewSimpleClientset(&fn1, &fn2, &otherThing, &otherNS)

	// setup informer like in main.go, but with shorter resync
	defaultResync := time.Millisecond
	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		kubeClient,
		defaultResync,
		informers.WithNamespace(fnNamespace),
	)
	// make sure to start the deployment informer or else it will timeout or find nothing
	deployments := informerFactory.Apps().V1().Deployments()
	go deployments.Informer().Run(stopCh)
	go informerFactory.Start(stopCh)
	time.Sleep(2 * defaultResync)

	found, err := getServiceList(fnNamespace, deployments.Lister())
	if err != nil {
		t.Fatalf("unexpected error listing the functions: %s", err)
	}

	if len(found) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(found))
	}

	// just test that we found the specific deployments, parsing the function replicas
	// is already tested
	found1 := found[0]
	if found1.Name != fn1.Name && found1.Name != fn2.Name {
		t.Fatalf("expected: %q or %q, got %q", fn1.Name, fn2.Name, found1.Name)
	}

	found2 := found[1]
	if found2.Name != fn1.Name && found2.Name != fn2.Name {
		t.Fatalf("expected: %q or %q, got %q", fn1.Name, fn2.Name, found2.Name)
	}
}
