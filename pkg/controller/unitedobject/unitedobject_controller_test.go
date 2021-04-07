package unitedobject

import (
	"context"
	"errors"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
)

var _ = Describe("unitedobject reconcile test", func() {
	ctx := context.Background()
	ns := "default"
	name := "test"
	ls := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"uo-label-key": "uo-label-value",
		},
	}
	template := &unstructured.Unstructured{}
	template.SetAPIVersion("v1")
	template.SetKind("Service")
	template.SetNamespace(ns)
	template.SetName(name)
	template.SetLabels(map[string]string{"label-key1": "label-value1"})
	template.SetAnnotations(map[string]string{})
	port := map[string]interface{}{
		"name":       "test-port",
		"protocol":   "TCP",
		"port":       int64(12345),
		"targetPort": int64(8080),
	}
	unstructured.SetNestedSlice(template.Object, []interface{}{port}, "spec", "ports")

	templateBytes, _ := template.MarshalJSON()

	insName1 := "instance1"
	insName2 := "instance2"
	insName3 := "instance3"
	labelKey1 := "label-key1"
	labelValue1 := `"changed-label-value1"`
	labelKey2 := "label-key2"
	labelValue2 := `"label-value2"`
	annoKey1 := "annotation-key1"
	annoValue1 := `"changed-annotation-value1"`
	annoKey2 := "annotation-key2"
	annoValue2 := `"annotation-value2"`

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
	uoKey := client.ObjectKey{Name: name, Namespace: ns}
	insKey1 := client.ObjectKey{Name: insName1, Namespace: ns}
	insKey2 := client.ObjectKey{Name: insName2, Namespace: ns}
	insKey3 := client.ObjectKey{Name: insName3, Namespace: ns}
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
	req := reconcile.Request{NamespacedName: uoKey}
	var instance1, instance2, instance3 *v1.Service

	BeforeEach(func() {
		By("create unitedobject")
		uo := originUO.DeepCopy()
		Expect(k8sClient.Create(ctx, uo)).Should(BeNil())
		newuo := &appsv1alpha1.UnitedObject{}
		Expect(k8sClient.Get(ctx, uoKey, newuo)).Should(BeNil())
		instance1, instance2, instance3 = &v1.Service{}, &v1.Service{}, &v1.Service{}
	})
	AfterEach(func() {
		By("Clean up resources")
		Expect(k8sClient.Delete(ctx, originUO)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
		newuo := &appsv1alpha1.UnitedObject{}
		Expect(k8sClient.Get(ctx, uoKey, newuo)).Should(&NotFoundMatcher{})
	})
	It("test if instances are created", func() {
		By("reconcile and get intances")
		Eventually(func() error {
			err := k8sClient.Get(ctx, insKey1, instance1)
			if err != nil {
				reconciler.Reconcile(req)
				return err
			}
			err = k8sClient.Get(ctx, insKey2, instance2)
			if err != nil {
				reconciler.Reconcile(req)
				return err
			}
			return err
		}, time.Second, 300*time.Millisecond).Should(BeNil())
		logf.Log.Info("instance name", "instance1", instance1, "instance2", instance2)
		Expect(instance1.Name).Should(Equal(insName1))
		Expect(instance1.ObjectMeta.Labels[labelKey1]).Should(Equal(strings.Trim(labelValue1, "\"")))
		Expect(instance1.ObjectMeta.Annotations[annoKey1]).Should(Equal(strings.Trim(annoValue1, "\"")))
		Expect(instance2.Name).Should(Equal(insName2))
		Expect(instance2.ObjectMeta.Labels[labelKey2]).Should(Equal(strings.Trim(labelValue2, "\"")))
		Expect(instance2.ObjectMeta.Annotations[annoKey2]).Should(Equal(strings.Trim(annoValue2, "\"")))

		Expect(k8sClient.Delete(ctx, instance1)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
		Expect(k8sClient.Delete(ctx, instance2)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
	})

	It("test if instances are updated", func() {
		By("reconcile and get intances")
		Eventually(func() error {
			err := k8sClient.Get(ctx, insKey1, instance1)
			if err != nil {
				reconciler.Reconcile(req)
				return err
			}
			err = k8sClient.Get(ctx, insKey2, instance2)
			if err != nil {
				reconciler.Reconcile(req)
				return err
			}
			return err
		}, time.Second, 300*time.Millisecond).Should(BeNil())
		logf.Log.Info("instance name", "instance1", instance1, "instance2", instance2)
		By("update unitedobject")
		newuo := &appsv1alpha1.UnitedObject{}
		Expect(k8sClient.Get(ctx, uoKey, newuo)).Should(BeNil())
		newLabelValue := `"updated-label-value"`
		newAnnoValue := `"updated-annotation-value"`
		newuo.Spec.Instances[0].InstanceValues[0].Value = newLabelValue
		newuo.Spec.Instances[0].InstanceValues[1].Value = newAnnoValue
		newuo.Spec.Instances[1].InstanceValues[0].Value = newLabelValue
		newuo.Spec.Instances[1].InstanceValues[1].Value = newAnnoValue

		logf.Log.Info("newuo", "before update", newuo)
		Expect(k8sClient.Update(ctx, newuo)).Should(BeNil())
		Expect(k8sClient.Get(ctx, uoKey, newuo)).Should(BeNil())
		logf.Log.Info("newuo", "after update", newuo)
		reconciler.Reconcile(req)
		Eventually(func() error {
			err := k8sClient.Get(ctx, insKey1, instance1)
			if err != nil {
				reconciler.Reconcile(req)
				return err
			}
			err = k8sClient.Get(ctx, insKey2, instance2)
			if err != nil {
				reconciler.Reconcile(req)
				return err
			}
			if instance1.ObjectMeta.Labels[labelKey1] != strings.Trim(newLabelValue, "\"") || instance2.ObjectMeta.Labels[labelKey2] != strings.Trim(newLabelValue, "\"") {
				reconciler.Reconcile(req)
				return errors.New("instance not updated as expect")
			}
			return nil
		}, time.Second, 300*time.Millisecond).Should(BeNil())
		logf.Log.Info("instance name", "instance1", instance1, "instance2", instance2)
		Expect(instance1.Name).Should(Equal(insName1))
		Expect(instance1.ObjectMeta.Labels[labelKey1]).Should(Equal(strings.Trim(newLabelValue, "\"")))
		Expect(instance1.ObjectMeta.Annotations[annoKey1]).Should(Equal(strings.Trim(newAnnoValue, "\"")))
		Expect(instance2.Name).Should(Equal(insName2))
		Expect(instance2.ObjectMeta.Labels[labelKey2]).Should(Equal(strings.Trim(newLabelValue, "\"")))
		Expect(instance2.ObjectMeta.Annotations[annoKey2]).Should(Equal(strings.Trim(newAnnoValue, "\"")))

		Expect(k8sClient.Delete(ctx, instance1)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
		Expect(k8sClient.Delete(ctx, instance2)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
	})

	It("test if instances are replaced", func() {
		By("reconcile and get intances")
		Eventually(func() error {
			err := k8sClient.Get(ctx, insKey1, instance1)
			if err != nil {
				reconciler.Reconcile(req)
				return err
			}
			err = k8sClient.Get(ctx, insKey2, instance2)
			if err != nil {
				reconciler.Reconcile(req)
				return err
			}
			return err
		}, time.Second, 300*time.Millisecond).Should(BeNil())
		logf.Log.Info("instance name", "instance1", instance1, "instance2", instance2)
		By("update unitedobject")
		newuo := &appsv1alpha1.UnitedObject{}
		Expect(k8sClient.Get(ctx, uoKey, newuo)).Should(BeNil())
		newuo.Spec.Instances = []appsv1alpha1.Instance{
			{
				Name: insName1,
				InstanceValues: []appsv1alpha1.InstanceValue{
					insLabel1,
					insAnno1,
				},
			},
			{
				Name: insName3,
				InstanceValues: []appsv1alpha1.InstanceValue{
					insLabel1,
					insAnno1,
				},
			},
		}
		Expect(k8sClient.Update(ctx, newuo)).Should(BeNil())
		Eventually(func() error {
			err := k8sClient.Get(ctx, insKey3, instance3)
			if err != nil {
				reconciler.Reconcile(req)
				return err
			}
			return nil
		}, time.Second, 300*time.Millisecond).Should(BeNil())
		Expect(k8sClient.Get(ctx, insKey1, instance1)).Should(BeNil())
		Expect(k8sClient.Get(ctx, insKey2, instance2)).Should(&NotFoundMatcher{})

		Expect(k8sClient.Delete(ctx, instance1)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
		Expect(k8sClient.Delete(ctx, instance3)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
	})

	It("test if instances are deleted", func() {
		By("reconcile and get intances")
		Eventually(func() error {
			err := k8sClient.Get(ctx, insKey1, instance1)
			if err != nil {
				reconciler.Reconcile(req)
				return err
			}
			err = k8sClient.Get(ctx, insKey2, instance2)
			if err != nil {
				reconciler.Reconcile(req)
				return err
			}
			return err
		}, time.Second, 300*time.Millisecond).Should(BeNil())
		logf.Log.Info("instance name", "instance1", instance1, "instance2", instance2)
		By("update unitedobject")
		newuo := &appsv1alpha1.UnitedObject{}
		Expect(k8sClient.Get(ctx, uoKey, newuo)).Should(BeNil())
		newuo.Spec.Instances = []appsv1alpha1.Instance{
			{
				Name: insName1,
				InstanceValues: []appsv1alpha1.InstanceValue{
					insLabel1,
					insAnno1,
				},
			},
		}
		Expect(k8sClient.Update(ctx, newuo)).Should(BeNil())
		Eventually(func() error {
			err := k8sClient.Get(ctx, insKey2, instance2)
			if err == nil {
				reconciler.Reconcile(req)
			}
			return err
		}, time.Second, 300*time.Millisecond).Should(&NotFoundMatcher{})
		Expect(k8sClient.Get(ctx, insKey1, instance1)).Should(BeNil())

		Expect(k8sClient.Delete(ctx, instance1)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
	})
})
