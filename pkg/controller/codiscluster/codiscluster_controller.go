/*
Copyright 2018 The Kubernetes Authors.

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

package codiscluster

import (
	"context"
	"fmt"

	codisv1alpha1 "github.com/tangcong/codis-operator/pkg/apis/codis/v1alpha1"
	member "github.com/tangcong/codis-operator/pkg/manager"
	"github.com/tangcong/codis-operator/pkg/manager/dashboard"
	"github.com/tangcong/codis-operator/pkg/manager/fe"
	"github.com/tangcong/codis-operator/pkg/manager/proxy"
	"github.com/tangcong/codis-operator/pkg/manager/redis"
	"github.com/tangcong/codis-operator/pkg/manager/sentinel"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	//	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	//"k8s.io/apimachinery/pkg/types"
	log "github.com/golang/glog"
	"k8s.io/client-go/kubernetes"
	eventv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new CodisCluster Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
// USER ACTION REQUIRED: update cmd/manager/main.go to call this codis.Add(mgr) to install this Controller
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("failed to get config: %v", err)
	}
	kubeCli, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("failed to get kubernetes Clientset: %v", err)
	}
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(log.Infof)
	eventBroadcaster.StartRecordingToSink(&eventv1.EventSinkImpl{
		Interface: eventv1.New(kubeCli.CoreV1().RESTClient()).Events("")})
	recorder := eventBroadcaster.NewRecorder(mgr.GetScheme(), corev1.EventSource{Component: "codiscluster"})
	proxy := proxy.NewProxyManager(mgr.GetClient(), mgr.GetScheme(), recorder)
	dashboard := dashboard.NewDashboardManager(mgr.GetClient(), mgr.GetScheme(), recorder)
	fe := fe.NewFeManager(mgr.GetClient(), mgr.GetScheme(), recorder)
	redis := redis.NewRedisManager(mgr.GetClient(), mgr.GetScheme(), recorder)
	sentinel := sentinel.NewSentinelManager(mgr.GetClient(), mgr.GetScheme(), recorder)
	return &defaultCodisClusterControl{Client: mgr.GetClient(), scheme: mgr.GetScheme(), recorder: recorder, proxy: proxy, dashboard: dashboard, fe: fe, redis: redis, sentinel: sentinel}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("codiscluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to CodisCluster
	err = c.Watch(&source.Kind{Type: &codisv1alpha1.CodisCluster{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}
	log.Infof("watch codis cluster,err is %s\n", err)

	// watch Deployment created by CodisCluster
	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &codisv1alpha1.CodisCluster{},
	})
	if err != nil {
		return err
	}
	log.Infof("watch deployment,err is %s\n", err)

	// watch Statefulset created by CodisCluster
	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &codisv1alpha1.CodisCluster{},
	})
	if err != nil {
		return err
	}
	log.Infof("watch statefulset,err is %s\n", err)

	return nil
}

// Reconcile reads that state of the cluster for a CodisCluster object and makes changes based on the state read
// and what is in the CodisCluster.Spec
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=codis.k8s.io,resources=codisclusters,verbs=get;list;watch;create;update;patch;delete
func (r *defaultCodisClusterControl) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the CodisCluster instance
	cluster := &codisv1alpha1.CodisCluster{}
	err := r.Get(context.TODO(), request.NamespacedName, cluster)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			log.Infof("codis cluster %s not found,err is %s\n", request.NamespacedName, err)
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Infof("get codis cluster %s failed,err is %s\n", request.NamespacedName, err)
		return reconcile.Result{}, err
	}
	if cluster.DeletionTimestamp != nil {
		log.Infof("codis cluster %s will be deleted,timestamp is %s\n", request.NamespacedName, cluster.DeletionTimestamp.String())
		return reconcile.Result{}, nil
	}
	log.Infof("codis cluster %s changed\n", request.NamespacedName)
	if err = r.ReconcileCodisCluster(cluster); err != nil {
		reason := fmt.Sprintf("Failed:%s", err)
		msg := fmt.Sprintf("CodisCluster %s failed error: %s", cluster.GetName(), err)
		r.recorder.Event(cluster, corev1.EventTypeWarning, reason, msg)
	}

	/*
		// TODO(user): Change this for the object type created by your controller
		// Update the found object and write the result back if there are any changes
		if !reflect.DeepEqual(deploy.Spec, found.Spec) {
			found.Spec = deploy.Spec
			log.Infof("Updating Deployment %s/%s\n", deploy.Namespace, deploy.Name)
			err = r.Update(context.TODO(), found)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	*/
	//to do

	return reconcile.Result{}, nil
}

type defaultCodisClusterControl struct {
	client.Client
	scheme    *runtime.Scheme
	proxy     member.Manager
	dashboard member.Manager
	redis     member.Manager
	fe        member.Manager
	sentinel  member.Manager
	recorder  record.EventRecorder
}

func (ccc *defaultCodisClusterControl) ReconcileCodisCluster(cc *codisv1alpha1.CodisCluster) error {
	err := ccc.dashboard.Reconcile(cc)
	if err != nil {
		log.Infof("Reconcile dashboard,err is %s\n", err)
	}
	log.Info("Reconcile dashboard succ\n")
	err = ccc.proxy.Reconcile(cc)
	if err != nil {
		log.Infof("Reconcile Proxy,err is %s\n", err)
	}
	log.Info("Reconcile proxy succ\n")
	err = ccc.fe.Reconcile(cc)
	if err != nil {
		log.Infof("Reconcile fe,err is %s\n", err)
	}
	log.Info("Reconcile fe succ\n")
	err = ccc.redis.Reconcile(cc)
	if err != nil {
		log.Infof("Reconcile redis,err is %s\n", err)
	}
	log.Info("Reconcile redis succ\n")
	err = ccc.sentinel.Reconcile(cc)
	if err != nil {
		log.Infof("Reconcile sentinel,err is %s\n", err)
	}
	log.Info("Reconcile Sentinel succ\n")
	return err
}
