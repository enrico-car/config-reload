/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"os"
	"time"

	configv1 "github.com/krateoplatformops/config-reload/apis/configreload/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	configMapName  = ".spec.configmapRef.name"
	deploymentName = ".spec.deploymentRef.name"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

// +kubebuilder:rbac:groups=configreload.example.com,resources=configreloads,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=configreload.example.com,resources=configreloads/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=configreload.example.com,resources=configreloads/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments/rollback,verbs=create
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

type reconciler struct {
	client.Client
	scheme *runtime.Scheme
}

func getLastRolloutTime(deployment *appsv1.Deployment) time.Time {
	if deployment.Status.Conditions == nil {
		return time.Time{}
	}
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing {
			if condition.LastUpdateTime.IsZero() {
				return time.Time{}
			}
			return condition.LastUpdateTime.Time
		}
	}
	return time.Time{}
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("configreload", req.NamespacedName)
	log.Info("reconciling config reload")

	var configreload configv1.ConfigReload
	if err := r.Get(ctx, req.NamespacedName, &configreload); err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Info("ConfigReload resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to get ConfigReload")
		return ctrl.Result{}, err
	}

	// Check if the ConfigMap and Deployment referenced in the ConfigReload exists
	var configMap corev1.ConfigMap
	if err := r.Get(ctx, types.NamespacedName{
		Name:      configreload.Spec.ConfigMapRef.Name,
		Namespace: configreload.Spec.ConfigMapRef.Namespace,
	}, &configMap); err != nil {
		log.Error(err, "unable to get ConfigMap")
		return ctrl.Result{}, err
	}
	var deployment appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{
		Name:      configreload.Spec.DeploymentRef.Name,
		Namespace: configreload.Spec.DeploymentRef.Namespace,
	}, &deployment); err != nil {
		log.Error(err, "unable to get Deployment")
		return ctrl.Result{}, err
	}

	// Initialize the status if it is not set
	updatedStatus := false
	if configreload.Status.LastRolloutTime.IsZero() || configreload.Status.LastRolloutTime.Before(&metav1.Time{Time: getLastRolloutTime(&deployment)}) {
		configreload.Status.LastRolloutTime = metav1.NewTime(getLastRolloutTime(&deployment))
		updatedStatus = true
	}
	if configreload.Status.LastConfigMapVersion == "" {
		configreload.Status.LastConfigMapVersion = configMap.ResourceVersion
		updatedStatus = true
	}

	if updatedStatus {
		if err := r.Status().Update(ctx, &configreload); err != nil {
			log.Error(err, "unable to update ConfigReload status during initialization")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if configreload.Status.LastConfigMapVersion == configMap.ResourceVersion {
		log.Info("ConfigMap version has not changed")
		return ctrl.Result{}, nil
	}

	// If the ConfigMap version has changed, we need to roll out the Deployment
	log.Info("ConfigMap version has changed, rolling out Deployment", "configMapVersion", configMap.ResourceVersion)
	deploymentCopy := deployment.DeepCopy()

	if deploymentCopy.Spec.Template.Annotations == nil {
		deploymentCopy.Spec.Template.Annotations = make(map[string]string)
	}
	now := time.Now()
	deploymentCopy.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = now.Format(time.RFC3339)

	if err := r.Update(ctx, deploymentCopy); err != nil {
		log.Error(err, "unable to update Deployment for rollout")
		return ctrl.Result{}, err
	}

	// Update the ConfigReload status with the last rollout time and ConfigMap version
	configreload.Status.LastRolloutTime = metav1.NewTime(getLastRolloutTime(deploymentCopy))
	configreload.Status.LastConfigMapVersion = configMap.ResourceVersion
	log.Info("updating ConfigReload status", "lastRolloutTime", configreload.Status.LastRolloutTime, "lastConfigMapVersion", configreload.Status.LastConfigMapVersion)

	if err := r.Status().Update(ctx, &configreload); err != nil {
		log.Error(err, "unable to update ConfigReload status")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *reconciler) findObjectsForConfigMap(ctx context.Context, configMap client.Object) []reconcile.Request {
	configReloadList := &configv1.ConfigReloadList{}

	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(configMapName, configMap.GetName()),
		Namespace:     configMap.GetNamespace(),
	}

	if err := r.List(ctx, configReloadList, listOps); err != nil {
		log.Log.Error(err, "unable to list ConfigReloads")
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(configReloadList.Items))
	for i, item := range configReloadList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			},
		}
	}

	return requests
}

func main() {
	ctrl.SetLogger(zap.New())

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// in a real controller, we'd create a new scheme for this
	err = configv1.AddToScheme(mgr.GetScheme())
	if err != nil {
		setupLog.Error(err, "unable to add scheme")
		os.Exit(1)
	}

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &configv1.ConfigReload{}, configMapName, func(rawObj client.Object) []string {
		// Extract the ConfigMap name from the ConfigReload Spec, if one is provided
		configReload := rawObj.(*configv1.ConfigReload)
		if configReload.Spec.ConfigMapRef.Name == "" {
			return nil
		}
		return []string{configReload.Spec.ConfigMapRef.Name}
	}); err != nil {
		setupLog.Error(err, "unable to create index for ConfigMapRef")
		os.Exit(1)
	}

	r := &reconciler{
		Client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
	}

	err = ctrl.NewControllerManagedBy(mgr).
		For(&configv1.ConfigReload{}).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForConfigMap),
			// Use a predicate to only trigger reconciliation when the ConfigMap's resource version changes
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
	if err != nil {
		setupLog.Error(err, "unable to create controller")
		os.Exit(1)
	}

	err = ctrl.NewWebhookManagedBy(mgr).
		For(&configv1.ConfigReload{}).
		Complete()
	if err != nil {
		setupLog.Error(err, "unable to create webhook")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
