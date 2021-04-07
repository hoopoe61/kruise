package unitedobject

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	appsv1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	"github.com/openkruise/kruise/pkg/util"
	"github.com/openkruise/kruise/pkg/util/refmanager"
)

func (r *UOReconciler) manageInstances(uo *appsv1alpha1.UnitedObject, expectedRevision string) (*appsv1alpha1.UnitedObjectStatus, error) {
	newStatus := uo.Status.DeepCopy()

	toCreateList, toDeleteList, toUpdateList, err := r.getInstancesList(uo, expectedRevision)
	if err != nil {
		return newStatus, err
	}
	var errs []error
	if len(toCreateList) > 0 {
		klog.V(0).Infof("unitedobject %s/%s needs to create instance with name: %v", uo.Namespace, uo.Name, toCreateList)
		if createErr := r.createInstances(uo, toCreateList, expectedRevision, newStatus); createErr != nil {
			errs = append(errs, createErr)
		}
	}
	if len(toDeleteList) > 0 {
		klog.V(0).Infof("unitedobject %s/%s needs to delete instance: %v", uo.Namespace, uo.Name, toDeleteList)
		if deleteErr := r.deleteInstances(uo, toDeleteList, expectedRevision, newStatus); deleteErr != nil {
			errs = append(errs, deleteErr)
		}
	}
	if len(toUpdateList) > 0 {
		klog.V(0).Infof("unitedobject %s/%s needs to update instance: %v", uo.Namespace, uo.Name, toUpdateList)
		if updateErr := r.updateInstances(uo, toUpdateList, expectedRevision, newStatus); updateErr != nil {
			errs = append(errs, updateErr)
		}
	}
	return newStatus, utilerrors.NewAggregate(errs)
}

func (r *UOReconciler) getAllInstances(uo *appsv1alpha1.UnitedObject) ([]*unstructured.Unstructured, error) {
	selector, err := metav1.LabelSelectorAsSelector(uo.Spec.Selector)
	if err != nil {
		return nil, err
	}
	objectIns := &unstructured.Unstructured{}
	if err := json.Unmarshal(uo.Spec.Template.Raw, objectIns); err != nil {
		klog.Errorf("fail to Unmarshal unitedobject, error is: %v", err)
		return nil, err
	}
	instanceList := &unstructured.UnstructuredList{}
	// only instances with the template's gvk will be selected
	instanceList.SetGroupVersionKind(objectIns.GetObjectKind().GroupVersionKind())
	err = r.Client.List(context.TODO(), instanceList, &client.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}

	manager, err := refmanager.New(r.Client, uo.Spec.Selector, uo, r.Scheme)
	if err != nil {
		return nil, err
	}

	v := reflect.ValueOf(instanceList).Elem().FieldByName("Items")
	selected := make([]*unstructured.Unstructured, v.Len())
	for i := 0; i < v.Len(); i++ {
		selected[i] = v.Index(i).Addr().Interface().(*unstructured.Unstructured)
	}
	mts := make([]metav1.Object, len(selected))
	for i, s := range selected {
		mts[i] = s.DeepCopy()
	}
	instances, err := manager.ClaimOwnedObjects(mts)
	if err != nil {
		return nil, err
	}
	claimedInstances := make([]*unstructured.Unstructured, len(instances))
	for i, ins := range instances {
		claimedInstances[i] = ins.(*unstructured.Unstructured)
	}

	return claimedInstances, nil
}

