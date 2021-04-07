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
	"fmt"
	"reflect"
	"testing"

	appsv1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestValidateInstanceTemplate(t *testing.T) {
	ns := "default"
	name := "test"
	lsKey := "uo-label-key"
	lsValue := "uo-label-value"
	objLsKey := "obj-label-key"
	objLsValue := "obj-label-value"
	insName1 := "instance1"
	insName2 := "instance2"

	labelKey1 := "label-key1"
	labelValue1 := `"changed-label-value1"`
	labelKey2 := "label-key2"
	labelValue2 := `"label-value2"`
	annoKey1 := "annotation-key1"
	annoValue1 := `"changed-annotation-value1"`
	annoKey2 := "annotation-key2"
	annoValue2 := `"annotation-value2"`

	ls := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			lsKey: lsValue,
		},
	}
	insLabel1 := appsv1alpha1.InstanceValue{
		FieldPath: "metadata.labels." + labelKey1,
		Value:     labelValue1,
	}
	insLabel2 := appsv1alpha1.InstanceValue{
		FieldPath: "metadata.labels." + labelKey2,
		Value:     labelValue2,
	}
	insAnno1 := appsv1alpha1.InstanceValue{
		FieldPath: "metadata.annotations." + annoKey1,
		Value:     annoValue1,
	}
	insAnno2 := appsv1alpha1.InstanceValue{
		FieldPath: "metadata.annotations." + annoKey2,
		Value:     annoValue2,
	}
	instances := []appsv1alpha1.Instance{
		{
			Name: insName1,
			InstanceValues: []appsv1alpha1.InstanceValue{
				insLabel1,
				insAnno1,
			},
		},
		{
			Name: insName2,
			InstanceValues: []appsv1alpha1.InstanceValue{
				insLabel2,
				insAnno2,
			},
		},
	}
	template := &unstructured.Unstructured{}
	template.SetAPIVersion("v1")
	template.SetKind("Service")
	template.SetNamespace(ns)
	template.SetName(name)
	template.SetLabels(map[string]string{lsKey: lsValue, objLsKey: objLsValue})
	template.SetAnnotations(map[string]string{})
	port := map[string]interface{}{
		"name":       "test-port",
		"protocol":   "TCP",
		"port":       int64(12345),
		"targetPort": int64(8080),
	}
	err := unstructured.SetNestedSlice(template.Object, []interface{}{port}, "spec", "ports")
	if err != nil {
		t.Fatalf("Fail to set port for template, error :%v", err)
	}

	templateBytes, err := template.MarshalJSON()
	if err != nil {
		t.Fatalf("Fail to marshal template to json, error :%v", err)
	}

	originUO := &appsv1alpha1.UnitedObject{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: appsv1alpha1.UnitedObjectSpec{
			Selector: ls,
			Template: &runtime.RawExtension{
				Raw: templateBytes,
			},
			Instances: instances,
		},
	}

	fldPath := field.NewPath("spec").Child("template")
	testCases := map[string]map[*appsv1alpha1.UnitedObject]field.ErrorList{}
	testCases["correct unitedobject"] = map[*appsv1alpha1.UnitedObject]field.ErrorList{
		originUO: {},
	}

	uo := originUO.DeepCopy()
	uo.Spec.Template.Raw = []byte{}
	testCases["unitedobject with empty template"] = map[*appsv1alpha1.UnitedObject]field.ErrorList{
		uo: {field.Required(fldPath, "should provide template in spec")},
	}

	uo = originUO.DeepCopy()
	uo.Spec.Template.Raw = uo.Spec.Template.Raw[0 : len(uo.Spec.Template.Raw)/2]
	testCases["unitedobject with invaild template"] = map[*appsv1alpha1.UnitedObject]field.ErrorList{
		uo: {field.Invalid(fldPath, string(uo.Spec.Template.Raw), "Convert_Template_To_Unstructured failed: ")},
	}

	uo = originUO.DeepCopy()
	uo.Spec.Template.Raw = []byte(`{"apiVersion":"","kind":"testkind","metadata":{"annotations":{},"labels":{"` + objLsKey + `":"` + objLsValue + `"},"name":"test","namespace":"default"},"spec":{"ports":[{"name":"test-port","port":12345,"protocol":"TCP","targetPort":8080}]}}`)
	testCases["unitedobject with invaild gvk and metadata"] = map[*appsv1alpha1.UnitedObject]field.ErrorList{
		uo: {field.Invalid(fldPath.Child("apiVersion"), "", "should provide apiVersion in template"),
			field.Invalid(fldPath.Child("metadata", "labels"), map[string]string{objLsKey: objLsValue}, "`selector` does not match template `labels`")},
	}

	for k, v := range testCases {
		for kv, vv := range v {
			selector, err := metav1.LabelSelectorAsSelector(kv.Spec.Selector)
			if err != nil {
				t.Fatalf("test case: %s, Invaild label selector in spec, error: %v", k, err)
			}
			validateErrs, err := validateInstanceTemplate(kv.Spec.Template, selector, fldPath)
			if len(vv) != len(validateErrs) {
				t.Fatalf("test case: %s, expect errors' length: %v, receive errors' length: %v", k, len(vv), len(validateErrs))
			}
			for i := range vv {
				if err != nil && i == len(vv)-1 {
					vv[i].Detail += err.Error()
				}
				if !reflect.DeepEqual(vv[i], validateErrs[i]) {
					t.Errorf("test case: %s, expect error: %v, receive errors: %v", k, vv[i], validateErrs[i])
				}
			}
		}
	}
}

