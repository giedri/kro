// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.
package ackekscluster_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	krov1alpha1 "github.com/awslabs/kro/api/v1alpha1"
	ctrlinstance "github.com/awslabs/kro/internal/controller/instance"
	"github.com/awslabs/kro/test/integration/environment"
)

var env *environment.Environment

func TestEKSCluster(t *testing.T) {
	RegisterFailHandler(Fail)
	BeforeSuite(func() {
		var err error
		env, err = environment.New(
			environment.ControllerConfig{
				AllowCRDDeletion: true,
				ReconcileConfig: ctrlinstance.ReconcileConfig{
					DefaultRequeueDuration: 15 * time.Second,
				},
			},
		)
		Expect(err).NotTo(HaveOccurred())
	})
	AfterSuite(func() {
		Expect(env.Stop()).NotTo(HaveOccurred())
	})

	RunSpecs(t, "EKSCluster Suite")
}

var _ = Describe("EKSCluster", func() {
	It("should handle complete lifecycle of ResourceGroup and Instance", func() {
		ctx := context.Background()
		namespace := fmt.Sprintf("test-%s", rand.String(5))

		// Create namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		Expect(env.Client.Create(ctx, ns)).To(Succeed())

		// Create ResourceGroup
		rg, genInstance := eksCluster(namespace, "test-eks-cluster")
		Expect(env.Client.Create(ctx, rg)).To(Succeed())

		// Verify ResourceGroup is created and becomes ready
		createdRG := &krov1alpha1.ResourceGroup{}
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      rg.Name,
				Namespace: namespace,
			}, createdRG)
			g.Expect(err).ToNot(HaveOccurred())

			// Verify the ResourceGroup fields
			g.Expect(createdRG.Spec.Schema.Kind).To(Equal("EKSCluster"))
			g.Expect(createdRG.Spec.Schema.APIVersion).To(Equal("v1alpha1"))
			g.Expect(createdRG.Spec.Resources).To(HaveLen(12)) // All resources from the generator

			g.Expect(createdRG.Status.TopologicalOrder).To(Equal([]string{
				"clusterRole",
				"clusterVPC",
				"clusterInternetGateway",
				"clusterRouteTable",
				"clusterSubnetA",
				"clusterSubnetB",
				"cluster",
				"clusterAdminRole",
				"clusterElasticIPAddress",
				"clusterNATGateway",
				"clusterNodeRole",
				"clusterNodeGroup",
			}))

			// Verify the ResourceGroup status
			g.Expect(createdRG.Status.TopologicalOrder).To(HaveLen(12))
			// Verify conditions
			g.Expect(createdRG.Status.Conditions).To(HaveLen(3))
			g.Expect(createdRG.Status.Conditions[0].Type).To(Equal(krov1alpha1.ResourceGroupConditionTypeReconcilerReady))
			g.Expect(createdRG.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
			g.Expect(createdRG.Status.Conditions[1].Type).To(Equal(krov1alpha1.ResourceGroupConditionTypeGraphVerified))
			g.Expect(createdRG.Status.Conditions[1].Status).To(Equal(metav1.ConditionTrue))
			g.Expect(createdRG.Status.Conditions[2].Type).To(
				Equal(krov1alpha1.ResourceGroupConditionTypeCustomResourceDefinitionSynced),
			)
			g.Expect(createdRG.Status.Conditions[2].Status).To(Equal(metav1.ConditionTrue))
			g.Expect(createdRG.Status.State).To(Equal(krov1alpha1.ResourceGroupStateActive))
		}, 10*time.Second, time.Second).Should(Succeed())

		// Create instance
		instance := genInstance(namespace, "test-instance", "1.27")
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Check if the instance is created
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "test-instance",
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		roleGVK := schema.GroupVersionKind{
			Group:   "iam.services.k8s.aws",
			Version: "v1alpha1",
			Kind:    "Role",
		}
		clusterRole := &unstructured.Unstructured{}
		clusterRole.SetGroupVersionKind(roleGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "kro-cluster-role",
				Namespace: namespace,
			}, clusterRole)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		clusterRole.Object["status"] = map[string]interface{}{
			"ackResourceMetadata": map[string]interface{}{
				"ownerAccountID": "123456789012",
				"region":         "us-west-2",
				"arn":            "arn:aws:iam::123456789012:role/kro-cluster-role",
			},
		}
		Expect(env.Client.Status().Update(ctx, clusterRole)).To(Succeed())

		// 2. Verify VPC
		vpcGVK := schema.GroupVersionKind{
			Group:   "ec2.services.k8s.aws",
			Version: "v1alpha1",
			Kind:    "VPC",
		}
		vpc := &unstructured.Unstructured{}
		vpc.SetGroupVersionKind(vpcGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "kro-cluster-vpc",
				Namespace: namespace,
			}, vpc)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		vpc.Object["status"] = map[string]interface{}{
			"vpcID": "vpc-12345",
		}
		Expect(env.Client.Status().Update(ctx, vpc)).To(Succeed())

		// 3. Verify Internet Gateway
		igwGVK := schema.GroupVersionKind{
			Group:   "ec2.services.k8s.aws",
			Version: "v1alpha1",
			Kind:    "InternetGateway",
		}
		igw := &unstructured.Unstructured{}
		igw.SetGroupVersionKind(igwGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "kro-cluster-igw",
				Namespace: namespace,
			}, igw)
			g.Expect(err).ToNot(HaveOccurred())

			vpcID, found, _ := unstructured.NestedString(igw.Object, "spec", "vpc")
			g.Expect(found).To(BeTrue())
			g.Expect(vpcID).To(Equal("vpc-12345"))
		}, 20*time.Second, time.Second).Should(Succeed())

		igw.Object["status"] = map[string]interface{}{
			"internetGatewayID": "igw-12345",
		}
		Expect(env.Client.Status().Update(ctx, igw)).To(Succeed())

		// 4. Verify Route Table
		rtGVK := schema.GroupVersionKind{
			Group:   "ec2.services.k8s.aws",
			Version: "v1alpha1",
			Kind:    "RouteTable",
		}
		rt := &unstructured.Unstructured{}
		rt.SetGroupVersionKind(rtGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "kro-cluster-public-route-table",
				Namespace: namespace,
			}, rt)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		rt.Object["status"] = map[string]interface{}{
			"routeTableID": "rtb-12345",
		}
		Expect(env.Client.Status().Update(ctx, rt)).To(Succeed())

		// 5-6. Verify Subnets A and B
		subnetGVK := schema.GroupVersionKind{
			Group:   "ec2.services.k8s.aws",
			Version: "v1alpha1",
			Kind:    "Subnet",
		}

		// SubnetA
		subnetA := &unstructured.Unstructured{}
		subnetA.SetGroupVersionKind(subnetGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "kro-cluster-public-subnet1",
				Namespace: namespace,
			}, subnetA)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		subnetA.Object["status"] = map[string]interface{}{
			"subnetID": "subnet-a12345",
		}
		Expect(env.Client.Status().Update(ctx, subnetA)).To(Succeed())

		// SubnetB
		subnetB := &unstructured.Unstructured{}
		subnetB.SetGroupVersionKind(subnetGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "kro-cluster-public-subnet2",
				Namespace: namespace,
			}, subnetB)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		subnetB.Object["status"] = map[string]interface{}{
			"subnetID": "subnet-b12345",
		}
		Expect(env.Client.Status().Update(ctx, subnetB)).To(Succeed())

		// 7. Verify EKS Cluster
		clusterGVK := schema.GroupVersionKind{
			Group:   "eks.services.k8s.aws",
			Version: "v1alpha1",
			Kind:    "Cluster",
		}
		cluster := &unstructured.Unstructured{}
		cluster.SetGroupVersionKind(clusterGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "test-instance",
				Namespace: namespace,
			}, cluster)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		cluster.Object["status"] = map[string]interface{}{
			"ackResourceMetadata": map[string]interface{}{
				"ownerAccountID": "123456789012",
				"region":         "us-west-2",
				"arn":            "arn:aws:eks:us-west-2:123456789012:cluster/test-instance",
			},
		}
		Expect(env.Client.Status().Update(ctx, cluster)).To(Succeed())

		// 8. Verify Admin Role
		adminRole := &unstructured.Unstructured{}
		adminRole.SetGroupVersionKind(roleGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "kro-cluster-pia-role",
				Namespace: namespace,
			}, adminRole)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		adminRole.Object["status"] = map[string]interface{}{
			"ackResourceMetadata": map[string]interface{}{
				"ownerAccountID": "123456789012",
				"region":         "us-west-2",
				"arn":            "arn:aws:iam::123456789012:role/kro-cluster-pia-role",
			},
		}
		Expect(env.Client.Status().Update(ctx, adminRole)).To(Succeed())

		// 9. Verify Elastic IP
		eipGVK := schema.GroupVersionKind{
			Group:   "ec2.services.k8s.aws",
			Version: "v1alpha1",
			Kind:    "ElasticIPAddress",
		}
		eip := &unstructured.Unstructured{}
		eip.SetGroupVersionKind(eipGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "kro-cluster-eip",
				Namespace: namespace,
			}, eip)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		eip.Object["status"] = map[string]interface{}{
			"allocationID": "eipalloc-12345",
		}
		Expect(env.Client.Status().Update(ctx, eip)).To(Succeed())

		// 10. Verify NAT Gateway
		natGVK := schema.GroupVersionKind{
			Group:   "ec2.services.k8s.aws",
			Version: "v1alpha1",
			Kind:    "NATGateway",
		}
		nat := &unstructured.Unstructured{}
		nat.SetGroupVersionKind(natGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "kro-cluster-natgateway1",
				Namespace: namespace,
			}, nat)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		nat.Object["status"] = map[string]interface{}{
			"natGatewayID": "nat-12345",
		}
		Expect(env.Client.Status().Update(ctx, nat)).To(Succeed())

		// 11. Verify Node Role
		nodeRole := &unstructured.Unstructured{}
		nodeRole.SetGroupVersionKind(roleGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "kro-cluster-node-role",
				Namespace: namespace,
			}, nodeRole)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		nodeRole.Object["status"] = map[string]interface{}{
			"ackResourceMetadata": map[string]interface{}{
				"ownerAccountID": "123456789012",
				"region":         "us-west-2",
				"arn":            "arn:aws:iam::123456789012:role/kro-cluster-node-role",
			},
		}
		Expect(env.Client.Status().Update(ctx, nodeRole)).To(Succeed())

		// 12. Verify Node Group
		nodeGroupGVK := schema.GroupVersionKind{
			Group:   "eks.services.k8s.aws",
			Version: "v1alpha1",
			Kind:    "Nodegroup",
		}
		nodeGroup := &unstructured.Unstructured{}
		nodeGroup.SetGroupVersionKind(nodeGroupGVK)
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "kro-cluster-nodegroup",
				Namespace: namespace,
			}, nodeGroup)
			g.Expect(err).ToNot(HaveOccurred())
		}, 20*time.Second, time.Second).Should(Succeed())

		// Verify final instance status
		Eventually(func(g Gomega) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "test-instance",
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())

			networkingInfo, found, _ := unstructured.NestedMap(instance.Object, "status", "networkingInfo")
			g.Expect(found).To(BeTrue())
			g.Expect(networkingInfo["vpcID"]).To(Equal("vpc-12345"))
			g.Expect(networkingInfo["subnetAZA"]).To(Equal("subnet-a12345"))
			g.Expect(networkingInfo["subnetAZB"]).To(Equal("subnet-b12345"))

			clusterARN, found, _ := unstructured.NestedString(instance.Object, "status", "clusterARN")
			g.Expect(found).To(BeTrue())
			g.Expect(clusterARN).To(Equal("arn:aws:eks:us-west-2:123456789012:cluster/test-instance"))
		}, 20*time.Second, time.Second).Should(Succeed())

		// Delete instance
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())

		// Verify instance and all its resources are deleted
		Eventually(func() bool {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      "test-instance",
				Namespace: namespace,
			}, instance)
			return errors.IsNotFound(err)
		}, 60*time.Second, time.Second).Should(BeTrue())

		// Delete ResourceGroup
		Expect(env.Client.Delete(ctx, rg)).To(Succeed())

		// Verify ResourceGroup is deleted
		Eventually(func() bool {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      rg.Name,
				Namespace: namespace,
			}, &krov1alpha1.ResourceGroup{})
			return errors.IsNotFound(err)
		}, 20*time.Second, time.Second).Should(BeTrue())

		// Cleanup namespace
		Expect(env.Client.Delete(ctx, ns)).To(Succeed())
	})
})