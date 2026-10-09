package gateway_test

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestStandaloneMixedListenerProtocols(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, helm, "template", "test", "turna-gateway", "--set", "mode=standalone", "--set-json", `listenerPorts=[{"name":"http","port":80},{"name":"dns-tcp","port":53,"protocol":"TCP"},{"name":"dns-udp","port":53,"protocol":"UDP"}]`).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	want := map[string]corev1.Protocol{"http": corev1.ProtocolTCP, "dns-tcp": corev1.ProtocolTCP, "dns-udp": corev1.ProtocolUDP}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
	seen := map[string]bool{}
	for {
		var obj struct {
			Kind string `json:"kind"`
			Spec struct {
				Ports    []corev1.ServicePort   `json:"ports"`
				Template corev1.PodTemplateSpec `json:"template"`
			} `json:"spec"`
		}
		if err := decoder.Decode(&obj); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		switch obj.Kind {
		case "Service":
			seen[obj.Kind] = true
			if len(obj.Spec.Ports) != len(want) {
				t.Fatalf("service ports = %+v", obj.Spec.Ports)
			}
			for _, p := range obj.Spec.Ports {
				if p.Protocol != want[p.Name] {
					t.Errorf("Service %s protocol = %s, want %s", p.Name, p.Protocol, want[p.Name])
				}
			}
		case "Deployment":
			seen[obj.Kind] = true
			ports := obj.Spec.Template.Spec.Containers[0].Ports
			if len(ports) != len(want)+1 {
				t.Fatalf("container ports = %+v", ports)
			}
			for _, p := range ports {
				if p.Name != "health" && p.Protocol != want[p.Name] {
					t.Errorf("Deployment %s protocol = %s, want %s", p.Name, p.Protocol, want[p.Name])
				}
			}
		}
	}
	if !seen["Service"] || !seen["Deployment"] {
		t.Fatal("standalone Service or Deployment missing")
	}
}
