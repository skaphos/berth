package operator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/go-logr/logr"

	berthv1alpha1 "github.com/skaphos/berth/api/v1alpha1"
	berthclient "github.com/skaphos/berth/pkg/client"
	appsv1 "k8s.io/api/apps/v1"
	authnv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestManagedExecution runs inside a disposable kind node, where loopback
// admission URLs and crictl refer to that node. Never point it at a shared cluster.
func TestManagedExecution(t *testing.T) {
	ctrl.SetLogger(logr.Discard())
	configPath := os.Getenv("BERTH_TEST_KUBECONFIG")
	if configPath == "" {
		t.Skip("requires disposable kind node and BERTH_TEST_KUBECONFIG")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{Paths: []string{filepath.Join("..", "..", "config", "crd")}}); err != nil {
		t.Fatal(err)
	}
	kc, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := kc.AuthenticationV1().SelfSubjectReviews().Create(context.Background(), &authnv1.SelfSubjectReview{}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	adoptionDenied := make(chan struct{}, 1)
	delays := map[string]*delayedAdmission{"late-pod": newDelayedAdmission(), "late-job": newDelayedAdmission()}
	for _, d := range delays {
		t.Cleanup(d.release)
	}
	_ = installTestAdmission(t, cfg, identity.Status.UserInfo.Username, func(inner admission.Handler) admission.Handler {
		return admission.HandlerFunc(func(ctx context.Context, req admission.Request) admission.Response {
			response := inner.Handle(ctx, req)
			if req.Name == "adoption-orphan" && req.Operation == admissionv1.Update && !response.Allowed {
				select {
				case adoptionDenied <- struct{}{}:
				default:
				}
			}
			if d := delays[req.Name]; d != nil && req.Operation == admissionv1.Create && response.Allowed {
				d.once.Do(func() { close(d.entered) })
				select {
				case <-d.proceed:
				case <-ctx.Done():
					return admission.Denied(ctx.Err().Error())
				}
			}
			return response
		})
	})
	c, err := ctrlclient.New(cfg, ctrlclient.Options{Scheme: newScheme(t)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	time.Sleep(time.Second) // allow API-server admission configuration propagation
	t.Run("ReplicaSetCannotAdoptRunningOrphan", func(t *testing.T) {
		zero := int32(0)
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "adoption-orphan", Labels: map[string]string{"app": "adoption"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "docker.io/kindest/local-path-helper:v20260131-7181c60a", ImagePullPolicy: corev1.PullNever, Command: []string{"/bin/sh", "-c", "sleep 300"}}}}}
		if err := c.Create(ctx, pod); err != nil {
			t.Fatal(err)
		}
		until(t, 30*time.Second, func() bool {
			if err := c.Get(ctx, ctrlclient.ObjectKeyFromObject(pod), pod); err != nil {
				t.Fatal(err)
			}
			return pod.Status.Phase == corev1.PodRunning
		})
		l := newLease(nil)
		l.Name, l.UID, l.ResourceVersion = "lease-adoption", "", ""
		l.Status = berthv1alpha1.BerthLeaseStatus{}
		l.Spec.Target.Name, l.Spec.Target.Kind = "adoption-rs", "ReplicaSet"
		if err := c.Create(ctx, l); err != nil {
			t.Fatal(err)
		}
		rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "adoption-rs"}, Spec: appsv1.ReplicaSetSpec{Replicas: &zero, Selector: &metav1.LabelSelector{MatchLabels: pod.Labels}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: pod.Labels}, Spec: *pod.Spec.DeepCopy()}}}
		rs.Spec.Template.Spec.NodeName = ""
		if err := c.Create(ctx, rs); err != nil {
			t.Fatal(err)
		}
		select {
		case <-adoptionDenied:
		case <-time.After(20 * time.Second):
			t.Fatal("real ReplicaSet controller did not reach adoption rejection")
		}
		if err := c.Get(ctx, ctrlclient.ObjectKeyFromObject(pod), pod); err != nil {
			t.Fatal(err)
		}
		if len(pod.OwnerReferences) != 0 || pod.Annotations[ManagedUID] != "" {
			t.Fatal("unadmitted orphan entered managed controller ownership")
		}
		t.Logf("normal ReplicaSet controller adoption denied for running orphan UID=%s", pod.UID)
		for _, obj := range []ctrlclient.Object{rs, l, pod} {
			if err := c.Delete(ctx, obj); err != nil {
				t.Fatal(err)
			}
		}
	})
	for _, kind := range []string{"Deployment", "CronJob"} {
		t.Run(kind, func(t *testing.T) {
			l := newLease(nil)
			l.Name = "lease-" + strings.ToLower(kind)
			l.UID = ""
			l.ResourceVersion = ""
			l.Status = berthv1alpha1.BerthLeaseStatus{}
			l.Spec.Target.Name = "target-" + strings.ToLower(kind)
			zero := int32(0)
			one := int32(1)
			grace := int64(2)
			tmpl := corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": l.Name}}, Spec: corev1.PodSpec{TerminationGracePeriodSeconds: &grace, Containers: []corev1.Container{{Name: "worker", Image: "docker.io/kindest/local-path-helper:v20260131-7181c60a", ImagePullPolicy: corev1.PullNever, Command: []string{"/bin/sh", "-c", "echo execution-started; trap 'echo execution-stopped; exit 0' TERM; while :; do sleep 1; done"}}}}}
			var target ctrlclient.Object
			if kind == "Deployment" {
				d := newDeployment(0)
				d.Name = l.Spec.Target.Name
				d.UID = ""
				d.Annotations = nil
				d.Labels = nil
				d.Spec.Replicas = &zero
				d.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": l.Name}}
				d.Spec.Template = tmpl
				target = d
				l.Spec.AcquireAction.Scale.Replicas = one
			} else {
				l.Spec.Target.APIVersion = "batch/v1"
				l.Spec.Target.Kind = "CronJob"
				off, on := false, true
				l.Spec.AcquireAction = &berthv1alpha1.LeaseAction{Suspend: &off}
				l.Spec.ReleaseAction = &berthv1alpha1.LeaseAction{Suspend: &on}
				tmpl.Spec.RestartPolicy = corev1.RestartPolicyNever
				target = &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: l.Spec.Target.Name}, Spec: batchv1.CronJobSpec{Schedule: "0 0 1 1 *", Suspend: &on, JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: tmpl}}}}
			}
			if err := c.Create(ctx, l); err != nil {
				t.Fatal(err)
			}
			if err := c.Create(ctx, target); err != nil {
				t.Fatal(err)
			}
			lc := &fakeLeaseClient{acquireResult: berthclient.AcquireResult{Acquired: true, Holder: "cluster-east", FencingToken: 7, ExpiresAt: time.Now().Add(5 * time.Minute)}}
			r := testReconciler(c, lc)
			tick := func() {
				t.Helper()
				if _, err := r.Reconcile(ctx, requestForLease(l)); err != nil {
					t.Logf("reconcile retry: %v", err)
				}
			}
			tick()
			if kind == "CronJob" {
				if err := c.Get(ctx, ctrlclient.ObjectKeyFromObject(target), target); err != nil {
					t.Fatal(err)
				}
				cron := target.(*batchv1.CronJob)
				job := &batchv1.Job{ObjectMeta: cron.Spec.JobTemplate.ObjectMeta, Spec: *cron.Spec.JobTemplate.Spec.DeepCopy()}
				job.Namespace = "ns"
				job.Name = "active-job"
				job.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(cron, batchv1.SchemeGroupVersion.WithKind("CronJob"))}
				if err := c.Create(ctx, job); err != nil {
					t.Fatal(err)
				}
			}
			var running corev1.Pod
			until(t, 60*time.Second, func() bool {
				tick()
				var pods corev1.PodList
				if err := c.List(ctx, &pods, ctrlclient.InNamespace("ns"), ctrlclient.MatchingLabels{ManagedUID: string(l.UID)}); err != nil {
					t.Fatal(err)
				}
				for _, p := range pods.Items {
					if p.Status.Phase == corev1.PodRunning {
						running = p
						return true
					}
				}
				return false
			})
			logs, err := kc.CoreV1().Pods("ns").GetLogs(running.Name, &corev1.PodLogOptions{Container: "worker"}).DoRaw(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(logs), "execution-started") {
				t.Fatalf("no actual execution evidence: %q", logs)
			}
			id := strings.TrimPrefix(running.Status.ContainerStatuses[0].ContainerID, "containerd://")
			before, err := exec.Command("crictl", "inspect", id).Output()
			if err != nil {
				t.Fatal(err)
			}
			var process struct {
				Info struct {
					PID int `json:"pid"`
				} `json:"info"`
			}
			if err := json.Unmarshal(before, &process); err != nil {
				t.Fatal(err)
			}
			if process.Info.PID <= 0 {
				t.Fatal("runtime did not report container process PID")
			}
			t.Logf("actual execution: %s Pod UID=%s container=%s", kind, running.UID, id)
			delayedResults := make(chan error, 2)
			var latePod *corev1.Pod
			var lateJob *batchv1.Job
			if kind == "CronJob" {
				latePod = &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: l.Namespace, Name: "late-pod", OwnerReferences: running.OwnerReferences}, Spec: *running.Spec.DeepCopy()}
				latePod.Spec.NodeName = ""
				latePod.Spec.SchedulingGates = []corev1.PodSchedulingGate{{Name: SchedulingGate}}
				cron := target.(*batchv1.CronJob)
				lateJob = &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Namespace: l.Namespace, Name: "late-job", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(cron, batchv1.SchemeGroupVersion.WithKind("CronJob"))}}, Spec: *cron.Spec.JobTemplate.Spec.DeepCopy()}
				go func() { delayedResults <- c.Create(ctx, latePod) }()
				go func() { delayedResults <- c.Create(ctx, lateJob) }()
				for _, d := range delays {
					select {
					case <-d.entered:
					case err := <-delayedResults:
						t.Fatalf("create failed before delayed admission: %v", err)
					case <-time.After(15 * time.Second):
						t.Fatal("late create did not reach admission")
					}
				}
			}
			if kind == "Deployment" {
				if err := c.Get(ctx, ctrlclient.ObjectKeyFromObject(l), l); err != nil {
					t.Fatal(err)
				}
				l.Status.Workload.Deadline = timePtr(time.Now().Add(-time.Second))
				if err := c.Status().Update(ctx, l); err != nil {
					t.Fatal(err)
				}
				lc.acquireErr = errors.New("central API partition")
				lc.releaseErr = lc.acquireErr
			} else {
				if err := c.Delete(ctx, l); err != nil {
					t.Fatal(err)
				}
			}
			until(t, 30*time.Second, func() bool {
				tick()
				data, err := exec.Command("crictl", "inspect", id).Output()
				if err != nil {
					if errors.Is(syscall.Kill(process.Info.PID, 0), syscall.ESRCH) {
						t.Logf("runtime removed container and process PID %d is gone", process.Info.PID)
						return true
					}
					return false
				}
				var inspect struct {
					Status struct {
						State    string `json:"state"`
						ExitCode int    `json:"exitCode"`
					} `json:"status"`
				}
				if err := json.Unmarshal(data, &inspect); err != nil {
					t.Fatal(err)
				}
				if inspect.Status.State == "CONTAINER_EXITED" {
					t.Logf("kubelet termination confirmed: %s exit=%d", id, inspect.Status.ExitCode)
					return true
				}
				return false
			})
			if kind == "Deployment" {
				lc.releaseErr = nil
			}
			until(t, 30*time.Second, func() bool {
				tick()
				if len(lc.releaseCalls) > 0 && lc.releaseErr == nil {
					return true
				}
				var state berthv1alpha1.BerthLease
				if err := c.Get(ctx, ctrlclient.ObjectKeyFromObject(l), &state); err == nil {
					var pods corev1.PodList
					_ = c.List(ctx, &pods, ctrlclient.InNamespace(l.Namespace))
					t.Logf("waiting for release phase=%s token=%d registered=%d existingPods=%d", state.Status.Workload.Phase, state.Status.Workload.Token, len(state.Status.Workload.Pods), len(pods.Items))
				}
				return false
			})
			if kind == "CronJob" {
				for _, d := range delays {
					d.release()
				}
				for range 2 {
					if err := <-delayedResults; err != nil {
						t.Fatal(err)
					}
				}
				if !hasGate(latePod) || latePod.Spec.NodeName != "" || len(latePod.Status.ContainerStatuses) > 0 {
					t.Fatal("late admitted Pod could execute after release")
				}
				t.Logf("late storage commits remained inert: Pod UID=%s Job UID=%s", latePod.UID, lateJob.UID)
				if _, err := (&managedPodReconciler{r}).Reconcile(ctx, ctrl.Request{NamespacedName: ctrlclient.ObjectKeyFromObject(latePod)}); err != nil {
					t.Fatal(err)
				}
				if _, err := (&managedJobReconciler{r}).Reconcile(ctx, ctrl.Request{NamespacedName: ctrlclient.ObjectKeyFromObject(lateJob)}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

type delayedAdmission struct {
	entered, proceed  chan struct{}
	once, releaseOnce sync.Once
}

func newDelayedAdmission() *delayedAdmission {
	return &delayedAdmission{entered: make(chan struct{}), proceed: make(chan struct{})}
}
func (d *delayedAdmission) release() { d.releaseOnce.Do(func() { close(d.proceed) }) }

func until(t *testing.T, timeout time.Duration, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatal("condition timed out")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func requestForLease(l *berthv1alpha1.BerthLease) ctrl.Request {
	return ctrl.Request{NamespacedName: ctrlclient.ObjectKeyFromObject(l)}
}
