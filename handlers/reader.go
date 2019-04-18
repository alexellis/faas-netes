// Copyright (c) Alex Ellis 2017. All rights reserved.
// Licensed under the MIT license. See LICENSE file in the project root for full license information.

package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/openfaas/faas/gateway/requests"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// MakeFunctionReader handler for reading functions deployed in the cluster as deployments.
func MakeFunctionReader(functionNamespace string, clientset *kubernetes.Clientset) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		functions, err := getServiceList(functionNamespace, clientset)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		functionBytes, _ := json.Marshal(functions)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(functionBytes)
	}
}

func getServiceList(functionNamespace string, clientset *kubernetes.Clientset) ([]requests.Function, error) {
	functions := []requests.Function{}

	listOpts := metav1.ListOptions{
		LabelSelector: FunctionNameLabel,
	}

	res, err := clientset.ExtensionsV1beta1().Deployments(functionNamespace).List(listOpts)

	if err != nil {
		return nil, err
	}

	for _, item := range res.Items {
		function := readFunction(item)
		if function != nil {
			functions = append(functions, *function)
		}
	}
	return functions, nil
}

// getService returns a function/service or nil if not found
func getService(functionNamespace string, functionName string, clientset *kubernetes.Clientset) (*requests.Function, error) {

	getOpts := metav1.GetOptions{}

	item, err := clientset.ExtensionsV1beta1().Deployments(functionNamespace).Get(functionName, getOpts)

	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}

		return nil, err
	}

	if item != nil {

		function := readFunction(*item)
		if function != nil {
			return function, nil
		}
	}

	return nil, fmt.Errorf("function: %s not found", functionName)
}

func readFunction(item v1beta1.Deployment) *requests.Function {
	var replicas uint64
	if item.Spec.Replicas != nil {
		replicas = uint64(*item.Spec.Replicas)
	}

	labels := item.Labels
	function := requests.Function{
		Name:              item.Name,
		Replicas:          replicas,
		Image:             item.Spec.Template.Spec.Containers[0].Image,
		AvailableReplicas: uint64(item.Status.AvailableReplicas),
		InvocationCount:   0,
		Labels:            &labels,
		Annotations:       &item.Spec.Template.Annotations,
	}

	return &function
}
