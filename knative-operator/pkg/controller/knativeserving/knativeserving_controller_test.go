package knativeserving

import (
	"context"
	"os"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"knative.dev/pkg/apis/istio/v1alpha3"
	"knative.dev/serving-operator/pkg/apis/serving/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var (
	defaultKnativeServing = v1alpha1.KnativeServing{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "knative-serving",
			Namespace: "knative-serving",
		},
	}
	defaultIngress = configv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: configv1.IngressSpec{
			Domain: "example.com",
		},
	}

	defaultVirtualService = v1alpha3.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vsName",
			Namespace: "vsNamespace",
		},
	}

	defaultRequest = reconcile.Request{
		NamespacedName: types.NamespacedName{Namespace: "knative-serving", Name: "knative-serving"},
	}

	defaultKnRoute = routev1.Route{
		TypeMeta: metav1.TypeMeta{
			Kind: "ConsoleCLIDownload",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kn-cli-downloads",
			Namespace: "knative-serving",
		},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				routev1.RouteIngress{
					Host:       "knroute.example.com",
					RouterName: "default",
					Conditions: []routev1.RouteIngressCondition{
						routev1.RouteIngressCondition{
							Type:   routev1.RouteAdmitted,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
		},
	}
)

func init() {
	os.Setenv("OPERATOR_NAME", "TEST_OPERATOR")
	os.Setenv("KOURIER_MANIFEST_PATH", "kourier/testdata/kourier-latest.yaml")
	os.Setenv("CONSOLECLIDOWNLOAD_MANIFEST_PATH", "consoleclidownload/testdata/console_cli_download_kn_resources.yaml")
}

// TestKourierReconcile runs Reconcile to verify if expected Kourier resources are deleted.
func TestKourierReconcile(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))

	tests := []struct {
		name           string
		ownerName      string
		ownerNamespace string
		deleted        bool
	}{
		{
			name:           "reconcile request with same KnativeServing owner",
			ownerName:      "knative-serving",
			ownerNamespace: "knative-serving",
			deleted:        true,
		},
		{
			name:           "reconcile request with different KnativeServing owner name",
			ownerName:      "FOO",
			ownerNamespace: "knative-serving",
			deleted:        false,
		},
		{
			name:           "reconcile request with different KnativeServing owner namespace",
			ownerName:      "knative-serving",
			ownerNamespace: "FOO",
			deleted:        false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ks := &defaultKnativeServing
			ingress := &defaultIngress
			knRoute := &defaultKnRoute
			ccd := &consolev1.ConsoleCLIDownload{}

			initObjs := []runtime.Object{ks, ingress, knRoute}

			// Register operator types with the runtime scheme.
			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.SchemeGroupVersion, ks)
			s.AddKnownTypes(configv1.SchemeGroupVersion, ingress)
			s.AddKnownTypes(v1alpha3.SchemeGroupVersion, &v1alpha3.VirtualServiceList{})
			s.AddKnownTypes(routev1.GroupVersion, knRoute)
			s.AddKnownTypes(consolev1.GroupVersion, ccd)

			cl := fake.NewFakeClient(initObjs...)
			r := &ReconcileKnativeServing{client: cl, scheme: s}

			// Reconcile to intialize
			if _, err := r.Reconcile(defaultRequest); err != nil {
				t.Fatalf("reconcile: (%v)", err)
			}

			// Check if Kourier is deployed.
			deploy := &appsv1.Deployment{}
			err := cl.Get(context.TODO(), types.NamespacedName{Name: "3scale-kourier-gateway", Namespace: "knative-serving-ingress"}, deploy)
			if err != nil {
				t.Fatalf("get: (%v)", err)
			}

			// Check kn ConsoleCLIDownload CR
			err = cl.Get(context.TODO(), types.NamespacedName{Name: "kn-cli-downloads", Namespace: ""}, ccd)
			if err != nil {
				t.Fatalf("get: (%v)", err)
			}

			// Delete Kourier deployment.
			err = cl.Delete(context.TODO(), deploy)
			if err != nil {
				t.Fatalf("delete: (%v)", err)
			}

			// Delete ConsoleCLIDownload CR
			err = cl.Delete(context.TODO(), ccd)
			if err != nil {
				t.Fatalf("delete: (%v)", err)
			}

			// Reconcile again with test requests.
			req := reconcile.Request{
				NamespacedName: types.NamespacedName{Namespace: test.ownerNamespace, Name: test.ownerName},
			}
			if _, err := r.Reconcile(req); err != nil {
				t.Fatalf("reconcile: (%v)", err)
			}

			// Check again if Kourier deployment is created after reconcile.
			err = cl.Get(context.TODO(), types.NamespacedName{Name: "3scale-kourier-gateway", Namespace: "knative-serving-ingress"}, deploy)
			if test.deleted {
				if err != nil {
					t.Fatalf("get: (%v)", err)
				}
			}
			if !test.deleted {
				if !errors.IsNotFound(err) {
					t.Fatalf("get: (%v)", err)
				}
			}
		})
	}
}

// TestKourierReconcile runs Reconcile to verify if orphaned virtualservice is deleted or not
func TestDeleteVirtualServiceReconcile(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))

	tests := []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
		deleted     bool
	}{
		{
			name:        "delete virtualservice with expected label and annotation",
			labels:      map[string]string{routeLabelKey: "something", "a": "b"},
			annotations: map[string]string{ingressClassKey: istioIngressClass, "c": "d"},
			deleted:     true,
		},
		{
			name:        "do not delete virtualservice with expected label but without annotation",
			labels:      map[string]string{routeLabelKey: "something", "a": "b"},
			annotations: map[string]string{"c": "d"},
			deleted:     false,
		},
		{
			name:        "do not delete virtualservice with expected annotation but without label",
			labels:      map[string]string{"a": "b"},
			annotations: map[string]string{ingressClassKey: istioIngressClass, "c": "d"},
			deleted:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ks := &defaultKnativeServing
			ingress := &defaultIngress
			vs := &defaultVirtualService
			knRoute := &defaultKnRoute

			// Set annotation and label for test
			vs.SetAnnotations(test.annotations)
			vs.SetLabels(test.labels)

			initObjs := []runtime.Object{ks, ingress, vs, knRoute}

			// Register operator types with the runtime scheme.
			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.SchemeGroupVersion, ks)
			s.AddKnownTypes(configv1.SchemeGroupVersion, ingress)
			s.AddKnownTypes(v1alpha3.SchemeGroupVersion, vs)
			s.AddKnownTypes(routev1.GroupVersion, knRoute)

			cl := fake.NewFakeClient(initObjs...)
			r := &ReconcileKnativeServing{client: cl, scheme: s}

			if _, err := r.Reconcile(defaultRequest); err != nil {
				t.Fatalf("reconcile: (%v)", err)
			}

			// Check if VirtualService is deleted.
			refetched := &v1alpha3.VirtualService{}
			err := cl.Get(context.TODO(), types.NamespacedName{Name: "vsName", Namespace: "vsNamespace"}, refetched)
			if test.deleted {
				if !errors.IsNotFound(err) {
					t.Fatalf("get: (%v)", err)
				}
			}
			if !test.deleted {
				if err != nil {
					t.Fatalf("get: (%v)", err)
				}
			}
		})
	}
}
