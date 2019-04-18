// Copyright (c) Alex Ellis 2017. All rights reserved.
// Licensed under the MIT license. See LICENSE file in the project root for full license information.

package handlers

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/openfaas/faas/gateway/requests"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// MakeDeleteHandler delete a function
func MakeDeleteHandler(functionNamespace string, clientset *kubernetes.Clientset) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		body, _ := ioutil.ReadAll(r.Body)

		request := requests.DeleteFunctionRequest{}
		err := json.Unmarshal(body, &request)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if len(request.FunctionName) == 0 {
			w.WriteHeader(http.StatusBadRequest)
		}

		getOpts := metav1.GetOptions{}

		// This makes sure we don't delete non-labelled deployments
		deployment, findDeployErr := clientset.ExtensionsV1beta1().
			Deployments(functionNamespace).
			Get(request.FunctionName, getOpts)

		if findDeployErr != nil {
			if errors.IsNotFound(findDeployErr) {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}

			w.Write([]byte(findDeployErr.Error()))
			return
		}

		if isFunction(deployment) {
			deleteFunction(functionNamespace, clientset, request, w)
		} else {
			w.WriteHeader(http.StatusBadRequest)

			w.Write([]byte("Not a function: " + request.FunctionName))
			return
		}

		w.WriteHeader(http.StatusAccepted)
	}
}

func isFunction(deployment *v1beta1.Deployment) bool {
	if deployment != nil {
		if _, found := deployment.Labels[FunctionNameLabel]; found {
			return true
		}
	}
	return false
}

func deleteFunction(functionNamespace string, clientset *kubernetes.Clientset, request requests.DeleteFunctionRequest, w http.ResponseWriter) {
	foregroundPolicy := metav1.DeletePropagationForeground
	opts := &metav1.DeleteOptions{PropagationPolicy: &foregroundPolicy}

	if deployErr := clientset.ExtensionsV1beta1().
		Deployments(functionNamespace).
		Delete(request.FunctionName, opts); deployErr != nil {

		if errors.IsNotFound(deployErr) {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.Write([]byte(deployErr.Error()))
		return
	}

	if svcErr := clientset.CoreV1().
		Services(functionNamespace).
		Delete(request.FunctionName, opts); svcErr != nil {

		if errors.IsNotFound(svcErr) {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}

		w.Write([]byte(svcErr.Error()))
		return
	}
}
