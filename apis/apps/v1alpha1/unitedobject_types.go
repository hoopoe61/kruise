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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// UnitedServiceConditionType indicates valid conditions type of a UnitedService.
type UnitedObjectConditionType string

const (
	// InstanceNameLabelKey is used to record the name of current instance
	InstanceNameLabelKey = "apps.kruise.io/instance-name"

	// InstanceProvisioned means all the expected instances are provisioned and unexpected instances are deleted
	InstanceProvisioned UnitedObjectConditionType = "InstanceProvisioned"
	// InstanceCreated means all expected instances have been created
	InstanceCreated UnitedObjectConditionType = "InstanceCreated"
	// InstanceDeleted means all unexpected instances have been deleted
	InstanceDeleted UnitedObjectConditionType = "InstanceDeleted"
	// InstanceUpdated means all instances have been updated to expect status
	InstanceUpdated UnitedObjectConditionType = "InstanceUpdated"
	// InstanceFailure is added to a UnitedService when one of its instances has failure during its own reconciling
	InstanceFailure UnitedObjectConditionType = "InstanceFailure"
)

// UnitedObjectSpec defines the desired state of UnitedObject
type UnitedObjectSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Selector is a label query over objects that should match the specification in intances
	// It must match the pod template's labels
	Selector *metav1.LabelSelector `json:"selector"`

	// Template describes the instance that will be created
	Template *runtime.RawExtension `json:"template"`

	// Customization describes the instantiation detail of each instances
	Instances []Instance `json:"instances"`

	// Indicates the number of histories to be conserved
	// If unspecified, defaults to 10
	// +optional
	RevisionHistoryLimit *int32 `json:"revisionHistoryLimit,omitempty"`
}
type Instance struct {
	// Name of the instances
	Name string `json:"name"`
	// Describe the instantition values of an instance
	InstanceValues []InstanceValue `json:"instanceValues,omitempty"`
}
type InstanceValue struct {
	// the path of the object, for example: spec.selector.app
	FieldPath string `json:"fieldPath"`
	// the value for FieldPath
	Value string `json:"value"`
}

// UnitedObjectStatus defines the observed state of UnitedObject
type UnitedObjectStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// The number of created instances.
	// +optional
	ExistInstances int32 `json:"existInstances,omitempty"`

	// Instances is the most recently observed number of replicas.
	Instances int32 `json:"instances"`

	// The number of instances in current version.
	UpdatedInstances int32 `json:"updatedInstances"`

	CollisionCount *int32 `json:"collisionCount,omitempty"`

	// CurrentRevision, if not empty, indicates the current version of the UnitedService.
	CurrentRevision string `json:"currentRevision"`

	// Represents the latest available observations of a UnitedService's current state.
	// +optional
	Conditions []UnitedObjectCondition `json:"conditions,omitempty"`

	UpdatedRevision string `json:"updatedRevision,omitempty"`
}
type UnitedObjectCondition struct {
	// Type of in place set condition.
	Type UnitedObjectConditionType `json:"type,omitempty"`

	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status,omitempty"`

	// Last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`

	// The reason for the condition's last transition.
	Reason string `json:"reason,omitempty"`

	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true

// UnitedObject is the Schema for the unitedobjects API
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=uo
// +kubebuilder:printcolumn:name="DESIRED",type="integer",JSONPath=".status.instances",description="The desired number of instances."
// +kubebuilder:printcolumn:name="CURRENT",type="integer",JSONPath=".status.existInstances",description="The number of all currently existing instances."
// +kubebuilder:printcolumn:name="UPDATED",type="integer",JSONPath=".status.updatedInstances",description="The number of updated instances."
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp",description="CreationTimestamp is a timestamp representing the server time when this object was created. It is not guaranteed to be set in happens-before order across separate operations. Clients may not set this value. It is represented in RFC3339 form and is in UTC."
type UnitedObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UnitedObjectSpec   `json:"spec,omitempty"`
	Status UnitedObjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// UnitedObjectList contains a list of UnitedObject
type UnitedObjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnitedObject `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnitedObject{}, &UnitedObjectList{})
}
