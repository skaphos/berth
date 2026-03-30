// Package k8s provides Kubernetes client initialization for Berth components.
//
// [NewClientset] builds a [kubernetes.Clientset] from either in-cluster
// configuration or an explicit kubeconfig file path. When the kubeconfig
// path is empty, it falls back to the in-cluster service account credentials.
package k8s