func (r *UOReconciler) getInstancesList(uo *appsv1alpha1.UnitedObject, expectedRevision string) ([]string, []*unstructured.Unstructured, []*unstructured.Unstructured, error) {
	var toCreateList []string
	var toDeleteList, toUpdateList []*unstructured.Unstructured
	expectList := sets.String{}
	for _, ins := range uo.Spec.Instances {
		expectList.Insert(ins.Name)
	}
	allInstances, err := r.getAllInstances(uo)
	if err != nil {
		r.Recorder.Event(uo.DeepCopy(), corev1.EventTypeWarning, fmt.Sprintf("Failed %s", eventTypeFindInstances), err.Error())
		return nil, nil, nil, err
	}
	nameToExistInstances := r.classifyInstanceByName(allInstances)

	for _, expectName := range expectList.List() {
		klog.V(2).Infof("expect instance :%v", expectName)
		if _, ok := nameToExistInstances[expectName]; ok {
			continue
		}
		klog.V(2).Infof("going to create instance with name :%v", expectName)
		toCreateList = append(toCreateList, expectName)
	}
	for existName, existInstances := range nameToExistInstances {
		klog.V(2).Infof("exist instance :%v", existName)
		if expectList.Has(existName) {
			equalFlag := false
			tmpCnt := 0
			for _, oneInstance := range existInstances {
				if getRevision(oneInstance) != expectedRevision {
					if tmpCnt < len(existInstances)-1 {
						tmpCnt++
						klog.V(2).Infof("going to delete instance :%v", oneInstance.GetName())
						toDeleteList = append(toDeleteList, oneInstance)
					} else {
						klog.V(2).Infof("going to update instance :%v", oneInstance.GetName())
						toUpdateList = append(toUpdateList, oneInstance)
					}
				} else {
					if !equalFlag {
						equalFlag = true
						klog.V(2).Infof("going to update instance :%v", oneInstance.GetName())
						toUpdateList = append(toUpdateList, oneInstance)
					} else {
						tmpCnt++
						klog.V(2).Infof("going to delete instance :%v", oneInstance.GetName())
						toDeleteList = append(toDeleteList, oneInstance)
					}
				}
			}
		} else {
			klog.V(2).Infof("going to delete instances with name :%v", existName)
			toDeleteList = append(toDeleteList, existInstances...)
		}
	}
	return toCreateList, toDeleteList, toUpdateList, nil
}

func (r *UOReconciler) classifyInstanceByName(instances []*unstructured.Unstructured) map[string][]*unstructured.Unstructured {
	nameToInstances := map[string][]*unstructured.Unstructured{}
	for _, ins := range instances {
		instanceName := ins.GetName()
		if len(instanceName) > 0 {
			nameToInstances[instanceName] = append(nameToInstances[instanceName], ins)
		}
	}
	return nameToInstances
}

func getRevision(object *unstructured.Unstructured) string {
	labels := object.GetLabels()
	if _, ok := labels[appsv1alpha1.ControllerRevisionHashLabelKey]; ok {
		return labels[appsv1alpha1.ControllerRevisionHashLabelKey]
	}
	return ""
}

func (r *UOReconciler) createInstances(uo *appsv1alpha1.UnitedObject, toCreateList []string, expectedRevision string, newStatus *appsv1alpha1.UnitedObjectStatus) error {
	var createdNum int
	var createdErr error
	createdNum, createdErr = util.SlowStartBatch(len(toCreateList), slowStartInitialBatchSize, func(idx int) error {
		instanceName := toCreateList[idx]
		objectIns := &unstructured.Unstructured{}
		if err := r.instantiation(uo, instanceName, expectedRevision, objectIns); err != nil {
			return fmt.Errorf("fail to instantiation instance %s: %s", instanceName, err.Error())
		}
		if err := r.Create(context.TODO(), objectIns); err != nil {
			return fmt.Errorf("fail to create instance %s: %s", instanceName, err.Error())
		}
		return nil
	})
	if createdErr == nil {
		SetUnitedObjectCondition(newStatus, NewUnitedObjectCondition(appsv1alpha1.InstanceCreated, corev1.ConditionTrue, "", ""))
		r.Recorder.Eventf(uo.DeepCopy(), corev1.EventTypeNormal, fmt.Sprintf("Success %s", appsv1alpha1.InstanceCreated), "Create %d instance", createdNum)
	} else {
		SetUnitedObjectCondition(newStatus, NewUnitedObjectCondition(appsv1alpha1.InstanceCreated, corev1.ConditionFalse, "Error", createdErr.Error()))
		r.Recorder.Eventf(uo.DeepCopy(), corev1.EventTypeWarning, fmt.Sprintf("Fail %s", appsv1alpha1.InstanceCreated), "Create %d instance", createdNum)
	}
	return createdErr
}

