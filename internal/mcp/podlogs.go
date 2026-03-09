package mcp

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

// kubePodLogReader implements PodLogReader using the Kubernetes clientset.
type kubePodLogReader struct {
	coreClient corev1client.CoreV1Interface
}

// NewPodLogReader creates a PodLogReader backed by a Kubernetes CoreV1 client.
func NewPodLogReader(coreClient corev1client.CoreV1Interface) PodLogReader {
	return &kubePodLogReader{coreClient: coreClient}
}

func (r *kubePodLogReader) GetLogs(ctx context.Context, namespace, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	return r.coreClient.Pods(namespace).GetLogs(podName, opts).Stream(ctx)
}
