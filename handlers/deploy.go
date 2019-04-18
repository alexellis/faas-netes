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
	corev1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// watchdogPort for the OpenFaaS function watchdog
const watchdogPort = 8080

// Regex for RFC-1123 validation:
// 	k8s.io/kubernetes/pkg/util/validation/validation.go
var validDNS = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// ValidateDeployRequest validates that the service name is valid for Kubernetes
func ValidateDeployRequest(request *requests.CreateFunctionRequest) error {
	matched := validDNS.MatchString(request.Service)
	if matched {
		return nil
	}

	return fmt.Errorf("(%s) must be a valid DNS entry for service name", request.Service)
}

// FunctionProbeConfig specify options for Liveliness and Readiness checks
type FunctionProbeConfig struct {
	InitialDelaySeconds int32
	TimeoutSeconds      int32
	PeriodSeconds       int32
}

// DeployHandlerConfig specify options for Deployments
type DeployHandlerConfig struct {
	HTTPProbe                    bool
	FunctionReadinessProbeConfig *FunctionProbeConfig
	FunctionLivenessProbeConfig  *FunctionProbeConfig
	ImagePullPolicy              string
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

		existingSecrets, err := getSecrets(clientset, functionNamespace, request.Secrets)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		deploymentSpec, specErr := makeDeploymentSpec(request, existingSecrets, config)

		if specErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(specErr.Error()))
			return
		}

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

func makeDeploymentSpec(request requests.CreateFunctionRequest, existingSecrets map[string]*apiv1.Secret, config *DeployHandlerConfig) (*v1beta1.Deployment, error) {
	envVars := buildEnvVars(&request)
	var handler apiv1.Handler

	if config.HTTPProbe {
		handler = apiv1.Handler{
			HTTPGet: &apiv1.HTTPGetAction{
				Path: "/_/health",
				Port: intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: int32(watchdogPort),
				},
			},
		}
	} else {
		path := filepath.Join(os.TempDir(), ".lock")
		handler = apiv1.Handler{
			Exec: &apiv1.ExecAction{
				Command: []string{"cat", path},
			},
		}
	}
	readinessProbe := &apiv1.Probe{
		Handler:             handler,
		InitialDelaySeconds: config.FunctionReadinessProbeConfig.InitialDelaySeconds,
		TimeoutSeconds:      config.FunctionReadinessProbeConfig.TimeoutSeconds,
		PeriodSeconds:       config.FunctionReadinessProbeConfig.PeriodSeconds,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}
	livenessProbe := &apiv1.Probe{
		Handler:             handler,
		InitialDelaySeconds: config.FunctionLivenessProbeConfig.InitialDelaySeconds,
		TimeoutSeconds:      config.FunctionLivenessProbeConfig.TimeoutSeconds,
		PeriodSeconds:       config.FunctionLivenessProbeConfig.PeriodSeconds,
		SuccessThreshold:    1,
		FailureThreshold:    3,
	}

	initialReplicas, replicaErr := getMinReplicaCount(request.Labels)
	if replicaErr != nil {
		return nil, replicaErr
	}
	labels := buildLabels(request.Service, request.Labels)
	nodeSelector := createSelector(request.Constraints)
	resources, resourceErr := createResources(request)

	if resourceErr != nil {
		return nil, resourceErr
	}

	var imagePullPolicy apiv1.PullPolicy
	switch config.ImagePullPolicy {
	case "Never":
		imagePullPolicy = apiv1.PullNever
	case "IfNotPresent":
		imagePullPolicy = apiv1.PullIfNotPresent
	default:
		imagePullPolicy = apiv1.PullAlways
	}

	annotations := buildAnnotations(request)

	var serviceAccount string

	if request.Annotations != nil {
		annotations := *request.Annotations
		if val, ok := annotations["com.openfaas.serviceaccount"]; ok && len(val) > 0 {
			serviceAccount = val
		}
	}

	deploymentSpec := &v1beta1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "extensions/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        request.Service,
			Annotations: annotations,
		},
		Spec: v1beta1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					FunctionNameLabel: request.Service,
				},
			},
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
					Name:        request.Service,
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: apiv1.PodSpec{
					NodeSelector: nodeSelector,
					Containers: []apiv1.Container{
						{
							Name:  request.Service,
							Image: request.Image,
							Ports: []apiv1.ContainerPort{
								{ContainerPort: int32(watchdogPort), Protocol: corev1.ProtocolTCP},
							},
							Env:             envVars,
							Resources:       *resources,
							ImagePullPolicy: imagePullPolicy,
							LivenessProbe:   livenessProbe,
							ReadinessProbe:  readinessProbe,
							SecurityContext: &corev1.SecurityContext{
								ReadOnlyRootFilesystem: &request.ReadOnlyRootFilesystem,
							},
						},
					},
					ServiceAccountName: serviceAccount,
					RestartPolicy:      corev1.RestartPolicyAlways,
					DNSPolicy:          corev1.DNSClusterFirst,
				},
			},
		},
	}

	configureReadOnlyRootFilesystem(request, deploymentSpec)

	if err := UpdateSecrets(request, deploymentSpec, existingSecrets); err != nil {
		return nil, err
	}

	return deploymentSpec, nil
}

