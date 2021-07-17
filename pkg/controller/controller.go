package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	glog "k8s.io/klog"

	faasv1 "github.com/openfaas/faas-netes/pkg/apis/openfaas/v1"
	clientset "github.com/openfaas/faas-netes/pkg/client/clientset/versioned"
	faasscheme "github.com/openfaas/faas-netes/pkg/client/clientset/versioned/scheme"
	informers "github.com/openfaas/faas-netes/pkg/client/informers/externalversions"
	listers "github.com/openfaas/faas-netes/pkg/client/listers/openfaas/v1"
)

const (
	controllerAgentName = "openfaas-operator"
	faasKind            = "Function"
	LabelMinReplicas    = "com.openfaas.scale.min"
	// SuccessSynced is used as part of the Event 'reason' when a Function is synced
	SuccessSynced = "Synced"
	// ErrResourceExists is used as part of the Event 'reason' when a Function fails
	// to sync due to a Deployment of the same name already existing.
	ErrResourceExists = "ErrResourceExists"

	// MessageResourceExists is the message used for Events when a resource
	// fails to sync due to a Deployment already existing
	MessageResourceExists = "Resource %q already exists and is not managed by OpenFaaS"
	// MessageResourceSynced is the message used for an Event fired when a Function
	// is synced successfully
	MessageResourceSynced = "Function synced successfully"
)

// Controller is the controller implementation for Function resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// faasclientset is a clientset for our own API group
	faasclientset clientset.Interface

	deploymentsLister appslisters.DeploymentLister
	deploymentsSynced cache.InformerSynced
	functionsLister   listers.FunctionLister
	functionsSynced   cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder

	// OpenFaaS function factory
	factory FunctionFactory
}

// NewController returns a new OpenFaaS controller
func NewController(
	kubeclientset kubernetes.Interface,
	faasclientset clientset.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	faasInformerFactory informers.SharedInformerFactory,
	factory FunctionFactory) *Controller {

	// obtain references to shared index informers for the Deployment and Function types
	deploymentInformer := kubeInformerFactory.Apps().V1().Deployments()
	faasInformer := faasInformerFactory.Openfaas().V1().Functions()

	// Create event broadcaster
	// Add o6s types to the default Kubernetes Scheme so Events can be
	// logged for faas-controller types.
	faasscheme.AddToScheme(scheme.Scheme)
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.V(4).Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset:     kubeclientset,
		faasclientset:     faasclientset,
		deploymentsLister: deploymentInformer.Lister(),
		deploymentsSynced: deploymentInformer.Informer().HasSynced,
		functionsLister:   faasInformer.Lister(),
		functionsSynced:   faasInformer.Informer().HasSynced,
		workqueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Functions"),
		recorder:          recorder,
		factory:           factory,
	}

	glog.Info("Setting up event handlers")

	//  Add Function (OpenFaaS CRD-entry) Informer
	//
	// Set up an event handler for when Function resources change
	faasInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueFunction,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueFunction(new)
		},
	})

	// Set up an event handler for when functions related resources like pods, deployments, replica sets
	// can't be materialized. This logs abnormal events like ImagePullBackOff, back-off restarting failed container,
	// failed to start container, oci runtime errors, etc
	// Enable this with -v=3
	kubeInformerFactory.Core().V1().Events().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				event := obj.(*corev1.Event)
				since := time.Since(event.LastTimestamp.Time)
				// log abnormal events occurred in the last minute
				if since.Seconds() < 61 && strings.Contains(event.Type, "Warning") {
					glog.V(3).Infof("Abnormal event detected on %s %s: %s", event.LastTimestamp, key, event.Message)
				}
			}
		},
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.deploymentsSynced, c.functionsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch two workers to process Function resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	glog.Info("Started workers")
	<-stopCh
	glog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
		}
		c.workqueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two.
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the Function resource with this namespace/name
	function, err := c.functionsLister.Functions(namespace).Get(name)
	if err != nil {
		// The Function resource may no longer exist, in which case we stop processing.
		if errors.IsNotFound(err) {
			runtime.HandleError(fmt.Errorf("function '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	deploymentName := function.Spec.Name
	if deploymentName == "" {
		// We choose to absorb the error here as the worker would requeue the
		// resource otherwise. Instead, the next time the resource is updated
		// the resource will be queued again.
		runtime.HandleError(fmt.Errorf("%s: deployment name must be specified", key))
		return nil
	}

	// Get the deployment with the name specified in Function.spec
	deployment, err := c.deploymentsLister.Deployments(function.Namespace).Get(deploymentName)
	// If the resource doesn't exist, we'll create it
	if errors.IsNotFound(err) {
		err = nil
		existingSecrets, err := c.getSecrets(function.Namespace, function.Spec.Secrets)
		if err != nil {
			return err
		}

		glog.Infof("Creating deployment for '%s'", function.Spec.Name)
		deployment, err = c.kubeclientset.AppsV1().Deployments(function.Namespace).Create(
			context.TODO(),
			newDeployment(function, deployment, existingSecrets, c.factory),
			metav1.CreateOptions{},
		)
		if err != nil {
			return err
		}
	}

	svcGetOptions := metav1.GetOptions{}
	_, getSvcErr := c.kubeclientset.CoreV1().Services(function.Namespace).Get(context.TODO(), deploymentName, svcGetOptions)
	if errors.IsNotFound(getSvcErr) {
		glog.Infof("Creating ClusterIP service for '%s'", function.Spec.Name)
		if _, err := c.kubeclientset.CoreV1().Services(function.Namespace).Create(context.TODO(), newService(function), metav1.CreateOptions{}); err != nil {
			// If an error occurs during Service Create, we'll requeue the item
			if errors.IsAlreadyExists(err) {
				err = nil
				glog.V(2).Infof("ClusterIP service '%s' already exists. Skipping creation.", function.Spec.Name)
			} else {
				return err
			}
		}
	}

	// If an error occurs during Get/Create, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return fmt.Errorf("transient error: %v", err)
	}

	// If the Deployment is not controlled by this Function resource, we should log
	// a warning to the event recorder and ret
	if !metav1.IsControlledBy(deployment, function) {
		msg := fmt.Sprintf(MessageResourceExists, deployment.Name)
		c.recorder.Event(function, corev1.EventTypeWarning, ErrResourceExists, msg)
		return fmt.Errorf(msg)
	}

	// Update the Deployment resource if the Function definition differs
	if deploymentNeedsUpdate(function, deployment) {
		glog.Infof("Updating deployment for '%s'", function.Spec.Name)

		existingSecrets, err := c.getSecrets(function.Namespace, function.Spec.Secrets)
		if err != nil {
			return err
		}

		deployment, err = c.kubeclientset.AppsV1().Deployments(function.Namespace).Update(
			context.TODO(),
			newDeployment(function, deployment, existingSecrets, c.factory),
			metav1.UpdateOptions{},
		)

		if err != nil {
			glog.Errorf("Updating deployment for '%s' failed: %v", function.Spec.Name, err)
		}

		existingService, err := c.kubeclientset.CoreV1().Services(function.Namespace).Get(context.TODO(), function.Spec.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		existingService.Annotations = makeAnnotations(function)
		_, err = c.kubeclientset.CoreV1().Services(function.Namespace).Update(context.TODO(), existingService, metav1.UpdateOptions{})
		if err != nil {
			glog.Errorf("Updating service for '%s' failed: %v", function.Spec.Name, err)
		}
	}

	// If an error occurs during Update, we'll requeue the item so we can
	// attempt processing again later. THis could have been caused by a
	// temporary network failure, or any other transient reason.
	if err != nil {
		return err
	}

	c.recorder.Event(function, corev1.EventTypeNormal, SuccessSynced, MessageResourceSynced)
	return nil
}

// enqueueFunction takes a Function resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Function.
func (c *Controller) enqueueFunction(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.workqueue.AddRateLimited(key)
}

// handleObject will take any resource implementing metav1.Object and attempt
// to find the Function resource that 'owns' it. It does this by looking at the
// objects metadata.ownerReferences field for an appropriate OwnerReference.
// It then enqueues that Function resource to be processed. If the object does not
// have an appropriate OwnerReference, it will simply be skipped.
func (c *Controller) handleObject(obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}
		glog.V(4).Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}
	glog.V(4).Infof("Processing object: %s", object.GetName())
	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		// If this object is not owned by a function, we should not do anything more
		// with it.
		if ownerRef.Kind != faasKind {
			return
		}

		function, err := c.functionsLister.Functions(object.GetNamespace()).Get(ownerRef.Name)
		if err != nil {
			glog.Infof("Function '%s' deleted. Ignoring orphaned object '%s'", ownerRef.Name, object.GetSelfLink())
			return
		}

		c.enqueueFunction(function)
		return
	}
}