func TestValidateUnitedObjectSpec(t *testing.T) {
	// tests for settings expect template in spec
	ns := "default"
	name := "test"
	lsKey := "uo-label-key"
	lsValue := "uo-label-value"
	insName1 := "instance1"
	ls := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			lsKey: lsValue,
		},
	}
	templateBytes := []byte(`{"apiVersion":"v1","kind":"Service","metadata":{"annotations":{},"labels":{"` + lsKey + `":"` + lsValue + `"},"name":"test","namespace":"default"},"spec":{"ports":[{"name":"test-port","port":12345,"protocol":"TCP","targetPort":8080}]}}`)
	originUO := &appsv1alpha1.UnitedObject{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: appsv1alpha1.UnitedObjectSpec{
			Selector: ls,
			Template: &runtime.RawExtension{
				Raw: templateBytes,
			},
			Instances: []appsv1alpha1.Instance{
				{
					Name: insName1,
				},
			},
		},
	}

	fldPath := field.NewPath("spec")
	testCases := map[string]map[*appsv1alpha1.UnitedObject]field.ErrorList{}
	testCases["correct unitedobject"] = map[*appsv1alpha1.UnitedObject]field.ErrorList{
		originUO: {},
	}

	uo := originUO.DeepCopy()
	uo.Spec.Selector = nil
	testCases["unitedobject with no selector"] = map[*appsv1alpha1.UnitedObject]field.ErrorList{
		uo: {field.Required(fldPath.Child("selector"), "")},
	}

	uo = originUO.DeepCopy()
	uo.Spec.Selector = &metav1.LabelSelector{}
	testCases["unitedobject with empty selector"] = map[*appsv1alpha1.UnitedObject]field.ErrorList{
		uo: {field.Invalid(fldPath.Child("selector"), uo.Spec.Selector, "empty selector is invalid for unitedobject")},
	}

	uo = originUO.DeepCopy()
	i := 0
	uo.Spec.Instances[i].Name = ""
	testCases["unitedobject with empty instances"] = map[*appsv1alpha1.UnitedObject]field.ErrorList{
		uo: {field.Required(fldPath.Child("instances").Index(i).Child("name"), "")},
	}

	uo = originUO.DeepCopy()
	uo.Spec.Instances = []appsv1alpha1.Instance{
		{
			Name: insName1,
		},
		{
			Name: insName1,
		},
	}
	i = 1
	testCases["unitedobject with duplicate instances"] = map[*appsv1alpha1.UnitedObject]field.ErrorList{
		uo: {field.Invalid(fldPath.Child("instances").Index(i).Child("name"), uo.Spec.Instances[i].Name, fmt.Sprintf("duplicated instance name %s", uo.Spec.Instances[i].Name))},
	}

	for k, v := range testCases {
		for kv, vv := range v {
			validateErrs := validateUnitedObjectSpec(&kv.Spec, fldPath)
			if len(vv) != len(validateErrs) {
				t.Fatalf("test case: %s, expect errors' length: %v, receive errors' length: %v", k, len(vv), len(validateErrs))
			}
			for i := range vv {
				if !reflect.DeepEqual(vv[i], validateErrs[i]) {
					t.Errorf("test case: %s, expect error: %v, receive errors: %v", k, vv[i], validateErrs[i])
				}
			}
		}
	}
}

