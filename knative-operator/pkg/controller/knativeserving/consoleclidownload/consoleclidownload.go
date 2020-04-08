package consoleclidownload

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/openshift-knative/serverless-operator/knative-operator/pkg/common"

	mfc "github.com/manifestival/controller-runtime-client"
	mf "github.com/manifestival/manifestival"
	consolev1 "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	consoleroute "github.com/openshift/console-operator/pkg/console/subresource/route"
	consoleutil "github.com/openshift/console-operator/pkg/console/subresource/util"
	k8sv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	servingv1alpha1 "knative.dev/serving-operator/pkg/apis/serving/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	knDownloadServer                    = "kn-download-server"
	knConsoleCLIDownloadDeployRoute     = "kn-cli-downloads"
	knConsoleCLIDownloadDeployNamespace = "kn-cli-downloads"
)

var log = common.Log.WithName("consoleclidownload")

// Apply installs kn ConsoleCLIDownload and its required resources
func Apply(instance *servingv1alpha1.KnativeServing, apiclient client.Client, scheme *runtime.Scheme) error {
	if err := applyKnDownloadResources(instance, apiclient, scheme); err != nil {
		return err
	}

	if err := applyKnConsoleCLIDownload(apiclient); err != nil {
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

	return nil
}

// applyKnConsoleCLIDownload applies kn ConsoleCLIDownload by finding
// kn download resource route URL and populating spec accordingly
func applyKnConsoleCLIDownload(apiclient client.Client) error {
	route := &routev1.Route{}
	// Waiting for the deployment/route to appear. TODO: ideally this should be a watch
	time.Sleep(time.Second * 10)
	err := apiclient.Get(context.TODO(),
		client.ObjectKey{Namespace: knConsoleCLIDownloadDeployNamespace, Name: knConsoleCLIDownloadDeployRoute},
		route)
	if err != nil {
		return fmt.Errorf("failed to find kn ConsoleCLIDownload deployment route: %w", err)
	}

	knRoute := consoleroute.GetCanonicalHost(route)
	if knRoute == "" {
		return fmt.Errorf("failed to find kn ConsoleCLIDownload deployment route, it might not be ready yet")
	}

	log.Info(fmt.Sprintf("kn ConsoleCLIDownload route found %s", knRoute))
	log.Info("Creating kn ConsoleCLIDownload CR")
	baseURL := fmt.Sprintf("%s", consoleutil.HTTPS(knRoute))
	knConsoleObj := populateKnConsoleCLIDownload(baseURL)
	err = apiclient.Create(context.TODO(), knConsoleObj)
	if err != nil {
		return fmt.Errorf("failed to create kn ConsoleCLIDownload CR: %w", err)
	}

	return nil
}

// Delete deletes kn ConsoleCLIDownload CO and respective deployment resources
func Delete(instance *servingv1alpha1.KnativeServing, apiclient client.Client, scheme *runtime.Scheme) error {
	log.Info("Deleting kn ConsoleCLIDownload CO")
	if err := apiclient.Delete(context.TODO(), populateKnConsoleCLIDownload("")); err != nil {
		return fmt.Errorf("failed to delete kn ConsoleCLIDownload CO: %w", err)
	}

	log.Info("Deleting kn ConsoleCLIDownload resources")
	manifest, err := manifest(instance, apiclient, scheme)
	if err != nil {
		return err
	}

	if err := manifest.Delete(); err != nil {
		return fmt.Errorf("failed to delete kn ConsoleCLIDownload resources manifest: %w", err)
	}

	return nil
}

// manifest returns kn ConsoleCLIDownload deploymnet resources manifest after traformation
func manifest(instance *servingv1alpha1.KnativeServing, apiclient client.Client, scheme *runtime.Scheme) (mf.Manifest, error) {
	manifest, err := rawManifest(apiclient)
	if err != nil {
		return mf.Manifest{}, fmt.Errorf("failed to read kn ConsoleCLIDownload deployment manifest: %w", err)
	}

	// 1. Use instance's namespace to deploy download resources into
	// 2. Set proper kn-cli-artifacts image
	transforms := []mf.Transformer{mf.InjectNamespace(instance.GetNamespace()),
		replaceKnCLIArtifactsImage(os.Getenv("IMAGE_KN_CLI_ARTIFACTS"), scheme),
	}

	manifest, err = manifest.Transform(transforms...)
	if err != nil {
		return mf.Manifest{}, fmt.Errorf("failed to transform kn ConsoleCLIDownload resources manifest: %w", err)
	}

	return manifest, nil
}

// manifest returns kn ConsoleCLIDownload deploymnet resources manifest without transformation
func rawManifest(apiclient client.Client) (mf.Manifest, error) {
	return mfc.NewManifest(manifestPath(), apiclient, mf.UseLogger(log.WithName("mf")))
}

// manifestPath returns kn ConsoleCLIDownload deployment resource manifest path
func manifestPath() string {
	return os.Getenv("CONSOLECLIDOWNLOAD_MANIFEST_PATH")
}

func replaceKnCLIArtifactsImage(image string, scheme *runtime.Scheme) mf.Transformer {
	return func(u *unstructured.Unstructured) error {
		if u.GetKind() == "Deployment" {
			deploy := &k8sv1.Deployment{}
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

// populateKnConsoleCLIDownload populates kn ConsoleCLIDownload object and its SPEC
// using route's baseURL
func populateKnConsoleCLIDownload(baseURL string) *consolev1.ConsoleCLIDownload {
	return &consolev1.ConsoleCLIDownload{
		metav1.TypeMeta{
			Kind:       "ConsoleCLIDownload",
			APIVersion: "console.openshift.io/v1",
		},
		metav1.ObjectMeta{
			Name: "kn-cli-downloads",
		},
		consolev1.ConsoleCLIDownloadSpec{
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
