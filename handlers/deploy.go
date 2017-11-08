// Copyright (c) Alex Ellis 2017. All rights reserved.
// Licensed under the MIT license. See LICENSE file in the project root for full license information.

package handlers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/openfaas/faas/gateway/requests"
	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// WatchdogPort for the OpenFaaS function watchdog
const WatchdogPort = 8080

// InitialReplicas how many replicas to start of creating for a function
const InitialReplicas = 1

// DefaultFunctionNamespace define default work namespace
const DefaultFunctionNamespace string = "default"

// ValidateDeployRequest validates that the service name is valid for Kubernetes
func ValidateDeployRequest(request *requests.CreateFunctionRequest) error {
	// Regex for RFC-1123 validation:
	// 	k8s.io/kubernetes/pkg/util/validation/validation.go
	var validDNS = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	matched := validDNS.MatchString(request.Service)
	if matched {
		return nil
	}

	return fmt.Errorf("(%s) must be a valid DNS entry for service name", request.Service)
}

// DeployHandlerConfig specify options for Deployments
type DeployHandlerConfig struct {
	EnableFunctionReadinessProbe bool
}

// MakeDeployHandler creates a handler to create new functions in the cluster
func MakeDeployHandler(functionNamespace string, clientset *kubernetes.Clientset, config *DeployHandlerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		body, _ := ioutil.ReadAll(r.Body)

		request := requests.CreateFunctionRequest{}
		err := json.Unmarshal(body, &request)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if err := ValidateDeployRequest(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		deploymentSpec := makeDeploymentSpec(request, config)
		deploy := clientset.Extensions().Deployments(functionNamespace)

		_, err = deploy.Create(deploymentSpec)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		log.Println("Created deployment - " + request.Service)

		service := clientset.Core().Services(functionNamespace)
		serviceSpec := makeServiceSpec(request)
		_, err = service.Create(serviceSpec)

		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		log.Println("Created service - " + request.Service)
		log.Println(string(body))

		w.WriteHeader(http.StatusAccepted)

	}
}

func makeDeploymentSpec(request requests.CreateFunctionRequest, config *DeployHandlerConfig) *v1beta1.Deployment {
	envVars := buildEnvVars(request)
	path := filepath.Join(os.TempDir(), ".lock")
	probe := &apiv1.Probe{
		Handler: apiv1.Handler{
			Exec: &apiv1.ExecAction{
				Command: []string{"cat", path},
			},
		},
		InitialDelaySeconds: 3,
		TimeoutSeconds:      1,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}
	if !config.EnableFunctionReadinessProbe {
		probe = nil
	}

	// Add / reference pre-existing secrets within Kubernetes
	imagePullSecrets := []apiv1.LocalObjectReference{}
	for _, secret := range request.Secrets {
		imagePullSecrets = append(imagePullSecrets,
			apiv1.LocalObjectReference{
				Name: secret,
			})
	}

	labels := map[string]string{
		"faas_function": request.Service,
	}
	if request.Labels != nil {
		for k, v := range *request.Labels {
			labels[k] = v
		}
	}

	nodeSelector := createSelector(request.Constraints)

	initialReplicas := int32p(InitialReplicas)

	deploymentSpec := &v1beta1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "extensions/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: request.Service,
		},
		Spec: v1beta1.DeploymentSpec{
			Replicas: initialReplicas,
			Strategy: v1beta1.DeploymentStrategy{
				Type: v1beta1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &v1beta1.RollingUpdateDeployment{
					MaxUnavailable: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: int32(0),
					},
					MaxSurge: &intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: int32(1),
					},
				},
			},
			RevisionHistoryLimit: int32p(10),
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   request.Service,
					Labels: labels,
				},
				Spec: apiv1.PodSpec{
					NodeSelector:     nodeSelector,
					ImagePullSecrets: imagePullSecrets,
					Containers: []apiv1.Container{
						{
							Name:  request.Service,
							Image: request.Image,
							Ports: []apiv1.ContainerPort{
								{ContainerPort: int32(WatchdogPort), Protocol: v1.ProtocolTCP},
							},
							Env: envVars,
							Resources: apiv1.ResourceRequirements{
								Limits: apiv1.ResourceList{
								//v1.ResourceCPU:    resource.MustParse("100m"),
								//v1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
							ImagePullPolicy: v1.PullAlways,
							LivenessProbe:   probe},
					},
					RestartPolicy: v1.RestartPolicyAlways,
					DNSPolicy:     v1.DNSClusterFirst,
				},
			},
		},
	}
	return deploymentSpec
}

func makeServiceSpec(request requests.CreateFunctionRequest) *v1.Service {
	serviceSpec := &v1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: request.Service,
		},
		Spec: v1.ServiceSpec{
			Type:     v1.ServiceTypeClusterIP,
			Selector: map[string]string{"faas_function": request.Service},
			Ports: []v1.ServicePort{
				{
					Protocol: v1.ProtocolTCP,
					Port:     8080,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: int32(8080),
					},
				},
			},
		},
	}
	return serviceSpec
}

func buildEnvVars(request requests.CreateFunctionRequest) []v1.EnvVar {
	envVars := []v1.EnvVar{}

	if len(request.EnvProcess) > 0 {
		envVar := v1.EnvVar{
			Name:  "fprocess",
			Value: request.EnvProcess,
		}
		envVars = append(envVars, envVar)
	}

	for k, v := range request.EnvVars {
		if len(request.EnvProcess) > 0 {
			envVar := v1.EnvVar{
				Name:  k,
				Value: v,
			}
			envVars = append(envVars, envVar)
		}
	}
	return envVars
}

func int32p(i int32) *int32 {
	return &i
}

func createSelector(constraints []string) map[string]string {
	selector := make(map[string]string)

	log.Println(constraints)
	if len(constraints) > 0 {
		for _, constraint := range constraints {
			parts := strings.Split(constraint, "=")

			if len(parts) == 2 {
				selector[parts[0]] = parts[1]
			}
		}
	}

	log.Println("selector: ", selector)
	return selector
}