func makeServiceSpec(request requests.CreateFunctionRequest) *corev1.Service {

	serviceSpec := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        request.Service,
			Annotations: buildAnnotations(request),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				FunctionNameLabel: request.Service,
			},
			Ports: []corev1.ServicePort{
				{
					Name:     "http",
					Protocol: corev1.ProtocolTCP,
					Port:     watchdogPort,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: int32(watchdogPort),
					},
				},
			},
		},
	}

	return serviceSpec
}

func buildAnnotations(request requests.CreateFunctionRequest) map[string]string {
	var annotations map[string]string
	if request.Annotations != nil {
		annotations = *request.Annotations
	} else {
		annotations = map[string]string{}
	}

	annotations["prometheus.io.scrape"] = "false"
	return annotations
}

func buildEnvVars(request *requests.CreateFunctionRequest) []corev1.EnvVar {
	envVars := []corev1.EnvVar{}

	if len(request.EnvProcess) > 0 {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "fprocess",
			Value: request.EnvProcess,
		})
	}

	for k, v := range request.EnvVars {
		envVars = append(envVars, corev1.EnvVar{
			Name:  k,
			Value: v,
		})
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

	// log.Println("selector: ", selector)
	return selector
}

func createResources(request requests.CreateFunctionRequest) (*apiv1.ResourceRequirements, error) {
	resources := &apiv1.ResourceRequirements{
		Limits:   apiv1.ResourceList{},
		Requests: apiv1.ResourceList{},
	}

	// Set Memory limits
	if request.Limits != nil && len(request.Limits.Memory) > 0 {
		qty, err := resource.ParseQuantity(request.Limits.Memory)
		if err != nil {
			return resources, err
		}
		resources.Limits[apiv1.ResourceMemory] = qty
	}

	if request.Requests != nil && len(request.Requests.Memory) > 0 {
		qty, err := resource.ParseQuantity(request.Requests.Memory)
		if err != nil {
			return resources, err
		}
		resources.Requests[apiv1.ResourceMemory] = qty
	}

	// Set CPU limits
	if request.Limits != nil && len(request.Limits.CPU) > 0 {
		qty, err := resource.ParseQuantity(request.Limits.CPU)
		if err != nil {
			return resources, err
		}
		resources.Limits[apiv1.ResourceCPU] = qty
	}

	if request.Requests != nil && len(request.Requests.CPU) > 0 {
		qty, err := resource.ParseQuantity(request.Requests.CPU)
		if err != nil {
			return resources, err
		}
		resources.Requests[apiv1.ResourceCPU] = qty
	}

	return resources, nil
}

// configureReadOnlyRootFilesystem will create or update the required settings and mounts to ensure
// that the ReadOnlyRootFilesystem setting works as expected, meaning:
// 1. when ReadOnlyRootFilesystem is true, the security context of the container will have ReadOnlyRootFilesystem also
//    marked as true and a new `/tmp` folder mount will be added to the deployment spec
// 2. when ReadOnlyRootFilesystem is false, the security context of the container will also have ReadOnlyRootFilesystem set
//    to false and there will be no mount for the `/tmp` folder
//
// This method is safe for both create and update operations.
func configureReadOnlyRootFilesystem(request requests.CreateFunctionRequest, deployment *v1beta1.Deployment) {
	if deployment.Spec.Template.Spec.Containers[0].SecurityContext != nil {
		deployment.Spec.Template.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem = &request.ReadOnlyRootFilesystem
	} else {
		deployment.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			ReadOnlyRootFilesystem: &request.ReadOnlyRootFilesystem,
		}
	}

	existingVolumes := removeVolume("temp", deployment.Spec.Template.Spec.Volumes)
	deployment.Spec.Template.Spec.Volumes = existingVolumes

	existingMounts := removeVolumeMount("temp", deployment.Spec.Template.Spec.Containers[0].VolumeMounts)
	deployment.Spec.Template.Spec.Containers[0].VolumeMounts = existingMounts

	if request.ReadOnlyRootFilesystem {
		deployment.Spec.Template.Spec.Volumes = append(
			existingVolumes,
			corev1.Volume{
				Name: "temp",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		)

		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(
			existingMounts,
			corev1.VolumeMount{
				Name:      "temp",
				MountPath: "/tmp",
				ReadOnly:  false},
		)
	}
}
