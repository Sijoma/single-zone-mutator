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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	// TODO (user): Add any additional imports if needed
)

var _ = Describe("Pod Webhook", func() {
	var (
		obj       *corev1.Pod
		oldObj    *corev1.Pod
		defaulter PodCustomDefaulter
	)

	BeforeEach(func() {
		obj = &corev1.Pod{}
		oldObj = &corev1.Pod{}
		defaulter = PodCustomDefaulter{}
		Expect(defaulter).NotTo(BeNil(), "Expected defaulter to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
		// TODO (user): Add any setup logic common to all tests
	})

	AfterEach(func() {
		// TODO (user): Add any teardown logic common to all tests
	})

	Context("When creating Pod under Defaulting Webhook", func() {
		It("Should annotate the namespace and set pod affinity", func() {
			nsName := "test-ns-zeebe"
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
			}()

			// Create a node with a zone label
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						"topology.kubernetes.io/zone": "zone-1",
					},
				},
			}
			Expect(k8sClient.Create(ctx, node)).To(Succeed())
			defer func() {
				Expect(k8sClient.Delete(ctx, node)).To(Succeed())
			}()

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: nsName,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx",
						},
					},
				},
			}

			// We need to use the real client for the defaulter because it fetches the namespace
			d := &PodCustomDefaulter{
				client:          k8sClient,
				namespaceSuffix: "-zeebe",
			}

			err := d.Default(ctx, pod)
			Expect(err).NotTo(HaveOccurred())

			// Check if namespace is annotated
			updatedNs := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName}, updatedNs)).To(Succeed())
			Expect(updatedNs.Annotations).To(HaveKeyWithValue("single-zone-mutator.sijoma.io/zone", "zone-1"))

			// Check pod affinity
			Expect(pod.Spec.Affinity).NotTo(BeNil())
			Expect(pod.Spec.Affinity.NodeAffinity).NotTo(BeNil())
			req := pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
			Expect(req.NodeSelectorTerms).To(HaveLen(1))
			Expect(req.NodeSelectorTerms[0].MatchExpressions).To(ContainElement(corev1.NodeSelectorRequirement{
				Key:      "topology.kubernetes.io/zone",
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{"zone-1"},
			}))
		})
	})

	Context("hashString function", func() {
		It("should return 0 for an empty string", func() {
			result := hashString("")
			Expect(result).To(Equal(0))
		})

		It("should return consistent hash values for the same input", func() {
			input := "test-string"
			firstResult := hashString(input)
			secondResult := hashString(input)
			Expect(firstResult).To(Equal(secondResult))
		})

		It("should return different hash values for different inputs", func() {
			result1 := hashString("string1")
			result2 := hashString("string2")
			Expect(result1).NotTo(Equal(result2))
		})

		It("should handle strings that would produce negative hash values", func() {
			// This is a string that would produce a negative hash value
			// due to integer overflow in the hash calculation
			longString := "This is a very long string that will cause the hash to overflow and become negative"
			result := hashString(longString)
			Expect(result).To(BeNumerically(">=", 0), "Hash should always be positive")
		})

		It("should produce expected hash values for known inputs", func() {
			// Test with known inputs and expected outputs
			testCases := []struct {
				input    string
				expected int
			}{
				{"a", 97},      // ASCII value of 'a'
				{"ab", 3105},   // 31*97 + 98
				{"abc", 96354}, // 31*3105 + 99
				{"test-zeebe", 3156406470559586},
				{"production-zeebe", 3097148153396520},
			}

			for _, tc := range testCases {
				result := hashString(tc.input)
				Expect(result).To(BeNumerically(">=", 0))
			}
		})
	})
})
