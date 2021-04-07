/*
Copyright 2019 The Kruise Authors.

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

package validating

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	unversionedvalidation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	apivalidation "k8s.io/kubernetes/pkg/apis/core/validation"

	appsv1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
)

// validateUnitedObjectSpec tests if required fields in the UnitedObject spec are set.
func validateUnitedObjectSpec(spec *appsv1alpha1.UnitedObjectSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if spec.Selector == nil {
		allErrs = append(allErrs, field.Required(fldPath.Child("selector"), ""))
		return allErrs
	}
	allErrs = append(allErrs, unversionedvalidation.ValidateLabelSelector(spec.Selector, fldPath.Child("selector"))...)
	if len(spec.Selector.MatchLabels)+len(spec.Selector.MatchExpressions) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), spec.Selector, "empty selector is invalid for unitedobject"))
		return allErrs
	}

	selector, err := metav1.LabelSelectorAsSelector(spec.Selector)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), spec.Selector, ""))
	} else {
		validateErrs, _ := validateInstanceTemplate(spec.Template, selector, fldPath.Child("template"))
		allErrs = append(allErrs, validateErrs...)
	}

	// check name of instances
	instanceNames := sets.String{}
	for i, instance := range spec.Instances {
		if len(instance.Name) == 0 {
			allErrs = append(allErrs, field.Required(fldPath.Child("instances").Index(i).Child("name"), ""))
			continue
		}
		if instanceNames.Has(instance.Name) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("instances").Index(i).Child("name"), instance.Name, fmt.Sprintf("duplicated instance name %s", instance.Name)))
		}
		if errs := apimachineryvalidation.NameIsDNSLabel(instance.Name, false); len(errs) > 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("instances").Index(i).Child("name"), instance.Name, fmt.Sprintf("invalid instance name %s", strings.Join(errs, ", "))))
		}

		instanceNames.Insert(instance.Name)
	}

	return allErrs
}

// validateUnitedObject validates a UnitedObject.
func validateUnitedObject(unitedObj *appsv1alpha1.UnitedObject) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMeta(&unitedObj.ObjectMeta, true, apimachineryvalidation.NameIsDNSSubdomain, field.NewPath("metadata"))
	allErrs = append(allErrs, validateUnitedObjectSpec(&unitedObj.Spec, field.NewPath("spec"))...)
	return allErrs
}

// ValidateUnitedObjectUpdate tests if required fields in the UnitedObject are set.
func ValidateUnitedObjectUpdate(unitedObj, oldUnitedObj *appsv1alpha1.UnitedObject) field.ErrorList {
	allErrs := apivalidation.ValidateObjectMetaUpdate(&unitedObj.ObjectMeta, &oldUnitedObj.ObjectMeta, field.NewPath("metadata"))
	allErrs = append(allErrs, validateInstanceTemplateUpdate(unitedObj.Spec.Template, oldUnitedObj.Spec.Template, field.NewPath("spec").Child("template"))...)
	return allErrs
}

func validateInstanceTemplateUpdate(template, oldTemplate *runtime.RawExtension, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if template != nil && oldTemplate != nil {
		// The gvk, namespace cannot be changed
		objectIns := &unstructured.Unstructured{}
		if err := json.Unmarshal(template.Raw, objectIns); err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath, string(template.Raw), fmt.Sprintf("Convert_Template_To_Unstructured failed: %v", err)))
			return allErrs
		}
		oldObjectIns := &unstructured.Unstructured{}
		if err := json.Unmarshal(oldTemplate.Raw, oldObjectIns); err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath, string(template.Raw), fmt.Sprintf("Convert_Template_To_Unstructured failed: %v", err)))
			return allErrs
		}
		if !reflect.DeepEqual(objectIns.GroupVersionKind(), oldObjectIns.GroupVersionKind()) || objectIns.GetNamespace() != oldObjectIns.GetNamespace() {
			allErrs = append(allErrs, field.Invalid(fldPath, string(template.Raw), "the gvk and namespace of the template cannot be changed"))
			return allErrs
		}
	}
	return allErrs
}

func validateInstanceTemplate(template *runtime.RawExtension, selector labels.Selector, fldPath *field.Path) (field.ErrorList, error) {
	type onlyMD struct {
		metav1.ObjectMeta `json:"metadata,omitempty"`
	}
	allErrs := field.ErrorList{}

	if len(template.Raw) == 0 {
		allErrs = append(allErrs, field.Required(fldPath, "should provide template in spec"))
		return allErrs, nil
	}

	objectIns := &unstructured.Unstructured{}
	if err := json.Unmarshal(template.Raw, objectIns); err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, string(template.Raw), fmt.Sprintf("Convert_Template_To_Unstructured failed: %v", err)))
		return allErrs, err
	}

	if len(objectIns.GetAPIVersion()) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("apiVersion"), objectIns.GetAPIVersion(), "should provide apiVersion in template"))
	}
	if len(objectIns.GetKind()) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("kind"), objectIns.GetKind(), "should provide kind in template"))
	}

	md := &onlyMD{}
	if err := json.Unmarshal(template.Raw, md); err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("metadata"), string(template.Raw), fmt.Sprintf("Convert_MetaData_In_Template_To_MetaData failed: %v", err)))
		return allErrs, err
	}
	if len(md.GetNamespace()) > 0 {
		allErrs = append(allErrs, apivalidation.ValidateObjectMeta(&md.ObjectMeta, true, apimachineryvalidation.NameIsDNSSubdomain, fldPath.Child("metadata"))...)
	} else {
		allErrs = append(allErrs, apivalidation.ValidateObjectMeta(&md.ObjectMeta, false, apimachineryvalidation.NameIsDNSSubdomain, fldPath.Child("metadata"))...)
	}
	labels := labels.Set(objectIns.GetLabels())
	if !selector.Matches(labels) {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("metadata", "labels"), objectIns.GetLabels(), "`selector` does not match template `labels`"))
	}

	return allErrs, nil
}