// getSecrets queries Kubernetes for a list of secrets by name in the given k8s namespace.
func (c *Controller) getSecrets(namespace string, secretNames []string) (map[string]*corev1.Secret, error) {
	secrets := map[string]*corev1.Secret{}

	for _, secretName := range secretNames {
		secret, err := c.kubeclientset.CoreV1().Secrets(namespace).Get(context.TODO(), secretName, metav1.GetOptions{})
		if err != nil {
			return secrets, err
		}
		secrets[secretName] = secret
	}

	return secrets, nil
}

// getReplicas returns the desired number of replicas for a function taking into account
// the min replicas label, HPA, the OF autoscaler and scaled to zero deployments
func getReplicas(function *faasv1.Function, deployment *appsv1.Deployment) *int32 {
	var minReplicas *int32

	// extract min replicas from label if specified
	if function != nil && function.Spec.Labels != nil {
		lb := *function.Spec.Labels
		if value, exists := lb[LabelMinReplicas]; exists {
			r, err := strconv.Atoi(value)
			if err == nil && r > 0 {
				minReplicas = int32p(int32(r))
			}
		}
	}

	// extract current deployment replicas if specified
	var deploymentReplicas *int32
	if deployment != nil {
		deploymentReplicas = deployment.Spec.Replicas
	}

	// do not set replicas if min replicas is not set
	// and current deployment has no replicas count
	if minReplicas == nil && deploymentReplicas == nil {
		return nil
	}

	// set replicas to min if deployment has no replicas and min replicas exists
	if minReplicas != nil && deploymentReplicas == nil {
		return minReplicas
	}

	// do not override replicas when deployment is scaled to zero
	if deploymentReplicas != nil && *deploymentReplicas == 0 {
		return deploymentReplicas
	}

	// do not override replicas when min is not specified
	if minReplicas == nil && deploymentReplicas != nil {
		return deploymentReplicas
	}

	// do not override HPA or OF autoscaler replicas if the value is greater than min
	if minReplicas != nil && deploymentReplicas != nil {
		if *deploymentReplicas >= *minReplicas {
			return deploymentReplicas
		}
	}

	return minReplicas
}