func (r *UOReconciler) deleteInstances(uo *appsv1alpha1.UnitedObject, toDeleteList []*unstructured.Unstructured, expectedRevision string, newStatus *appsv1alpha1.UnitedObjectStatus) error {
	var deleteNum int
	var deleteErr error
	deleteNum, deleteErr = util.SlowStartBatch(len(toDeleteList), slowStartInitialBatchSize, func(idx int) error {
		instance := toDeleteList[idx]

		if err := r.Delete(context.TODO(), instance); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("fail to delete instance %s: %s", instance.GetName(), err.Error())
		}
		return nil
	})
	if deleteErr == nil {
		SetUnitedObjectCondition(newStatus, NewUnitedObjectCondition(appsv1alpha1.InstanceDeleted, corev1.ConditionTrue, "", ""))
		r.Recorder.Eventf(uo.DeepCopy(), corev1.EventTypeNormal, fmt.Sprintf("Success %s", appsv1alpha1.InstanceDeleted), "Delete %d instance", deleteNum)
	} else {
		SetUnitedObjectCondition(newStatus, NewUnitedObjectCondition(appsv1alpha1.InstanceDeleted, corev1.ConditionFalse, "Error", deleteErr.Error()))
		r.Recorder.Eventf(uo.DeepCopy(), corev1.EventTypeWarning, fmt.Sprintf("Fail %s", appsv1alpha1.InstanceDeleted), "Delete %d instance", deleteNum)
	}
	return deleteErr
}

func (r *UOReconciler) updateInstances(uo *appsv1alpha1.UnitedObject, toUpdateList []*unstructured.Unstructured, expectedRevision string, newStatus *appsv1alpha1.UnitedObjectStatus) error {
	var updateNum int
	var updateErr error
	updateNum, updateErr = util.SlowStartBatch(len(toUpdateList), slowStartInitialBatchSize, func(idx int) error {
		instance := toUpdateList[idx]

		err := r.updateOneInstance(uo, instance, expectedRevision)
		if err != nil {
			return fmt.Errorf("fail to update instance %s: %s", instance.GetName(), err.Error())
		}
		return nil
	})
	if updateErr == nil {
		SetUnitedObjectCondition(newStatus, NewUnitedObjectCondition(appsv1alpha1.InstanceUpdated, corev1.ConditionTrue, "", ""))
		r.Recorder.Eventf(uo.DeepCopy(), corev1.EventTypeNormal, fmt.Sprintf("Success %s", appsv1alpha1.InstanceUpdated), "Update %d instance", updateNum)
	} else {
		SetUnitedObjectCondition(newStatus, NewUnitedObjectCondition(appsv1alpha1.InstanceUpdated, corev1.ConditionFalse, "Error", updateErr.Error()))
		r.Recorder.Eventf(uo.DeepCopy(), corev1.EventTypeWarning, fmt.Sprintf("Fail %s", appsv1alpha1.InstanceUpdated), "Update %d instance", updateNum)
	}
	return updateErr
}

func (r *UOReconciler) updateOneInstance(uo *appsv1alpha1.UnitedObject, instance *unstructured.Unstructured, expectedRevision string) error {
	var updateErr error
	for i := 0; i < updateRetries; i++ {
		if err := r.Get(context.TODO(), types.NamespacedName{Namespace: instance.GetNamespace(), Name: instance.GetName()}, instance); err != nil {
			return fmt.Errorf("fail to get instance %s: %s", instance.GetName(), err.Error())
		}
		objectIns := instance.DeepCopy()
		if err := r.instantiation(uo, instance.GetName(), expectedRevision, objectIns); err != nil {
			return fmt.Errorf("fail to instantiation instance %s: %s", instance.GetName(), err.Error())
		}
		bytes, err := objectIns.MarshalJSON()
		if err != nil {
			return err
		}
		// TODO: types.StrategicMergePatchType?
		if updateErr = r.Patch(context.TODO(), instance, client.RawPatch(types.MergePatchType, bytes)); updateErr == nil {
			break
		}
	}
	if updateErr != nil {
		return fmt.Errorf("fail to update instance %s: %s", instance.GetName(), updateErr.Error())
	}
	return nil
}

