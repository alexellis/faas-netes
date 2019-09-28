// Copyright (c) Alex Ellis 2017. All rights reserved.
// Licensed under the MIT license. See LICENSE file in the project root for full license information.

package main

import (
	"flag"
	"log"
	"os"

	"github.com/openfaas/faas-netes/k8s"

	"github.com/openfaas/faas-netes/handlers"
	"github.com/openfaas/faas-netes/types"
	"github.com/openfaas/faas-netes/version"
	bootstrap "github.com/openfaas/faas-provider"
	"github.com/openfaas/faas-provider/logs"
	bootTypes "github.com/openfaas/faas-provider/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var kubeconfig string
	var masterURL string

	flag.StringVar(&kubeconfig, "kubeconfig", "",
		"Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "",
		"The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.Parse()

	clientCmdConfig, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	clientset, err := kubernetes.NewForConfig(clientCmdConfig)
	if err != nil {
		log.Fatalf("Error building Kubernetes clientset: %s", err.Error())

	}

	functionNamespace := "default"

	if namespace, exists := os.LookupEnv("function_namespace"); exists {
		functionNamespace = namespace
	}

	readConfig := types.ReadConfig{}
	osEnv := types.OsEnv{}
	cfg := readConfig.Read(osEnv)

	log.Printf("HTTP Read Timeout: %s\n", cfg.ReadTimeout)
	log.Printf("HTTP Write Timeout: %s\n", cfg.WriteTimeout)
	log.Printf("HTTPProbe: %v\n", cfg.HTTPProbe)
	log.Printf("SetNonRootUser: %v\n", cfg.SetNonRootUser)

	deployConfig := k8s.DeploymentConfig{
		RuntimeHTTPPort: 8080,
		HTTPProbe:       cfg.HTTPProbe,
		SetNonRootUser:  cfg.SetNonRootUser,
		ReadinessProbe: &k8s.ProbeConfig{
			InitialDelaySeconds: int32(cfg.ReadinessProbeInitialDelaySeconds),
			TimeoutSeconds:      int32(cfg.ReadinessProbeTimeoutSeconds),
			PeriodSeconds:       int32(cfg.ReadinessProbePeriodSeconds),
		},
		LivenessProbe: &k8s.ProbeConfig{
			InitialDelaySeconds: int32(cfg.LivenessProbeInitialDelaySeconds),
			TimeoutSeconds:      int32(cfg.LivenessProbeTimeoutSeconds),
			PeriodSeconds:       int32(cfg.LivenessProbePeriodSeconds),
		},
		ImagePullPolicy: cfg.ImagePullPolicy,
	}

	factory := k8s.NewFunctionFactory(clientset, deployConfig)

	bootstrapHandlers := bootTypes.FaaSHandlers{
		FunctionProxy:        handlers.MakeProxy(functionNamespace, cfg.ReadTimeout),
		DeleteHandler:        handlers.MakeDeleteHandler(functionNamespace, clientset),
		DeployHandler:        handlers.MakeDeployHandler(functionNamespace, factory),
		FunctionReader:       handlers.MakeFunctionReader(functionNamespace, clientset),
		ReplicaReader:        handlers.MakeReplicaReader(functionNamespace, clientset),
		ReplicaUpdater:       handlers.MakeReplicaUpdater(functionNamespace, clientset),
		UpdateHandler:        handlers.MakeUpdateHandler(functionNamespace, factory),
		HealthHandler:        handlers.MakeHealthHandler(),
		InfoHandler:          handlers.MakeInfoHandler(version.BuildVersion(), version.GitCommit),
		SecretHandler:        handlers.MakeSecretHandler(functionNamespace, clientset),
		LogHandler:           logs.NewLogHandlerFunc(handlers.NewLogRequestor(clientset, functionNamespace), cfg.WriteTimeout),
		ListNamespaceHandler: handlers.MakeNamespacesLister(functionNamespace, clientset),
	}

	var port int
	port = cfg.Port

	bootstrapConfig := bootTypes.FaaSConfig{
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		TCPPort:      &port,
		EnableHealth: true,
	}

	bootstrap.Serve(&bootstrapHandlers, &bootstrapConfig)
}
