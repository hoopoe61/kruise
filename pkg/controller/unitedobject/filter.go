package unitedobject

import (
	"reflect"

	appsv1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// UOPredicate describes the filter function for the update events
type UOPredicate struct {
	predicate.Funcs
}

// Update return false when only the status sections is changed
func (uop *UOPredicate) Update(e event.UpdateEvent) bool {
	oldUO, ok := e.ObjectOld.DeepCopyObject().(*appsv1alpha1.UnitedObject)
	if !ok {
		return false
	}
	newUO, ok := e.ObjectNew.DeepCopyObject().(*appsv1alpha1.UnitedObject)
	if !ok {
		return false
	}
	if !reflect.DeepEqual(oldUO.TypeMeta, newUO.TypeMeta) || !reflect.DeepEqual(oldUO.ObjectMeta, newUO.ObjectMeta) || !reflect.DeepEqual(oldUO.Spec, newUO.Spec) {
		return true
	}
	return false
}
