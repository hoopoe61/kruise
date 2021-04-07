/*


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

package unitedobject

import (
	"context"
	"fmt"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	"github.com/openkruise/kruise/pkg/util"
	utildiscovery "github.com/openkruise/kruise/pkg/util/discovery"
)

const (
	cascadingFlag  = "cascading.uo."
	controllerName = "unitedobject-controller"

	eventTypeRevisionProvision = "RevisionProvision"
	eventTypeFindInstances     = "FindInstances"

	slowStartInitialBatchSize = 1
	updateRetries             = 5
)

var (
	controllerKind                      = appsv1alpha1.SchemeGroupVersion.WithKind("UnitedObject")
	_              reconcile.Reconciler = &UOReconciler{}
)

// UOReconciler reconciles a UnitedObject object
type UOReconciler struct {
	client.Client
	Config   *rest.Config
	Recorder record.EventRecorder
	Scheme   *runtime.Scheme
}

// Add creates a new UnitedObject Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	if !utildiscovery.DiscoverGVK(controllerKind) {
		return nil
	}
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	cli := util.NewClientFromManager(mgr, controllerName)
	return &UOReconciler{
		Client: cli,
		//used for discovery client
		Config:   mgr.GetConfig(),
		Recorder: mgr.GetEventRecorderFor(controllerName),
		Scheme:   mgr.GetScheme(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	builder := ctrl.NewControllerManagedBy(mgr).For(&appsv1alpha1.UnitedObject{})
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(r.(*UOReconciler).Config)
	if err != nil {
		return err
	}
	_, apiResourceList, err := discoveryClient.ServerGroupsAndResources()
	if err != nil {
		return err
	}
	for _, resources := range apiResourceList {
		gv, err := schema.ParseGroupVersion(resources.GroupVersion)
		if err != nil {
			return err
		}
		for _, oneGVK := range resources.APIResources {
			// only watch resources with "watch" verb and can be accessed(set in rbac) by current sa or user
			if util.IfContainItem("watch", oneGVK.Verbs) {
				if can, err := canIWatch(gv, oneGVK, r.(*UOReconciler)); err != nil || !can {
					klog.Warningf("Fail to watch resources, error: %v,  access authorization: %v", err, can)
					continue
				}
				uu := &unstructured.Unstructured{}
				uu.SetGroupVersionKind(schema.GroupVersionKind{Group: gv.Group, Version: gv.Version, Kind: oneGVK.Kind})
				builder = builder.Watches(&source.Kind{Type: uu}, &handler.EnqueueRequestForOwner{OwnerType: &appsv1alpha1.UnitedObject{}})
			}
		}
	}
	return builder.WithEventFilter(&UOPredicate{}).Complete(r)
}

// canIWatch returns if current sa or user can watch this resource
func canIWatch(gv schema.GroupVersion, oneGVK metav1.APIResource, r *UOReconciler) (bool, error) {
	ssar := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Group:    gv.Group,
				Resource: oneGVK.Name,
				Verb:     "watch",
			},
		},
	}
	r.Delete(context.TODO(), ssar)
	if err := r.Create(context.TODO(), ssar); err != nil {
		return false, err
	}
	time.Sleep(10 * time.Millisecond)
	allowd := ssar.Status.Allowed
	klog.V(2).Infof("Get SelfSubjectAccessReview: %v", ssar)
	r.Delete(context.TODO(), ssar)
	return allowd, nil
}

// set groups=*,resources=*,verbs=* to let unitedobject to control all resources in cluster
// +kubebuilder:rbac:groups=apps.kruise.io,resources=unitedobjects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kruise.io,resources=unitedobjects/status,verbs=get;update;patch

func (r *UOReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()

	klog.V(0).Infof("Begin to Reconcile unitedobject: %s/%s", req.Namespace, req.Name)
	uo := &appsv1alpha1.UnitedObject{}
	if err := r.Get(context.TODO(), req.NamespacedName, uo); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if uo.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}
	oldStatus := uo.Status.DeepCopy()
	currentRevision, updatedRevision, _, collisionCount, err := r.constructUnitedObjectRevisions(uo)
	if err != nil {
		klog.Errorf("Fail to construct controllerrevision of unitedobject %s/%s: %v", uo.Namespace, uo.Name, err)
		r.Recorder.Event(uo.DeepCopy(), corev1.EventTypeWarning, fmt.Sprintf("Failed %s", eventTypeRevisionProvision), err.Error())
		return reconcile.Result{}, err
	}

	expectedRevision := currentRevision.Name
	if updatedRevision != nil {
		expectedRevision = updatedRevision.Name
	}
	// manageInstances will create, update and delete objects
	newStatus, manageErr := r.manageInstances(uo, expectedRevision)
	_, updateStatusErr := r.updateStatus(uo, newStatus, oldStatus, currentRevision, updatedRevision, collisionCount, manageErr)
	if manageErr != nil {
		return ctrl.Result{}, manageErr
	}
	if updateStatusErr != nil {
		return ctrl.Result{}, updateStatusErr
	}
	return ctrl.Result{}, nil
}

func (r *UOReconciler) updateStatus(uo *appsv1alpha1.UnitedObject, newStatus, oldStatus *appsv1alpha1.UnitedObjectStatus, currentRevision, updatedRevision *appsv1.ControllerRevision, collisionCount int32, manageErr error) (reconcile.Result, error) {
	var err error
	if newStatus, err = r.calculateUnitedObjectStatus(uo, newStatus, currentRevision, updatedRevision, collisionCount, manageErr); err != nil {
		return reconcile.Result{}, err
	}
	_, err = r.updateUnitedObjectStatus(uo, oldStatus, newStatus)
	return reconcile.Result{}, err
}

func (r *UOReconciler) calculateUnitedObjectStatus(uo *appsv1alpha1.UnitedObject, newStatus *appsv1alpha1.UnitedObjectStatus, currentRevision, updatedRevision *appsv1.ControllerRevision, collisionCount int32, manageErr error) (*appsv1alpha1.UnitedObjectStatus, error) {
	expectedRevision := currentRevision.Name
	if updatedRevision != nil {
		expectedRevision = updatedRevision.Name
	}
	if newStatus.CurrentRevision == "" {
		newStatus.CurrentRevision = currentRevision.Name
	}
	newStatus.UpdatedRevision = expectedRevision
	if newStatus.CollisionCount == nil || (*newStatus.CollisionCount != collisionCount) {
		newStatus.CollisionCount = &collisionCount
	}
	// only update CurrentRevision when the manageInstances(...) returns no error
	if newStatus.CurrentRevision != newStatus.UpdatedRevision && manageErr == nil {
		newStatus.CurrentRevision = newStatus.UpdatedRevision
	}

	instancesLists, err := r.getAllInstances(uo)
	if err != nil {
		return nil, err
	}
	newStatus.ExistInstances = int32(len(instancesLists))
	newStatus.Instances = int32(len(uo.Spec.Instances))
	updateInstances := 0
	for _, instance := range instancesLists {
		if getRevision(instance) == expectedRevision {
			updateInstances++
		}
	}
	newStatus.UpdatedInstances = int32(updateInstances)

	return newStatus, nil
}

func (r *UOReconciler) updateUnitedObjectStatus(uo *appsv1alpha1.UnitedObject, oldStatus, newStatus *appsv1alpha1.UnitedObjectStatus) (*appsv1alpha1.UnitedObject, error) {
	if oldStatus.CurrentRevision == newStatus.CurrentRevision &&
		*oldStatus.CollisionCount == *newStatus.CollisionCount &&
		uo.Generation == newStatus.ObservedGeneration &&
		oldStatus.UpdatedRevision == newStatus.UpdatedRevision &&
		reflect.DeepEqual(oldStatus.Conditions, newStatus.Conditions) {
		return uo, nil
	}

	newStatus.ObservedGeneration = uo.Generation
	var getErr, updateErr error
	for i, obj := 0, uo; ; i++ {
		klog.V(2).Infof(fmt.Sprintf("The %d th time updating status for %v: %s/%s, ", i, obj.Kind, obj.Namespace, obj.Name) +
			fmt.Sprintf("sequence No: %v->%v", obj.Status.ObservedGeneration, newStatus.ObservedGeneration))

		obj.Status = *newStatus

		updateErr = r.Status().Update(context.TODO(), obj)
		if updateErr == nil {
			return obj, nil
		}
		if i >= updateRetries {
			break
		}
		tmpObj := &appsv1alpha1.UnitedObject{}
		if getErr = r.Get(context.TODO(), client.ObjectKey{Namespace: obj.Namespace, Name: obj.Name}, tmpObj); getErr != nil {
			return nil, getErr
		}
		obj = tmpObj
	}

	klog.Errorf("fail to update unitedobject %s/%s status: %s", uo.Namespace, uo.Name, updateErr)
	return nil, updateErr
}

// NewUnitedObjectCondition creates a new UnitedObject condition.
func NewUnitedObjectCondition(condType appsv1alpha1.UnitedObjectConditionType, status corev1.ConditionStatus, reason, message string) *appsv1alpha1.UnitedObjectCondition {
	return &appsv1alpha1.UnitedObjectCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
}

// GetUnitedObjectCondition returns the condition with the provided type.
func GetUnitedObjectCondition(status appsv1alpha1.UnitedObjectStatus, condType appsv1alpha1.UnitedObjectConditionType) *appsv1alpha1.UnitedObjectCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// SetUnitedObjectCondition updates the UnitedObject to include the provided condition. If the condition that
// we are about to add already exists and has the same status, reason and message then we are not going to update.
func SetUnitedObjectCondition(status *appsv1alpha1.UnitedObjectStatus, condition *appsv1alpha1.UnitedObjectCondition) {
	currentCond := GetUnitedObjectCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason && currentCond.Message == condition.Message {
		return
	}

	if currentCond != nil && currentCond.Status == condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, *condition)
}

// RemoveUnitedObjectCondition removes the UnitedObject condition with the provided type.
func RemoveUnitedObjectCondition(status *appsv1alpha1.UnitedObjectStatus, condType appsv1alpha1.UnitedObjectConditionType) {
	status.Conditions = filterOutCondition(status.Conditions, condType)
}

func filterOutCondition(conditions []appsv1alpha1.UnitedObjectCondition, condType appsv1alpha1.UnitedObjectConditionType) []appsv1alpha1.UnitedObjectCondition {
	var newConditions []appsv1alpha1.UnitedObjectCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
