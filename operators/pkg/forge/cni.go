package forge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"

	clv1alpha2 "github.com/netgroup-polito/CrownLabs/operators/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	addonsv1 "sigs.k8s.io/cluster-api/exp/addons/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func insertKubeConfig(instance *clv1alpha2.Instance, environment *clv1alpha2.Environment, host string) error {

	cluster := environment.Cluster
	path := fmt.Sprintf("./kubeconfigs/%s-instance.kubeconfig", instance.Name)

	cmd := exec.Command(
		"clusterctl", "get", "kubeconfig", fmt.Sprintf("%s-cluster", cluster.Name),
		"--namespace", instance.Namespace,
	)

	raw, _ := cmd.Output()

	cfg, _ := clientcmd.Load(raw)

	newURL := fmt.Sprintf("https://%s:%d",
		host, environment.Cluster.ClusterNet.NginxPort)

	for _, c := range cfg.Clusters {
		c.Server = newURL
	}

	updated, _ := clientcmd.Write(*cfg)

	return os.WriteFile(path, updated, 0o600)

}

func DownloadCiliumYAML(localPath string) error {
	url := "https://raw.githubusercontent.com/cilium/cilium/v1.17.5/install/kubernetes/cilium.yaml"

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	out, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to creat the file: %w", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	fmt.Println("save cilium.yaml", localPath)
	return nil
}

func CreateCiliumCRS(ctx context.Context, c client.Client, instance *clv1alpha2.Instance, environment *clv1alpha2.Environment, yamlPath string) error {
	cluster := environment.Cluster
	namespace := instance.Namespace
	clusterName := cluster.Name

	content, err := os.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("Cannot get Cilium YAML: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cilium-install",
			Namespace: namespace,
			Labels: map[string]string{
				"capi.cluster.x-k8s.io/cluster-name": clusterName,
			},
		},
		Data: map[string][]byte{
			"cilium.yaml": content,
		},
	}

	if err := c.Create(ctx, secret); err != nil {
		return fmt.Errorf("Failed to get Secret: %w", err)
	}

	crs := &addonsv1.ClusterResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cilium-crs",
			Namespace: namespace,
		},
		Spec: addonsv1.ClusterResourceSetSpec{
			ClusterSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"capi.cluster.x-k8s.io/cluster-name": clusterName,
				},
			},
			Resources: []addonsv1.ResourceRef{
				{
					Name: "cilium-install",
					Kind: "Secret",
				},
			},
			Strategy: addonsv1.ClusterResourceSetStrategyApplyOnce,
		},
	}

	if err := c.Create(ctx, crs); err != nil {
		return fmt.Errorf("Failed to create CRS: %w", err)
	}

	return nil
}
