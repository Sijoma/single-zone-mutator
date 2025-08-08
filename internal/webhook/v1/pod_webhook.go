/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// nolint:unused
// log is for logging in this package.
var podlog = logf.Log.WithName("pod-resource")

// SetupPodWebhookWithManager registers the webhook for Pod in the manager.
func SetupPodWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1.Pod{}).
		WithDefaulter(&PodCustomDefaulter{client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:rbac:groups="",resources=nodes;persistentvolumeclaims,verbs=list;get;watch
// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=ignore,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod-v1.kb.io,admissionReviewVersions=v1

// PodCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Pod when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type PodCustomDefaulter struct {
	client client.Client
}

var _ webhook.CustomDefaulter = &PodCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Pod.
func (d *PodCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	pod, ok := obj.(*corev1.Pod)

	if !ok {
		return fmt.Errorf("expected an Pod object but got %T", obj)
	}
	podlog.Info("Defaulting for Pod", "name", pod.GetName())

	// Annotation here to decide whether to apply the defaulting logic
	if !strings.HasSuffix(pod.GetNamespace(), "-zeebe") {
		podlog.Info("Skipping defaulting for Pod not in zeebe namespace", "namespace", pod.GetNamespace())
		return nil
	}

	wasAlreadyScheduled := checkIfAlreadyScheduled(ctx, d.client, pod)
	if wasAlreadyScheduled {
		podlog.Info("Pod already scheduled before, do not modify pod. Let the disk decide.")
		return nil
	}

	zones := listNodesToZones(ctx, d.client)
	// If no zones found, use a default
	if len(zones) == 0 {
		podlog.Info("No zones found, do not modify pod.")
		return nil
	}

	// Get deterministic zone based on namespace
	zoneIndex := hashString(pod.GetNamespace()) % len(zones)
	zone := zones[zoneIndex]
	podlog.Info("Selected zone for namespace", "namespace", pod.GetNamespace(), "zone", zone)

	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &corev1.Affinity{}
	}

	pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "topology.kubernetes.io/zone",
							Operator: corev1.NodeSelectorOpIn,
							Values:   []string{zone},
						},
					},
				},
			},
		},
	}

	return nil
}

func checkIfAlreadyScheduled(ctx context.Context, ctrlClient client.Client, pod *corev1.Pod) bool {
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			var pvc corev1.PersistentVolumeClaim
			err := ctrlClient.Get(ctx, types.NamespacedName{
				Namespace: pod.GetNamespace(),
				Name:      volume.PersistentVolumeClaim.ClaimName,
			}, &pvc)
			if err != nil {
				if errors.IsNotFound(err) {
					return false
				}
				// Todo: Log the error and return false to indicate we cannot determine the PVC status
				return false // Or should we return true here?
			}
			if pvc.Status.Phase == corev1.ClaimBound {
				return true
			}
		}
	}
	// If we reach here, it means none of the PVCs are bound
	return false
}

func listNodesToZones(ctx context.Context, ctrlClient client.Client) []string {
	var nodeList corev1.NodeList
	if err := ctrlClient.List(ctx, &nodeList); err != nil {
		podlog.Error(err, "Failed to list nodes for Pod defaulting")
		return nil
	}

	zones := []string{}
	for _, node := range nodeList.Items {
		if z, ok := node.Labels["topology.kubernetes.io/zone"]; ok {
			// Check if zone is already in the list to avoid duplicates
			found := false
			for _, existingZone := range zones {
				if existingZone == z {
					found = true
					break
				}
			}
			if !found {
				zones = append(zones, z)
			}
		}
	}
	sort.Strings(zones) // Sort alphabetically for deterministic order
	return zones
}

// hashString creates a simple hash from a string to get a deterministic number
func hashString(s string) int {
	h := 0
	for i := 0; i < len(s); i++ {
		h = 31*h + int(s[i])
	}
	if h < 0 {
		h = -h // Ensure positive number
	}
	return h
}
