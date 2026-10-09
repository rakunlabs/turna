package kubernetes

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestRestConfigMergesKubeconfigEnvironment(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	dir := t.TempDir()
	contextPath, credentialsPath := filepath.Join(dir, "context.yaml"), filepath.Join(dir, "credentials.yaml")
	first := clientcmdapi.NewConfig()
	first.CurrentContext = "test"
	first.Contexts["test"] = &clientcmdapi.Context{Cluster: "cluster", AuthInfo: "user"}
	second := clientcmdapi.NewConfig()
	second.Clusters["cluster"] = &clientcmdapi.Cluster{Server: "https://api.example.com"}
	second.AuthInfos["user"] = &clientcmdapi.AuthInfo{Token: "test-token"}
	for path, cfg := range map[string]*clientcmdapi.Config{contextPath: first, credentialsPath: second} {
		if err := clientcmd.WriteToFile(*cfg, path); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("KUBECONFIG", contextPath+string(os.PathListSeparator)+credentialsPath)
	rc, err := restConfig("")
	if err != nil {
		t.Fatalf("loading merged KUBECONFIG: %v", err)
	}
	if rc.Host != "https://api.example.com" || rc.BearerToken != "test-token" {
		t.Fatalf("merged config host/token = %q/%q", rc.Host, rc.BearerToken)
	}
	// An explicit path must still override the environment's file list.
	explicit := clientcmdapi.NewConfig()
	explicit.CurrentContext = "explicit"
	explicit.Contexts["explicit"] = &clientcmdapi.Context{Cluster: "explicit"}
	explicit.Clusters["explicit"] = &clientcmdapi.Cluster{Server: "https://explicit.example.com"}
	path := filepath.Join(dir, "explicit.yaml")
	if err := clientcmd.WriteToFile(*explicit, path); err != nil {
		t.Fatal(err)
	}
	rc, err = restConfig(path)
	if err != nil || rc.Host != "https://explicit.example.com" {
		t.Fatalf("explicit kubeconfig = %+v, err = %v", rc, err)
	}
}
