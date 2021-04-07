package unitedobject

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/controller/history"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	appsv1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	"github.com/openkruise/kruise/pkg/util/refmanager"
)

const (
	ControllerRevisionHashLabel = "controller.kubernetes.io/hash"
	defaultRevisionHistoryLimit = 10
)

func (r *UOReconciler) constructUnitedObjectRevisions(uo *appsv1alpha1.UnitedObject) (*appsv1.ControllerRevision, *appsv1.ControllerRevision, *[]*appsv1.ControllerRevision, int32, error) {
	var currentRevision, updateRevision *appsv1.ControllerRevision
	// Use a local copy of uo.Status.CollisionCount to avoid modifying uo.Status directly.
	var collisionCount int32
	if uo.Status.CollisionCount != nil {
		collisionCount = *uo.Status.CollisionCount
	}
	revisions, err := r.controlledHistories(uo)
	if err != nil {
		return currentRevision, updateRevision, nil, collisionCount, err
	}
	history.SortControllerRevisions(revisions)
	cleanedRevision, err := r.cleanExpiredRevision(uo, &revisions)
	if err != nil {
		return currentRevision, updateRevision, nil, collisionCount, err
	}
	revisions = *cleanedRevision

	// create a new revision from the current uo
	updateRevision, err = r.newRevision(uo, nextRevision(revisions), &collisionCount)
	if err != nil {
		return nil, nil, nil, collisionCount, err
	}

	// find any equivalent revisions
	equalRevisions := history.FindEqualRevisions(revisions, updateRevision)
	equalCount := len(equalRevisions)
	revisionCount := len(revisions)

	if equalCount > 0 {
		if history.EqualRevision(revisions[revisionCount-1], equalRevisions[equalCount-1]) {
			updateRevision = revisions[revisionCount-1]
		} else {
			// if the equivalent revision is not immediately prior we will roll back by incrementing the
			// Revision of the equivalent revision
			equalRevisions[equalCount-1].Revision = updateRevision.Revision
			err := r.Client.Update(context.TODO(), equalRevisions[equalCount-1])
			if err != nil {
				return nil, nil, nil, collisionCount, err
			}
			updateRevision = equalRevisions[equalCount-1]
		}
	} else {
		//if there is no equivalent revision we create a new one
		updateRevision, err = r.createControllerRevision(uo, updateRevision, &collisionCount)
		if err != nil {
			return nil, nil, nil, collisionCount, err
		}
	}

	// attempt to find the revision that corresponds to the current revision
	for i := range revisions {
		if revisions[i].Name == uo.Status.CurrentRevision {
			currentRevision = revisions[i]
		}
	}

	// if the current revision is nil we initialize the history by setting it to the update revision
	if currentRevision == nil {
		currentRevision = updateRevision
	}
	return currentRevision, updateRevision, &revisions, collisionCount, nil
}
func (r *UOReconciler) controlledHistories(uo *appsv1alpha1.UnitedObject) ([]*appsv1.ControllerRevision, error) {
	// List all histories to include those that don't match the selector anymore
	// but have a ControllerRef pointing to the controller.
	selector, err := metav1.LabelSelectorAsSelector(uo.Spec.Selector)
	if err != nil {
		return nil, err
	}
	// ControllerRevision object can be created or deleted, but cannot be updated
	histories := &appsv1.ControllerRevisionList{}
	err = r.Client.List(context.TODO(), histories, &client.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	klog.V(1).Infof("List controllerrevision of unitedobject %s/%s: count %d\n", uo.Namespace, uo.Name, len(histories.Items))

	// Use ControllerRefManager to adopt/orphan as needed.
	cm, err := refmanager.New(r.Client, uo.Spec.Selector, uo, r.Scheme)
	if err != nil {
		return nil, err
	}

	mts := make([]metav1.Object, len(histories.Items))
	for i, history := range histories.Items {
		mts[i] = history.DeepCopy()
	}

	claims, err := cm.ClaimOwnedObjects(mts)
	if err != nil {
		return nil, err
	}

	claimHistories := make([]*appsv1.ControllerRevision, len(claims))
	for i, mt := range claims {
		claimHistories[i] = mt.(*appsv1.ControllerRevision)
	}

	return claimHistories, nil
}
func (r *UOReconciler) cleanExpiredRevision(uo *appsv1alpha1.UnitedObject, sortedRevisions *[]*appsv1.ControllerRevision) (*[]*appsv1.ControllerRevision, error) {
	if uo.Spec.RevisionHistoryLimit == nil {
		uo.Spec.RevisionHistoryLimit = new(int32)
		*uo.Spec.RevisionHistoryLimit = defaultRevisionHistoryLimit
	}
	exceedNum := len(*sortedRevisions) - int(*uo.Spec.RevisionHistoryLimit)
	if exceedNum <= 0 {
		return sortedRevisions, nil
	}

	live := map[string]bool{}
	live[uo.Status.CurrentRevision] = true
	if len(uo.Status.UpdatedRevision) != 0 {
		live[uo.Status.UpdatedRevision] = true
	}

	for i, revision := range *sortedRevisions {
		if _, exist := live[revision.Name]; exist {
			continue
		}

		if i >= exceedNum {
			break
		}

		if err := r.Client.Delete(context.TODO(), revision); err != nil {
			return sortedRevisions, err
		}
	}
	cleanedRevisions := (*sortedRevisions)[exceedNum:]

	return &cleanedRevisions, nil
}

// createControllerRevision creates the controller revision owned by the parent.
func (r *UOReconciler) createControllerRevision(parent metav1.Object, revision *appsv1.ControllerRevision, collisionCount *int32) (*appsv1.ControllerRevision, error) {
	if collisionCount == nil {
		return nil, fmt.Errorf("collisionCount should not be nil")
	}
	// Clone the input
	clone := revision.DeepCopy()

	var err error
	// Continue to attempt to create the revision updating the name with a new hash on each iteration
	for {
		hash := history.HashControllerRevision(revision, collisionCount)
		// Update the revisions name
		clone.Name = history.ControllerRevisionName(parent.GetName(), hash)
		clone.Labels[history.ControllerRevisionHashLabel] = hash
		err = r.Client.Create(context.TODO(), clone)
		if errors.IsAlreadyExists(err) {
			exists := &appsv1.ControllerRevision{}
			err := r.Client.Get(context.TODO(), client.ObjectKey{Namespace: parent.GetNamespace(), Name: clone.Name}, exists)
			if err != nil {
				return nil, err
			}
			if bytes.Equal(exists.Data.Raw, clone.Data.Raw) {
				return exists, nil
			}
			*collisionCount++
			continue
		}
		return clone, err
	}
}

// newRevision creates a new ControllerRevision containing a patch that reapplies the target state of set.
// The Revision of the returned ControllerRevision is set to revision. If the returned error is nil, the returned
// ControllerRevision is valid. StatefulSet revisions are stored as patches that re-apply the current state of set
// to a new StatefulSet using a strategic merge patch to replace the saved state of the new StatefulSet.
func (r *UOReconciler) newRevision(uo *appsv1alpha1.UnitedObject, revision int64, collisionCount *int32) (*appsv1.ControllerRevision, error) {
	patch, err := getUnitedObjectPatch(uo)
	if err != nil {
		return nil, err
	}

	gvk, err := apiutil.GVKForObject(uo, r.Scheme)
	if err != nil {
		return nil, err
	}

	cr, err := history.NewControllerRevision(uo,
		gvk,
		map[string]string{},
		runtime.RawExtension{Raw: patch},
		revision,
		collisionCount)
	if err != nil {
		return nil, err
	}
	cr.Namespace = uo.Namespace

	return cr, nil
}

// nextRevision finds the next valid revision number based on revisions. If the length of revisions
// is 0 this is 1. Otherwise, it is 1 greater than the largest revision's Revision. This method
// assumes that revisions has been sorted by Revision.
func nextRevision(revisions []*appsv1.ControllerRevision) int64 {
	count := len(revisions)
	if count <= 0 {
		return 1
	}
	return revisions[count-1].Revision + 1
}

func getUnitedObjectPatch(uo *appsv1alpha1.UnitedObject) ([]byte, error) {
	dsBytes, err := json.Marshal(uo)
	if err != nil {
		return nil, err
	}
	var raw map[string]interface{}
	err = json.Unmarshal(dsBytes, &raw)
	if err != nil {
		return nil, err
	}
	objCopy := make(map[string]interface{})
	specCopy := make(map[string]interface{})

	// Create a patch of the UnitedObject that replaces spec.template and spec.instances
	spec := raw["spec"].(map[string]interface{})
	template := spec["template"].(map[string]interface{})
	template["$patch"] = "replace"
	instances := spec["instances"].([]interface{})
	//instances["$patch"] = "replace"
	specCopy["template"] = template
	specCopy["instances"] = instances
	objCopy["spec"] = specCopy
	patch, err := json.Marshal(objCopy)
	return patch, err
}
