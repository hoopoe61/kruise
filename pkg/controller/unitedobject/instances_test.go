package unitedobject

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("get all instances", func() {
	ctx := context.Background()
	ns := "default"
	name := "test-uo"
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

	insName1 := "instance-1"
	insName2 := "instance-2"
	insName3 := "instance-3"
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

	BeforeEach(func() {
		By("create unitedobject")
		uo := originUO.DeepCopy()
		Expect(k8sClient.Create(ctx, uo)).Should(BeNil())
		newuo := &appsv1alpha1.UnitedObject{}
		Expect(k8sClient.Get(ctx, uoKey, newuo)).Should(BeNil())
	})
	AfterEach(func() {
		By("Clean up resources")
		Expect(k8sClient.Delete(ctx, originUO)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
		newuo := &appsv1alpha1.UnitedObject{}
		Expect(k8sClient.Get(ctx, uoKey, newuo)).Should(&NotFoundMatcher{})
	})

	It("test func getAllInstances", func() {
		uo := &appsv1alpha1.UnitedObject{}
		Expect(k8sClient.Get(ctx, uoKey, uo)).Should(BeNil())
		instance1 := template.DeepCopy()
		instance1.SetName(insName1)
		instance1.SetLabels(ls.MatchLabels)
		Expect(k8sClient.Create(ctx, instance1)).Should(BeNil())
		Expect(k8sClient.Get(ctx, insKey1, instance1)).Should(BeNil())

		instance2 := template.DeepCopy()
		instance2.SetName(insName2)
		instance2.SetLabels(ls.MatchLabels)
		Expect(k8sClient.Create(ctx, instance2)).Should(BeNil())
		Expect(k8sClient.Get(ctx, insKey2, instance2)).Should(BeNil())

		allInstances, err := reconciler.getAllInstances(uo)
		Expect(err).Should(BeNil())
		Expect(len(allInstances)).Should(Equal(2))

		if allInstances[0].GetObjectKind().GroupVersionKind() != instance1.GetObjectKind().GroupVersionKind() ||
			allInstances[0].GetNamespace() != instance1.GetNamespace() || allInstances[0].GetName() != instance1.GetName() {
			Expect(allInstances[0].GetObjectKind().GroupVersionKind()).Should(Equal(instance2.GetObjectKind().GroupVersionKind()))
			Expect(allInstances[0].GetNamespace()).Should(Equal(instance2.GetNamespace()))
			Expect(allInstances[0].GetName()).Should(Equal(instance2.GetName()))

			Expect(allInstances[1].GetObjectKind().GroupVersionKind()).Should(Equal(instance1.GetObjectKind().GroupVersionKind()))
			Expect(allInstances[1].GetNamespace()).Should(Equal(instance1.GetNamespace()))
			Expect(allInstances[1].GetName()).Should(Equal(instance1.GetName()))
		} else {
			Expect(allInstances[1].GetObjectKind().GroupVersionKind()).Should(Equal(instance2.GetObjectKind().GroupVersionKind()))
			Expect(allInstances[1].GetNamespace()).Should(Equal(instance2.GetNamespace()))
			Expect(allInstances[1].GetName()).Should(Equal(instance2.GetName()))
		}

		Expect(k8sClient.Delete(ctx, instance1)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
		Expect(k8sClient.Delete(ctx, instance2)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
	})

	It("test func getInstancesList", func() {
		uo := &appsv1alpha1.UnitedObject{}
		Expect(k8sClient.Get(ctx, uoKey, uo)).Should(BeNil())
		instance1 := template.DeepCopy()
		instance1.SetName(insName1)
		instance1.SetLabels(ls.MatchLabels)
		Expect(k8sClient.Create(ctx, instance1)).Should(BeNil())
		Expect(k8sClient.Get(ctx, insKey1, instance1)).Should(BeNil())

		instance2 := template.DeepCopy()
		instance2.SetName(insName2)
		instance2.SetLabels(ls.MatchLabels)
		Expect(k8sClient.Create(ctx, instance2)).Should(BeNil())
		Expect(k8sClient.Get(ctx, insKey2, instance2)).Should(BeNil())

		expectedRevision := "newRevision"
		uo.Spec.Instances = []appsv1alpha1.Instance{
			{
				Name:           insName1,
				InstanceValues: []appsv1alpha1.InstanceValue{},
			},
			{
				Name:           insName3,
				InstanceValues: []appsv1alpha1.InstanceValue{},
			},
		}

		toCreateList, toDeleteList, toUpdateList, err := reconciler.getInstancesList(uo, expectedRevision)
		Expect(err).Should(BeNil())
		Expect(len(toCreateList)).Should(Equal(1))
		Expect(toCreateList[0]).Should(Equal(insName3))
		Expect(len(toDeleteList)).Should(Equal(1))
		Expect(toDeleteList[0].GetName()).Should(Equal(insName2))
		Expect(len(toUpdateList)).Should(Equal(1))
		Expect(toUpdateList[0].GetName()).Should(Equal(insName1))

		Expect(k8sClient.Delete(ctx, uo)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
		Expect(k8sClient.Delete(ctx, instance1)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
		Expect(k8sClient.Delete(ctx, instance2)).Should(SatisfyAny(BeNil(), &NotFoundMatcher{}))
	})

	It("test func instantiation", func() {
		uo := &appsv1alpha1.UnitedObject{}
		Expect(k8sClient.Get(ctx, uoKey, uo)).Should(BeNil())

		cascadingKey := cascadingFlag + "cascadingKey"
		cascadingValue := "cascadingValue"
		notAllowdInstanceValues := []appsv1alpha1.InstanceValue{
			{
				FieldPath: "apiVersion",
				Value:     `"test-apiVersion"`,
			},
			{
				FieldPath: "kind",
				Value:     `"test-kind"`,
			},
			{
				FieldPath: "metadata.name",
				Value:     `"test-name"`,
			},
			{
				FieldPath: "metadata.namespace",
				Value:     `"test-namespace"`,
			},
			{
				FieldPath: "metadata.labels." + strings.ReplaceAll(strings.ReplaceAll(appsv1alpha1.ControllerRevisionHashLabelKey, ".", `\.`), "/", "~1"),
				Value:     `"test-resvison"`,
			},
			{
				FieldPath: "metadata.labels." + strings.ReplaceAll(strings.ReplaceAll(appsv1alpha1.InstanceNameLabelKey, ".", `\.`), "/", "~1"),
				Value:     `"test-instancename"`,
			},
			{
				FieldPath: "metadata.annotations." + strings.ReplaceAll(strings.ReplaceAll(cascadingKey, ".", `\.`), "/", "~1"),
				Value:     `"` + cascadingValue + `"`,
			},
		}
		uo.Spec.Instances = []appsv1alpha1.Instance{
			{
				Name:           insName1,
				InstanceValues: notAllowdInstanceValues,
			},
		}
		currentRevision, updatedRevision, _, _, err := reconciler.constructUnitedObjectRevisions(uo)
		Expect(err).Should(BeNil())
		expectedRevision := currentRevision.Name
		if updatedRevision != nil {
			expectedRevision = updatedRevision.Name
		}
		instance1 := &unstructured.Unstructured{}
		Expect(reconciler.instantiation(uo, insName1, expectedRevision, instance1)).Should(BeNil())
		Expect(instance1.GetObjectKind().GroupVersionKind()).Should(Equal(template.GetObjectKind().GroupVersionKind()))
		Expect(instance1.GetNamespace()).Should(Equal(ns))
		Expect(instance1.GetName()).Should(Equal(insName1))
		Expect(instance1.GetLabels()[appsv1alpha1.ControllerRevisionHashLabelKey]).Should(Equal(expectedRevision))
		Expect(instance1.GetLabels()[appsv1alpha1.InstanceNameLabelKey]).Should(Equal(insName1))
		Expect(instance1.GetAnnotations()[cascadingKey]).Should(Equal(cascadingValue))
	})
})
