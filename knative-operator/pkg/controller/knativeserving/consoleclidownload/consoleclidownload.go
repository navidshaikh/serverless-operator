package consoleclidownload

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/openshift-knative/serverless-operator/knative-operator/pkg/common"

	mfc "github.com/manifestival/controller-runtime-client"
	mf "github.com/manifestival/manifestival"
	consolev1 "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	servingv1alpha1 "knative.dev/serving-operator/pkg/apis/serving/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	knCLIDownload                   = "kn-cli-downloads"
	knDownloadServer                = "kn-download-server"
	knConsoleCLIDownloadDeployRoute = "kn-cli-downloads"
	defaultIngressController        = "default" // value taken from github.com/openshift/console-operator/pkg/console/subresource/route/route.go
	OwnerName                       = "serving.knative.openshift.io/ownerName"
	OwnerNamespace                  = "serving.knative.openshift.io/ownerNamespace"
)

var log = common.Log.WithName("consoleclidownload")

// Apply installs kn ConsoleCLIDownload and its required resources
func Apply(instance *servingv1alpha1.KnativeServing, apiclient client.Client, scheme *runtime.Scheme) error {
	if err := applyKnDownloadResources(instance, apiclient, scheme); err != nil {
		return err
	}

	if err := applyKnConsoleCLIDownload(apiclient, instance.GetNamespace()); err != nil {
		return err
	}

	return nil
}

// applyKnDownloadResources creates required resources viz Namespace, Deployment, Service, Route
// which will serve kn cross platform binaries within cluster
func applyKnDownloadResources(instance *servingv1alpha1.KnativeServing, apiclient client.Client, scheme *runtime.Scheme) error {
	log.Info("Installing kn ConsoleCLIDownload resources")
	manifest, err := manifest(instance, apiclient, scheme)
	if err != nil {
		return err
	}

	if err := manifest.Apply(); err != nil {
		return fmt.Errorf("failed to apply kn ConsoleCLIDownload resources manifest: %w", err)
	}

	if err := checkDeployments(&manifest, instance, apiclient); err != nil {
		return err
	}

	return nil
}

// Check for deployments of kn ConsoleCLIDownload
// This function is copied from knativeserving_controller.go in serving-operator
func checkDeployments(manifest *mf.Manifest, instance *servingv1alpha1.KnativeServing, api client.Client) error {
	log.Info("Checking deployments")
	for _, u := range manifest.Filter(mf.ByKind("Deployment"), mf.ByKind("Route")).Resources() {
		switch u.GetKind() {
		case "Deployment":
			deployment := &appsv1.Deployment{}
			err := api.Get(context.TODO(), client.ObjectKey{Namespace: u.GetNamespace(), Name: u.GetName()}, deployment)
			if err != nil {
				return err
			}
			for _, c := range deployment.Status.Conditions {
				if c.Type == appsv1.DeploymentAvailable && c.Status != corev1.ConditionTrue {
					return fmt.Errorf("Deployment %q/%q not ready", u.GetName(), u.GetNamespace())
				}
			}
		case "Route":
			route := &routev1.Route{}
			if err := api.Get(context.TODO(), client.ObjectKey{Namespace: u.GetNamespace(), Name: u.GetName()}, route); err != nil {
				return err
			}
			host := getCanonicalHost(route)
			if host == "" {
				return fmt.Errorf("Route %q/%q not ready yet", u.GetName(), u.GetNamespace())
			}
		}
	}
	return nil
}

// applyKnConsoleCLIDownload applies kn ConsoleCLIDownload by finding
// kn download resource route URL and populating spec accordingly
func applyKnConsoleCLIDownload(apiclient client.Client, namespace string) error {
	log.Info("Installing kn ConsoleCLIDownload")
	route := &routev1.Route{}
	knConsole := &consolev1.ConsoleCLIDownload{}

	// find the route first
	if err := apiclient.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: knConsoleCLIDownloadDeployRoute}, route); err != nil {
		return fmt.Errorf("failed to find kn ConsoleCLIDownload deployment route")
	}

	knRoute := getCanonicalHost(route)
	knConsoleObj := populateKnConsoleCLIDownload(https(knRoute))

	// Check if kn ConsoleCLIDownload exists
	err := apiclient.Get(context.TODO(), client.ObjectKey{Namespace: "", Name: knCLIDownload}, knConsole)
	switch {
	case apierrors.IsNotFound(err):
		log.Info("Creating kn ConsoleCLIDownload")
		if err := apiclient.Create(context.TODO(), knConsoleObj); err != nil {
			return err
		}
	case apierrors.IsAlreadyExists(err):
		log.Info("Updating kn ConsoleCLIDownload")
		if err := apiclient.Update(context.TODO(), knConsoleObj); err != nil {
			return err
		}
	default:
		return err
	}
	return nil
}

// Delete deletes kn ConsoleCLIDownload CO and respective deployment resources
func Delete(instance *servingv1alpha1.KnativeServing, apiclient client.Client, scheme *runtime.Scheme) error {
	log.Info("Deleting kn ConsoleCLIDownload CO")
	if err := apiclient.Delete(context.TODO(), populateKnConsoleCLIDownload("")); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete kn ConsoleCLIDownload CO: %w", err)
	}

	log.Info("Deleting kn ConsoleCLIDownload resources")
	manifest, err := manifest(instance, apiclient, scheme)
	if err != nil {
		return err
	}

	if err := manifest.Delete(); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete kn ConsoleCLIDownload resources manifest: %w", err)
	}

	return nil
}

