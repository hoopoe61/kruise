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
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	appsv1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var scheme = runtime.NewScheme()
var reconciler *UOReconciler
var mgrclose chan struct{}

func TestUOReconcile(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		" Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.LoggerTo(GinkgoWriter, true))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	Expect(appsv1alpha1.AddToScheme(scheme)).Should(BeNil())
	Expect(clientgoscheme.AddToScheme(scheme)).Should(BeNil())
	Expect(crdv1.AddToScheme(scheme)).Should(BeNil())
	Expect(appsv1alpha1.AddToScheme(scheme)).Should(BeNil())

	// +kubebuilder:scaffold:scheme

	By("Setting up kubernetes client")
	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	By("Creating Reconciler for appconfig")
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme, MetricsBindAddress: "0"})
	Expect(err).Should(BeNil())
	mgrclose = make(chan struct{})
	go mgr.Start(mgrclose)

	reconciler = &UOReconciler{
		Client: mgr.GetClient(),
		//used for discovery client
		Config:   mgr.GetConfig(),
		Recorder: mgr.GetEventRecorderFor("unitedobject-controller"),
		Scheme:   mgr.GetScheme(),
	}

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
	close(mgrclose)
})

type NotFoundMatcher struct {
}

// Match matches the api error.
func (matcher NotFoundMatcher) Match(actual interface{}) (success bool, err error) {
	if actual == nil {
		return false, nil
	}
	actualError := actual.(error)
	return apierrors.IsNotFound(actualError), nil
}

// FailureMessage builds an error message.
func (matcher NotFoundMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "to be not found")
}

// NegatedFailureMessage builds an error message.
func (matcher NotFoundMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "not to be not found")
}
