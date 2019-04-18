package handlers

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	// initialReplicasCount how many replicas to start of creating for a function, this is
	// also used as the default return value for getMinReplicaCount
	initialReplicasCount = 1

	// FunctionNameLabel is the label key used by OpenFaaS to store the function name
	// on the resources managed by OpenFaaS for that function.  This key is also used to
	// denote that a resource is a "Function"
	FunctionNameLabel = "faas_function"
	// FunctionMinReplicaCount is a label that user's can set and will be passed to Kubernetes
	// as the Deployment replicas value.
	FunctionMinReplicaCount = "com.openfaas.scale.min"
	// FunctionVersionUID is the lable key used to store the uid value for the deploy/update of a
	// function, this is currently a unix timestamp.
	FunctionVersionUID = "com.openfaas.uid"
)

// buildLabels will copy the user request labels and ensure that any required internal labels
// are set appropriately.
func buildLabels(functionName string, requestLables *map[string]string) map[string]string {
	labels := map[string]string{}
	if requestLables != nil {
		for k, v := range *requestLables {
			labels[k] = v
		}
	}

	labels[FunctionNameLabel] = functionName
	labels[FunctionVersionUID] = fmt.Sprintf("%d", time.Now().Nanosecond())

	return labels
}

// getMinReplicaCount extracts the functions minimum replica count from the user's
// request labels. If the value is not found, this will return the default value, 1.
func getMinReplicaCount(labels *map[string]string) (*int32, error) {
	if labels == nil {
		return int32p(initialReplicasCount), nil
	}

	l := *labels
	if value, exists := l[FunctionMinReplicaCount]; exists {
		minReplicas, err := strconv.Atoi(value)
		if err != nil {
			return nil, errors.New("could not parse the minimum replica value")
		}

		if minReplicas > 0 {
			return int32p(int32(minReplicas)), nil
		}

		return nil, errors.New("replica count must be a positive integer")
	}

	return int32p(initialReplicasCount), nil
}