// manifest returns kn ConsoleCLIDownload deploymnet resources manifest after traformation
func manifest(instance *servingv1alpha1.KnativeServing, apiclient client.Client, scheme *runtime.Scheme) (mf.Manifest, error) {
	manifest, err := RawManifest(apiclient)
	if err != nil {
		return mf.Manifest{}, fmt.Errorf("failed to read kn ConsoleCLIDownload deployment manifest: %w", err)
	}

	// 1. Use instance's namespace to deploy download resources into
	// 2. Set proper kn-cli-artifacts image
	// 3. Set Owner annotations
	transforms := []mf.Transformer{mf.InjectNamespace(instance.GetNamespace()),
		replaceKnCLIArtifactsImage(os.Getenv("IMAGE_KN_CLI_ARTIFACTS"), scheme),
		setOwnerAnnotations(instance),
	}

	manifest, err = manifest.Transform(transforms...)
	if err != nil {
		return mf.Manifest{}, fmt.Errorf("failed to transform kn ConsoleCLIDownload resources manifest: %w", err)
	}

	return manifest, nil
}

// manifest returns kn ConsoleCLIDownload deploymnet resources manifest without transformation
func RawManifest(apiclient client.Client) (mf.Manifest, error) {
	return mfc.NewManifest(manifestPath(), apiclient, mf.UseLogger(log.WithName("mf")))
}

// manifestPath returns kn ConsoleCLIDownload deployment resource manifest path
func manifestPath() string {
	return os.Getenv("CONSOLECLIDOWNLOAD_MANIFEST_PATH")
}

func replaceKnCLIArtifactsImage(image string, scheme *runtime.Scheme) mf.Transformer {
	return func(u *unstructured.Unstructured) error {
		if u.GetKind() == "Deployment" {
			deploy := &appsv1.Deployment{}
			if err := scheme.Convert(u, deploy, nil); err != nil {
				return fmt.Errorf("failed to convert unstructured obj to Deployment: %w", err)
			}

			containers := deploy.Spec.Template.Spec.Containers
			for i, container := range containers {
				if container.Name == knDownloadServer && container.Image != image {
					log.Info("Replacing", "deployment", container.Name, "image", image)
					containers[i].Image = image
					break
				}
			}

			if err := scheme.Convert(deploy, u, nil); err != nil {
				return fmt.Errorf("failed to convert Deployment obj to unstructured: %w", err)
			}
		}
		return nil
	}
}

func setOwnerAnnotations(instance *servingv1alpha1.KnativeServing) mf.Transformer {
	return func(u *unstructured.Unstructured) error {
		u.SetAnnotations(map[string]string{
			OwnerName:      instance.Name,
			OwnerNamespace: instance.Namespace,
		})
		return nil
	}
}

// populateKnConsoleCLIDownload populates kn ConsoleCLIDownload object and its SPEC
// using route's baseURL
func populateKnConsoleCLIDownload(baseURL string) *consolev1.ConsoleCLIDownload {
	return &consolev1.ConsoleCLIDownload{
		ObjectMeta: metav1.ObjectMeta{
			Name: knCLIDownload,
		},
		Spec: consolev1.ConsoleCLIDownloadSpec{
			DisplayName: "kn - OpenShift Serverless Command Line Interface (CLI)",
			Description: "The OpenShift Serverless client `kn` is a CLI tool that allows you to fully manage OpenShift Serverless Serving and Eventing resources without writing a single line of YAML.",
			Links: []consolev1.Link{
				consolev1.Link{
					Text: "Download kn for Linux",
					Href: baseURL + "/amd64/linux/kn-linux-amd64.tar.gz",
				},
				consolev1.Link{
					Text: "Download kn for macOS",
					Href: baseURL + "/amd64/macos/kn-macos-amd64.tar.gz",
				},
				consolev1.Link{
					Text: "Download kn for Windows",
					Href: baseURL + "/amd64/windows/kn-windows-amd64.zip",
				},
			},
		},
	}
}

// Function copied from github.com/openshift/console-operator/pkg/console/subresource/route/route.go and modified
func getCanonicalHost(route *routev1.Route) string {
	for _, ingress := range route.Status.Ingress {
		if ingress.RouterName != defaultIngressController {
			continue
		}
		// ingress must be admitted before it is useful to us
		if !isIngressAdmitted(ingress) {
			continue
		}
		return ingress.Host
	}
	return ""
}

// Function copied from github.com/openshift/console-operator/pkg/console/subresource/route/route.go
func isIngressAdmitted(ingress routev1.RouteIngress) bool {
	admitted := false
	for _, condition := range ingress.Conditions {
		if condition.Type == routev1.RouteAdmitted && condition.Status == corev1.ConditionTrue {
			admitted = true
		}
	}
	return admitted
}

// copied from github.com/openshift/console-operator/pkg/console/subresource/util/util.go and modified
func https(host string) string {
	protocol := "https://"
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, protocol) {
		return host
	}
	secured := fmt.Sprintf("%s%s", protocol, host)
	return secured
}
