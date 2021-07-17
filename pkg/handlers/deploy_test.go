// Copyright 2020 OpenFaaS Author(s)
// Licensed under the MIT license. See LICENSE file in the project root for full license information.

package handlers

import (
	"testing"

	"github.com/openfaas/faas-netes/pkg/k8s"
	types "github.com/openfaas/faas-provider/types"
	"k8s.io/client-go/kubernetes/fake"

	apiv1 "k8s.io/api/core/v1"
)

func Test_buildAnnotations_Empty_In_CreateRequest(t *testing.T) {
	request := types.FunctionDeployment{}

	annotations := buildAnnotations(request)

	if len(annotations) != 1 {
		t.Errorf("want: %d annotations got: %d", 1, len(annotations))
	}

	v, ok := annotations["prometheus.io.scrape"]
	if !ok {
		t.Errorf("missing prometheus.io.scrape key")
	}

	want := "false"
	if v != want {
		t.Errorf("want: %s for annotation prometheus.io.scrape got: %s", want, v)
	}
}

func Test_buildAnnotations_Premetheus_NotOverridden(t *testing.T) {
	request := types.FunctionDeployment{Annotations: &map[string]string{"prometheus.io.scrape": "true"}}

	annotations := buildAnnotations(request)

	if len(annotations) != 1 {
		t.Errorf("want: %d annotations got: %d", 1, len(annotations))
	}

	v, ok := annotations["prometheus.io.scrape"]
	if !ok {
		t.Errorf("missing prometheus.io.scrape key")
	}
	want := "true"
	if v != want {
		t.Errorf("want: %s for annotation prometheus.io.scrape got: %s", want, v)
	}
}

func Test_buildAnnotations_From_CreateRequest(t *testing.T) {
	request := types.FunctionDeployment{
		Annotations: &map[string]string{
			"date-created": "Wed 25 Jul 21:26:22 BST 2018",
			"foo":          "bar",
		},
	}

	annotations := buildAnnotations(request)

	if len(annotations) != 3 {
		t.Errorf("want: %d annotations got: %d", 1, len(annotations))
	}

	v, ok := annotations["date-created"]
	if !ok {
		t.Errorf("missing date-created key")
	}

	if v != "Wed 25 Jul 21:26:22 BST 2018" {
		t.Errorf("want: %s for annotation date-created got: %s", "Wed 25 Jul 21:26:22 BST 2018", v)
	}
}

func Test_SetNonRootUser(t *testing.T) {

	scenarios := []struct {
		name       string
		setNonRoot bool
	}{
		{"does not set userid value when SetNonRootUser is false", false},
		{"does set userid to constant value when SetNonRootUser is true", true},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			request := types.FunctionDeployment{Service: "testfunc", Image: "alpine:latest"}
			factory := k8s.NewFunctionFactory(fake.NewSimpleClientset(), k8s.DeploymentConfig{
				LivenessProbe:  &k8s.ProbeConfig{},
				ReadinessProbe: &k8s.ProbeConfig{},
				SetNonRootUser: s.setNonRoot,
			}, nil)
			deployment, err := makeDeploymentSpec(request, map[string]*apiv1.Secret{}, factory)
			if err != nil {
				t.Errorf("unexpected makeDeploymentSpec error: %s", err.Error())
			}

			functionContainer := deployment.Spec.Template.Spec.Containers[0]
			if functionContainer.SecurityContext == nil {
				t.Errorf("expected container %s to have a non-nil security context", functionContainer.Name)
			}

			if !s.setNonRoot && functionContainer.SecurityContext.RunAsUser != nil {
				t.Errorf("expected RunAsUser to be nil, got %d", functionContainer.SecurityContext.RunAsUser)
			}

			if s.setNonRoot && *functionContainer.SecurityContext.RunAsUser != k8s.SecurityContextUserID {
				t.Errorf("expected RunAsUser to be %d, got %d", k8s.SecurityContextUserID, functionContainer.SecurityContext.RunAsUser)
			}
		})
	}

}

func Test_buildEnvVars_NoSortedKeys(t *testing.T) {

	inputEnvs := map[string]string{}

	function := types.FunctionDeployment{
		EnvVars: inputEnvs,
	}

	coreEnvs := buildEnvVars(&function)

	if len(coreEnvs) != 1 {
		t.Errorf("want: %d env-vars, got: %d", 1, len(coreEnvs))
		t.Fail()
	}
}

func Test_buildEnvVars_TwoSortedKeys(t *testing.T) {
	firstKey := "first"
	lastKey := "last"

	inputEnvs := map[string]string{
		lastKey:  "",
		firstKey: "",
	}

	function := types.FunctionDeployment{
		EnvVars: inputEnvs,
	}

	coreEnvs := buildEnvVars(&function)

	if coreEnvs[0].Name != "PORT" {
		t.Errorf("first want: %s, got: %s", "PORT", coreEnvs[0].Name)
		t.Fail()
	}

	if coreEnvs[1].Name != firstKey {
		t.Errorf("first want: %s, got: %s", firstKey, coreEnvs[0].Name)
		t.Fail()
	}
}

func Test_buildEnvVars_FourSortedKeys(t *testing.T) {
	firstKey := "alex"
	secondKey := "elliot"
	thirdKey := "stefan"
	lastKey := "zane"

	inputEnvs := map[string]string{
		lastKey:   "",
		firstKey:  "",
		thirdKey:  "",
		secondKey: "",
	}

	function := types.FunctionDeployment{
		EnvVars: inputEnvs,
	}

	coreEnvs := buildEnvVars(&function)

	if coreEnvs[0].Name != "PORT" {
		t.Errorf("first want: %s, got: %s", "PORT", coreEnvs[0].Name)
		t.Fail()
	}
	if coreEnvs[1].Name != firstKey {
		t.Errorf("first want: %s, got: %s", firstKey, coreEnvs[0].Name)
		t.Fail()
	}

	if coreEnvs[2].Name != secondKey {
		t.Errorf("second want: %s, got: %s", secondKey, coreEnvs[1].Name)
		t.Fail()
	}

	if coreEnvs[3].Name != thirdKey {
		t.Errorf("third want: %s, got: %s", thirdKey, coreEnvs[2].Name)
		t.Fail()
	}

	if coreEnvs[4].Name != lastKey {
		t.Errorf("last want: %s, got: %s", lastKey, coreEnvs[3].Name)
		t.Fail()
	}
}
