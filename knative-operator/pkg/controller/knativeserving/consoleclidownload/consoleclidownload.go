package consoleclidownload

import (
	"context"
	"fmt"
	"os"
	"time"

	mfc "github.com/manifestival/controller-runtime-client"
	mf "github.com/manifestival/manifestival"
	consolev1 "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	consoleroute "github.com/openshift/console-operator/pkg/console/subresource/route"
	consoleutil "github.com/openshift/console-operator/pkg/console/subresource/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	servingv1alpha1 "knative.dev/serving-operator/pkg/apis/serving/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-knative/serverless-operator/knative-operator/pkg/common"
)

const (
	knConsoleCLIDownloadDeployRoute       = "kn-downloads"
	knConsoleCLIDownloadDeployNamespace   = "serverless-operator"
	defaultKnConsoleCLIDownloadCR         = "deploy/resources/console_cli_download_kn.yaml"
	defaultKnConsoleCLIDownloadDeployment = "deploy/resources/console_cli_download_kn_deployment.yaml"
)

var log = common.Log.WithName("consoleclidownload")

// Create deploy deployment and CR for kn ConsoleCLIDownload
func Create(instance *servingv1alpha1.KnativeServing, apiclient client.Client) error {
	if err := createKnDeployment(apiclient); err != nil {
		return err
	}
	if err := createCR(apiclient); err != nil {
		return err
	}
	return nil
}

func createKnDeployment(apiclient client.Client) error {
	log.Info("Creating ConsoleCLIDownloadDeployment for kn")
	manifest, err := mfc.NewManifest(manifestPathKnConsoleCLIDownloadDeploy(), apiclient)
	if err != nil {
		return fmt.Errorf("failed to read ConsoleCLIDownloadDeployment manifest: %w", err)
	}
	if err := manifest.Apply(); err != nil {
		return fmt.Errorf("failed to apply ConsoleCLIDownloadDeployment manifest: %w", err)
	}
	return nil
}

func createCR(apiclient client.Client) error {
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
	if knRoute == "" { // TODO: route ingress is not ready yet, add a retry logic here
		return fmt.Errorf("failed to find kn ConsoleCLIDownload deployment route, it might be ready yet")
	}

	log.Info("Creating ConsoleCLIDownload CR for kn")
	manifest, err := mfc.NewManifest(manifestPathKnConsoleCLIDownloadCR(), apiclient)
	if err != nil {
		return fmt.Errorf("failed to read kn ConsoleCLIDownload CR manifest: %w", err)
	}

	manifest, err = manifest.Transform(updateKnDownloadLinks(knRoute))
	if err != nil {
		return fmt.Errorf("failed to transform kn ConsoleCLIDownload CR manifest: %w", err)
	}

	if err := manifest.Apply(); err != nil {
		return fmt.Errorf("failed to apply kn ConsoleCLIDownload CR manifest: %w", err)
	}

	return nil
}

// Delete deletes ConsoleCLIDownload for kn CLI download links
func Delete(instance *servingv1alpha1.KnativeServing, apiclient client.Client) error {
	log.Info("Deleting kn ConsoleCLIDownload CR")
	manifest, err := mfc.NewManifest(manifestPathKnConsoleCLIDownloadCR(), apiclient)
	if err != nil {
		return fmt.Errorf("failed to read kn ConsoleCLIDownload CR manifest: %w", err)
	}

	if err := manifest.Delete(); err != nil {
		return fmt.Errorf("failed to delete kn ConsoleCLIDownload deployment manifest: %w", err)
	}

	log.Info("Deleting kn ConsoleCLIDownload deployment, route, service")
	manifest, err = mfc.NewManifest(manifestPathKnConsoleCLIDownloadDeploy(), apiclient)
	if err != nil {
		return fmt.Errorf("failed to read kn ConsoleCLIDownloadDeployment manifest: %w", err)
	}

	if err := manifest.Delete(); err != nil {
		return fmt.Errorf("failed to delete kn ConsoleCLIDownloadDeployment manifest: %w", err)
	}

	return nil

}

func manifestPathKnConsoleCLIDownloadDeploy() string {
	knConsoleCLIDownloadDeploy := os.Getenv("CONSOLE_DOWNLOAD_MANIFEST_PATH")
	if knConsoleCLIDownloadDeploy == "" {
		return defaultKnConsoleCLIDownloadDeployment
	}
	return knConsoleCLIDownloadDeploy
}

func manifestPathKnConsoleCLIDownloadCR() string {
	knConsoleCLIDownloadCR := os.Getenv("CONSOLE_DOWNLOAD_MANIFEST_PATH")
	if knConsoleCLIDownloadCR == "" {
		return defaultKnConsoleCLIDownloadCR
	}
	return knConsoleCLIDownloadCR
}

func updateKnDownloadLinks(route string) mf.Transformer {
	return func(u *unstructured.Unstructured) error {
		scheme := &runtime.Scheme{}
		if u.GetKind() == "ConsoloeCLIDownload" {
			addToScheme(scheme)
			obj := &consolev1.ConsoleCLIDownload{}
			if err := scheme.Convert(u, obj, nil); err != nil {
				return err
			}
			obj.Spec.Links = populateKnLinks(route)
			if err := scheme.Convert(obj, u, nil); err != nil {
				return err
			}
		}
		return nil
	}
}

func addToScheme(scheme *runtime.Scheme) {
	scheme.AddKnownTypes(consolev1.GroupVersion,
		&consolev1.ConsoleCLIDownload{})
}

func populateKnLinks(route string) []consolev1.Link {
	baseURL := fmt.Sprintf("%s", consoleutil.HTTPS(route))
	return []consolev1.Link{
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
	}
}
