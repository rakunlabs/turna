package kubernetes

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"

	"github.com/rakunlabs/turna/pkg/server"
)

type nopApplier struct{}

func (nopApplier) SetLayer(string, *server.Server) error { return nil }

func TestControllerProvisionsPerGateway(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "turna-system")

	kube := kubefake.NewSimpleClientset()
	gw := gwfake.NewSimpleClientset()
	bg := context.Background()

	_, _ = gw.GatewayV1().GatewayClasses().Create(bg, class(), metav1.CreateOptions{})
	_, _ = gw.GatewayV1().Gateways("a").Create(bg, gatewayObj("a", "web", httpListener("http", 80, ""), httpListener("alt", 8080, "x.example.com")), metav1.CreateOptions{})
	_, _ = gw.GatewayV1().Gateways("b").Create(bg, gatewayObj("b", "web", httpListener("http", 80, "")), metav1.CreateOptions{})

	ctx, cancel := context.WithCancel(bg)
	defer cancel()

	p := NewWithClients(Config{
		Mode:       ModeController,
		PortOffset: 10000,
		Debounce:   5 * time.Millisecond,
		Status:     Status{LeaderElection: LeaderElection{Disabled: true}},
		Proxy:      Proxy{Image: "turna:test", ServiceAccount: "turna-proxy", ServiceType: "LoadBalancer"},
	}, nopApplier{}, kube, gw)

	go func() { _ = p.Run(ctx) }()

	nameA := proxyName(nsName{"a", "web"})
	nameB := proxyName(nsName{"b", "web"})

	eventually(t, "proxies created", func() bool {
		_, errA := kube.AppsV1().Deployments("turna-system").Get(bg, nameA, metav1.GetOptions{})
		_, errB := kube.AppsV1().Deployments("turna-system").Get(bg, nameB, metav1.GetOptions{})

		return errA == nil && errB == nil
	})

	dep, _ := kube.AppsV1().Deployments("turna-system").Get(bg, nameA, metav1.GetOptions{})
	c := dep.Spec.Template.Spec.Containers[0]
	if c.Image != "turna:test" || dep.Spec.Template.Spec.ServiceAccountName != "turna-proxy" {
		t.Fatalf("deployment = %+v", dep.Spec.Template.Spec)
	}

	ports := map[int32]bool{}
	for _, p := range c.Ports {
		ports[p.ContainerPort] = true
	}

	if !ports[10080] || !ports[18080] || !ports[9901] {
		t.Fatalf("container ports = %v", c.Ports)
	}

	cm, _ := kube.CoreV1().ConfigMaps("turna-system").Get(bg, nameA, metav1.GetOptions{})
	cfg := cm.Data["turna.yaml"]
	if !strings.Contains(cfg, "mode: proxy") || !strings.Contains(cfg, "a/web") {
		t.Fatalf("config = %s", cfg)
	}

	svc, _ := kube.CoreV1().Services("turna-system").Get(bg, nameA, metav1.GetOptions{})
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer || len(svc.Spec.Ports) != 2 || svc.Spec.Ports[0].TargetPort.IntValue() != 10080 {
		t.Fatalf("service = %+v", svc.Spec)
	}

	// not ready yet: programmed is false
	eventually(t, "pending status", func() bool {
		g, _ := gw.GatewayV1().Gateways("a").Get(bg, "web", metav1.GetOptions{})
		c := meta.FindStatusCondition(g.Status.Conditions, string(gwv1.GatewayConditionProgrammed))

		return c != nil && c.Status == metav1.ConditionFalse && c.Reason == string(gwv1.GatewayReasonPending)
	})

	// proxy becomes ready and gets an address
	dep.Status.ReadyReplicas = 1
	if _, err := kube.AppsV1().Deployments("turna-system").UpdateStatus(bg, dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	svc.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{IP: "192.0.2.10"}}
	if _, err := kube.CoreV1().Services("turna-system").UpdateStatus(bg, svc, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	eventually(t, "programmed with address", func() bool {
		g, _ := gw.GatewayV1().Gateways("a").Get(bg, "web", metav1.GetOptions{})

		return meta.IsStatusConditionTrue(g.Status.Conditions, string(gwv1.GatewayConditionProgrammed)) &&
			len(g.Status.Addresses) == 1 && g.Status.Addresses[0].Value == "192.0.2.10"
	})

	// deleting a gateway removes its proxy
	if err := gw.GatewayV1().Gateways("b").Delete(bg, "web", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}

	eventually(t, "proxy removed", func() bool {
		_, err := kube.AppsV1().Deployments("turna-system").Get(bg, nameB, metav1.GetOptions{})

		return err != nil
	})

	// resources not managed by turna are not touched
	_, _ = kube.AppsV1().Deployments("turna-system").Create(bg, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "turna-system"},
	}, metav1.CreateOptions{})

	p.notify()
	time.Sleep(100 * time.Millisecond)

	if _, err := kube.AppsV1().Deployments("turna-system").Get(bg, "unrelated", metav1.GetOptions{}); err != nil {
		t.Fatal("unmanaged deployment deleted")
	}
}

func TestProxyName(t *testing.T) {
	short := proxyName(nsName{"ns", "gw"})
	if !strings.HasPrefix(short, "turna-ns-gw-") || len(short) > 63 {
		t.Fatalf("short = %q", short)
	}

	long := proxyName(nsName{strings.Repeat("n", 40), strings.Repeat("g", 40)})
	if len(long) > 63 || long == proxyName(nsName{strings.Repeat("n", 40), strings.Repeat("g", 41)}) {
		t.Fatalf("long = %q", long)
	}
}
