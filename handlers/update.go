package handlers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/openfaas/faas/gateway/requests"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// MakeUpdateHandler update specified function
func MakeUpdateHandler(functionNamespace string, clientset *kubernetes.Clientset) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		defer r.Body.Close()

		body, _ := ioutil.ReadAll(r.Body)

		request := requests.CreateFunctionRequest{}
		err := json.Unmarshal(body, &request)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		annotations := buildAnnotations(request)
		if err, status := updateDeploymentSpec(functionNamespace, clientset, request, annotations); err != nil {
			w.WriteHeader(status)
			w.Write([]byte(err.Error()))
		}

		if err, status := updateService(functionNamespace, clientset, request, annotations); err != nil {
			w.WriteHeader(status)
			w.Write([]byte(err.Error()))
		}

		w.WriteHeader(http.StatusAccepted)
	}
}

func updateDeploymentSpec(
	functionNamespace string,
	clientset *kubernetes.Clientset,
	request requests.CreateFunctionRequest,
	annotations map[string]string) (err error, httpStatus int) {
	getOpts := metav1.GetOptions{}

	deployment, findDeployErr := clientset.ExtensionsV1beta1().
		Deployments(functionNamespace).
		Get(request.Service, getOpts)

	if findDeployErr != nil {
		return findDeployErr, http.StatusNotFound
	}

	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		deployment.Spec.Template.Spec.Containers[0].Image = request.Image

		// Disabling update support to prevent unexpected mutations of deployed functions,
		// since imagePullPolicy is now configurable. This could be reconsidered later depending
		// on desired behavior, but will need to be updated to take config.
		//deployment.Spec.Template.Spec.Containers[0].ImagePullPolicy = v1.PullAlways

		deployment.Spec.Template.Spec.Containers[0].Env = buildEnvVars(&request)

		configureReadOnlyRootFilesystem(request, deployment)

		deployment.Spec.Template.Spec.NodeSelector = createSelector(request.Constraints)

		labels := buildLabels(request.Service, request.Labels)
		labels["uid"] = fmt.Sprintf("%d", time.Now().Nanosecond())

		deployment.Labels = labels
		deployment.Spec.Template.ObjectMeta.Labels = labels

		replicaCount, replicaErr := getMinReplicaCount(request.Labels)
		if replicaErr != nil {
			return replicaErr, http.StatusBadRequest
		}
		deployment.Spec.Replicas = replicaCount

		deployment.Annotations = annotations
		deployment.Spec.Template.Annotations = annotations
		deployment.Spec.Template.ObjectMeta.Annotations = annotations

		resources, resourceErr := createResources(request)
		if resourceErr != nil {
			return resourceErr, http.StatusBadRequest
		}

		deployment.Spec.Template.Spec.Containers[0].Resources = *resources

		var serviceAccount string

		if request.Annotations != nil {
			annotations := *request.Annotations
			if val, ok := annotations["com.openfaas.serviceaccount"]; ok && len(val) > 0 {
				serviceAccount = val
			}
		}

		deployment.Spec.Template.Spec.ServiceAccountName = serviceAccount

		existingSecrets, err := getSecrets(clientset, functionNamespace, request.Secrets)
		if err != nil {
			return err, http.StatusBadRequest
		}

		err = UpdateSecrets(request, deployment, existingSecrets)
		if err != nil {
			log.Println(err)
			return err, http.StatusBadRequest
		}
	}

	if _, updateErr := clientset.ExtensionsV1beta1().
		Deployments(functionNamespace).
		Update(deployment); updateErr != nil {

		return updateErr, http.StatusInternalServerError
	}

	return nil, http.StatusAccepted
}

func updateService(
	functionNamespace string,
	clientset *kubernetes.Clientset,
	request requests.CreateFunctionRequest,
	annotations map[string]string) (err error, httpStatus int) {

	getOpts := metav1.GetOptions{}

	service, findServiceErr := clientset.CoreV1().
		Services(functionNamespace).
		Get(request.Service, getOpts)

	if findServiceErr != nil {
		return findServiceErr, http.StatusNotFound
	}

	service.Annotations = annotations

	if _, updateErr := clientset.CoreV1().
		Services(functionNamespace).
		Update(service); updateErr != nil {

		return updateErr, http.StatusInternalServerError
	}

	return nil, http.StatusAccepted
}