func TestValidateInstanceTemplateUpdate(t *testing.T) {
	ns := "default"
	name := "test"
	lsKey := "uo-label-key"
	lsValue := "uo-label-value"
	template := &unstructured.Unstructured{}
	template.SetAPIVersion("v1")
	template.SetKind("Service")
	template.SetNamespace(ns)
	template.SetName(name)
	template.SetLabels(map[string]string{lsKey: lsValue})
	template.SetAnnotations(map[string]string{})
	port := map[string]interface{}{
		"name":       "test-port",
		"protocol":   "TCP",
		"port":       int64(12345),
		"targetPort": int64(8080),
	}
	err := unstructured.SetNestedSlice(template.Object, []interface{}{port}, "spec", "ports")
	if err != nil {
		t.Fatalf("Fail to set port for template, error :%v", err)
	}
	templateBytes, err := template.MarshalJSON()
	if err != nil {
		t.Fatalf("Fail to marshal template to json, error :%v", err)
	}
	originRE := &runtime.RawExtension{
		Raw: templateBytes,
	}

	fldPath := field.NewPath("spec").Child("template")
	type templateStuct struct {
		oldTemplate *runtime.RawExtension
		newTemplate *runtime.RawExtension
	}
	testCases := map[string]map[templateStuct]field.ErrorList{}
	oldTemplate := originRE.DeepCopy()
	newTemplate := originRE.DeepCopy()
	tpl := template.DeepCopy()
	tpl.SetAPIVersion("newapiVersion")
	templateBytes, err = tpl.MarshalJSON()
	if err != nil {
		t.Fatalf("Fail to marshal template to json, error :%v", err)
	}
	errStr := "the gvk and namespace of the template cannot be changed"
	newTemplate.Raw = templateBytes
	testCases["unitedobject with changed apiVersion"] = map[templateStuct]field.ErrorList{
		{oldTemplate: oldTemplate, newTemplate: newTemplate}: {field.Invalid(fldPath, string(newTemplate.Raw), errStr)},
	}

	newTemplate = originRE.DeepCopy()
	tpl = template.DeepCopy()
	tpl.SetKind("newKind")
	templateBytes, err = tpl.MarshalJSON()
	if err != nil {
		t.Fatalf("Fail to marshal template to json, error :%v", err)
	}
	newTemplate.Raw = templateBytes
	testCases["unitedobject with changed Kind"] = map[templateStuct]field.ErrorList{
		{oldTemplate: oldTemplate, newTemplate: newTemplate}: {field.Invalid(fldPath, string(newTemplate.Raw), errStr)},
	}

	newTemplate = originRE.DeepCopy()
	tpl = template.DeepCopy()
	tpl.SetNamespace("newNamespace")
	templateBytes, err = tpl.MarshalJSON()
	if err != nil {
		t.Fatalf("Fail to marshal template to json, error :%v", err)
	}
	newTemplate.Raw = templateBytes
	testCases["unitedobject with changed namespace"] = map[templateStuct]field.ErrorList{
		{oldTemplate: oldTemplate, newTemplate: newTemplate}: {field.Invalid(fldPath, string(newTemplate.Raw), errStr)},
	}

	for k, v := range testCases {
		for kv, vv := range v {
			validateErrs := validateInstanceTemplateUpdate(kv.newTemplate, kv.oldTemplate, fldPath)
			if len(vv) != len(validateErrs) {
				t.Fatalf("test case: %s, expect errors' length: %v, receive errors' length: %v", k, len(vv), len(validateErrs))
			}
			for i := range vv {
				if !reflect.DeepEqual(vv[i], validateErrs[i]) {
					t.Errorf("test case: %s, expect error: %v, receive errors: %v", k, vv[i], validateErrs[i])
				}
			}
		}
	}
}