func (r *UOReconciler) instantiation(uo *appsv1alpha1.UnitedObject, instanceName, expectedRevision string, objectIns *unstructured.Unstructured) error {
	var instanceConfig *appsv1alpha1.Instance
	for _, instance := range uo.Spec.Instances {
		if instance.Name == instanceName {
			instanceConfig = &instance
			break
		}
	}
	if instanceConfig == nil {
		return fmt.Errorf("fail to find instance setting for %s", instanceName)
	}
	if err := json.Unmarshal(uo.Spec.Template.Raw, objectIns); err != nil {
		klog.Errorf("fail to Unmarshal unitedobject, error is: %v", err)
		return err
	}

	gvkRecord := objectIns.GetObjectKind().GroupVersionKind()
	objectIns.SetNamespace(uo.Namespace)

	jsonBytes, err := json.Marshal(objectIns)
	if err != nil {
		klog.Errorf("json Marshal error is: %v", err)
		return err
	}
	for _, iv := range instanceConfig.InstanceValues {
		jsonBytes, err = jsonOperation(jsonBytes, iv.FieldPath, iv.Value)
		if err != nil {
			klog.Errorf("jsonOperation error for iv :%v is: %v", iv, err)
			return err
		}
	}
	if err := json.Unmarshal(jsonBytes, objectIns); err != nil {
		klog.Errorf("json Unmarshal error is: %v", err)
		return err
	}
	// The gvk, namespace, name and ownerreference cannot be changed
	objectIns.SetGroupVersionKind(gvkRecord)
	objectIns.SetNamespace(uo.Namespace)
	objectIns.SetName(instanceName)
	if err := controllerutil.SetControllerReference(uo, objectIns, r.Scheme); err != nil {
		return err
	}
	var label, anno map[string]string
	if objectIns.GetLabels() == nil {
		label = map[string]string{}
	} else {
		label = objectIns.GetLabels()
	}
	for k, v := range uo.Spec.Selector.MatchLabels {
		// may override the setting in instanceValues
		label[k] = v
	}
	for k, v := range uo.ObjectMeta.Labels {
		if strings.HasPrefix(k, cascadingFlag) {
			label[k] = v
		}
	}
	label[appsv1alpha1.ControllerRevisionHashLabelKey] = expectedRevision
	label[appsv1alpha1.InstanceNameLabelKey] = instanceName
	objectIns.SetLabels(label)

	if objectIns.GetAnnotations() == nil {
		anno = map[string]string{}
	} else {
		anno = objectIns.GetAnnotations()
	}
	for k, v := range uo.ObjectMeta.Annotations {
		if strings.HasPrefix(k, cascadingFlag) {
			anno[k] = v
		}
	}
	if len(anno) > 0 {
		objectIns.SetAnnotations(anno)
	}

	return nil
}

func jsonOperation(jsonBytes []byte, path, value string) ([]byte, error) {
	if len(jsonBytes) == 0 || len(path) == 0 {
		return []byte(value), nil
	}
	patchJSON := []byte(`[{"op": "replace", "path": "`)
	path = strings.ReplaceAll(path, `\.`, `@@@DOTDOTDOT@@@`)
	path = strings.ReplaceAll(path, ".", "/")
	path = strings.ReplaceAll(path, `@@@DOTDOTDOT@@@`, `.`)
	if path[0] != '/' {
		patchJSON = append(patchJSON, '/')
	}
	value = strings.ReplaceAll(strings.ReplaceAll(value, `\"`, `"`), `\\`, `\`)
	if len(value) > 1 && value[0] == '"' && value[len(value)-1] == '"' {
		value = `"` + strings.ReplaceAll(strings.ReplaceAll(value[1:len(value)-1], `\`, `\\`), `"`, `\"`) + `"`
	}
	patchJSON = append(patchJSON, []byte(path+`", "value": `+value+`}]`)...)
	klog.V(2).Infof("patchJSON is: %s\n jsonBytes is: %s", patchJSON, jsonBytes)
	patch, err := jsonpatch.DecodePatch(patchJSON)
	if err != nil {
		return nil, err
	}
	return patch.Apply(jsonBytes)
}
