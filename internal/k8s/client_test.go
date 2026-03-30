package k8s

import "testing"

func TestNewClientsetInvalidKubeconfig(t *testing.T) {
	t.Parallel()

	clientset, err := NewClientset("/path/that/does/not/exist")
	if err == nil {
		t.Fatal("expected error for invalid kubeconfig path")
	}
	if clientset != nil {
		t.Fatal("expected nil clientset on error")
	}
}
