package operator

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// This test uses a real API server and etcd. It intentionally requires assets
// rather than silently pretending that a fake client's admission behavior is real.
func TestManagedAdmissionAPIStorage(t *testing.T) {
	assets := os.Getenv("KUBEBUILDER_ASSETS")
	if assets == "" {
		t.Skip("set KUBEBUILDER_ASSETS to run API storage/admission integration")
	}
	env := &envtest.Environment{BinaryAssetsDirectory: assets, CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd")}, ErrorIfCRDPathMissing: true}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Error(err)
		}
	})
	c := installTestAdmission(t, cfg, "system:serviceaccount:ops:operator")
	ctx := context.Background()
	lease := newLease(nil)
	lease.UID = ""
	lease.ResourceVersion = ""
	lease.Status = berthv1alpha1.BerthLeaseStatus{}
	if err := c.Create(ctx, lease); err != nil {
		t.Fatal(err)
	}
	// Admission configuration propagates asynchronously. An unresolved owner is
	// a harmless probe; wait for rejection proving the handlers are installed.
	for deadline := time.Now().Add(10 * time.Second); ; {
		probe := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{GenerateName: "probe-", Namespace: "ns", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(&appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "missing", UID: "missing"}}, appsv1.SchemeGroupVersion.WithKind("ReplicaSet"))}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "probe", Image: "invalid.test/never-run"}}}}
		if err := c.Create(ctx, probe); err != nil {
			break
		}
		if err := c.Delete(ctx, probe); err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("admission did not become active")
		}
		time.Sleep(100 * time.Millisecond)
	}
	dep := newDeployment(0)
	dep.UID = ""
	dep.Annotations = nil
	dep.Labels = nil
	dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": "worker"}}
	dep.Spec.Template = corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "worker"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "invalid.test/never-run"}}}}
	dryTarget := dep.DeepCopy()
	if err := c.Create(ctx, dryTarget, ctrlclient.DryRunAll); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, ctrlclient.ObjectKeyFromObject(lease), lease); err != nil {
		t.Fatal(err)
	}
	if lease.Status.Workload != nil {
		t.Fatal("dry-run target admission wrote status")
	}
	if err := c.Create(ctx, dep); err != nil {
		t.Fatal(err)
	}
	if dep.UID == "" {
		t.Fatal("no target UID")
	}
	if err := c.Get(ctx, ctrlclient.ObjectKeyFromObject(lease), lease); err != nil {
		t.Fatal(err)
	}
	if lease.Status.Workload == nil || lease.Status.Workload.TargetUID != string(dep.UID) {
		t.Fatalf("final CREATE did not record API-assigned UID: %#v", lease.Status.Workload)
	}
	if len(dep.Spec.Template.Spec.SchedulingGates) != 1 || dep.Spec.Template.Spec.SchedulingGates[0].Name != SchedulingGate {
		t.Fatal("target did not receive Pod scheduling gate")
	}
	// A conflicting status writer cannot replace the newly recorded target.
	stale := lease.DeepCopy()
	lease.Status.Workload.Phase = PhaseStopping
	if err := c.Status().Update(ctx, lease); err != nil {
		t.Fatal(err)
	}
	stale.Status.Workload.Phase = PhaseActive
	if err := c.Status().Update(ctx, stale); err == nil {
		t.Fatal("stale status writer overwrote Stopping")
	}
	// Removing the template gate is rejected at final UPDATE validation.
	dep.Spec.Template.Spec.SchedulingGates = nil
	if err := c.Update(ctx, dep); err == nil {
		t.Fatal("managed template gate removal accepted")
	}
	// Changing lease target identity is rejected by the actual CRD CEL rule.
	lease.Spec.Target.Name = "other"
	if err := c.Update(ctx, lease); err == nil {
		t.Fatal("immutable target update accepted")
	}
}

func installTestAdmission(t *testing.T, cfg *rest.Config, operatorUser string, decorate ...func(admission.Handler) admission.Handler) ctrlclient.Client {
	t.Helper()
	scheme := newScheme(t)
	if err := admissionregistrationv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/mutate", &admission.Webhook{Handler: &WorkloadAdmission{Client: c, Mutate: true, OperatorUser: operatorUser}})
	var validator admission.Handler = &WorkloadAdmission{Client: c, OperatorUser: operatorUser}
	if len(decorate) > 0 {
		validator = decorate[0](validator)
	}
	mux.Handle("/validate", &admission.Webhook{Handler: validator})
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.TLS.Certificates[0].Certificate[0]})
	fail := admissionregistrationv1.Fail
	none := admissionregistrationv1.SideEffectClassNone
	dry := admissionregistrationv1.SideEffectClassNoneOnDryRun
	mutateURL, validateURL := server.URL+"/mutate", server.URL+"/validate"
	rules := []admissionregistrationv1.RuleWithOperations{{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update}, Rule: admissionregistrationv1.Rule{APIGroups: []string{"apps"}, APIVersions: []string{"v1"}, Resources: []string{"deployments", "replicasets", "statefulsets"}}}, {Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update}, Rule: admissionregistrationv1.Rule{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"pods"}}}}
	rules = append(rules, admissionregistrationv1.RuleWithOperations{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create, admissionregistrationv1.Update}, Rule: admissionregistrationv1.Rule{APIGroups: []string{"batch"}, APIVersions: []string{"v1"}, Resources: []string{"jobs", "cronjobs"}}})
	ctx := context.Background()
	m := &admissionregistrationv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "managed-test"}, Webhooks: []admissionregistrationv1.MutatingWebhook{{Name: "mutate.berth.test", ClientConfig: admissionregistrationv1.WebhookClientConfig{URL: &mutateURL, CABundle: ca}, Rules: rules, FailurePolicy: &fail, SideEffects: &none, AdmissionReviewVersions: []string{"v1"}}}}
	v := &admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "managed-test"}, Webhooks: []admissionregistrationv1.ValidatingWebhook{{Name: "validate.berth.test", ClientConfig: admissionregistrationv1.WebhookClientConfig{URL: &validateURL, CABundle: ca}, Rules: rules, FailurePolicy: &fail, SideEffects: &dry, AdmissionReviewVersions: []string{"v1"}}}}
	bindingRule := admissionregistrationv1.RuleWithOperations{Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create}, Rule: admissionregistrationv1.Rule{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"pods/binding"}}}
	v.Webhooks[0].Rules = append(v.Webhooks[0].Rules, bindingRule)
	timeout := int32(30)
	v.Webhooks[0].TimeoutSeconds = &timeout
	if err := c.Create(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns"}}); err != nil {
		t.Fatal(err)
	}
	return c
}
