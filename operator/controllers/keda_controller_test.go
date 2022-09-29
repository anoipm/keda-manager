package controllers

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"

	rtypes "github.com/kyma-project/module-manager/operator/pkg/types"

	"github.com/kyma-project/keda-manager/operator/api/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Keda controller", func() {
	Context("When creating fresh instance", func() {
		const (
			namespaceName            = "keda"
			kedaName                 = "test"
			operatorName             = "keda-manager"
			serviceAccountName       = "keda-manager"
			serviceAccountLabelCount = 7
			deploymentsCount         = 2
		)

		var (
			metricsDeploymentName     = fmt.Sprintf("%s-metrics-apiserver", operatorName)
			kedaDeploymentName        = operatorName
			notDefaultLogLevel        = v1alpha1.LogLevelDebug
			notDefaultLogFormat       = v1alpha1.LogFormatJSON
			notDefaultLogTimeEncoding = v1alpha1.TimeEncodingEpoch
			kedaSpec                  = v1alpha1.KedaSpec{
				Logging: &v1alpha1.LoggingCfg{
					Operator: &v1alpha1.LoggingOperatorCfg{
						Level:        &notDefaultLogLevel,
						Format:       &notDefaultLogFormat,
						TimeEncoding: &notDefaultLogTimeEncoding,
					},
					MetricsServer: &v1alpha1.LoggingMetricsSrvCfg{
						//TODO: in values.yaml there is level "0" - it's incompatible with LogLevel enum
						Level: &notDefaultLogLevel,
					},
				},
				Resources: &v1alpha1.Resources{
					Operator: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("171m"),
							corev1.ResourceMemory: resource.MustParse("172Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("173m"),
							corev1.ResourceMemory: resource.MustParse("174Mi"),
						},
					},
					MetricsServer: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("175m"),
							corev1.ResourceMemory: resource.MustParse("176Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("177m"),
							corev1.ResourceMemory: resource.MustParse("178Mi"),
						},
					},
				},
				Env: []v1alpha1.NameValue{
					{
						Name:  "some-env-name",
						Value: "some-env-value",
					},
					{
						Name:  "other-env-name",
						Value: "other-env-value",
					},
				},
			}
		)

		It("The status should be Success", func() {
			h := testHelper{
				ctx:           context.Background(),
				namespaceName: namespaceName,
			}
			h.createNamespace()

			// operations like C(R)UD can be tested in separated tests,
			// but we have time-consuming flow and decided do it in one test
			startKedaAndCheckIfIsReady(h, kedaName, kedaDeploymentName, metricsDeploymentName, kedaSpec)

			checkIfKedaCrdSpecIsPassedToObject(h, kedaDeploymentName, metricsDeploymentName, kedaSpec)

			// we check one of kubernetes objects
			checkIfServiceAccountExists(h, serviceAccountName, serviceAccountLabelCount)

			//TODO: disabled because of bug in operator (https://github.com/kyma-project/module-manager/issues/94)
			//updateKedaAndCheckIfIsUpdated(h, kedaName, kedaDeploymentName)

			deleteKedaAndCheckThatObjectsDisappear(h, kedaName, deploymentsCount)
		})
	})
})

func startKedaAndCheckIfIsReady(h testHelper, kedaName, kedaDeploymentName, metricsDeploymentName string, kedaSpec v1alpha1.KedaSpec) {
	// act
	h.createKeda(kedaName, kedaSpec)

	// we have to update deployment status manually
	h.updateDeploymentStatus(metricsDeploymentName)
	h.updateDeploymentStatus(kedaDeploymentName)

	// assert
	Eventually(h.createGetKedaStateFunc(kedaName)).
		WithPolling(time.Second * 2).
		WithTimeout(time.Second * 20).
		Should(Equal(rtypes.StateReady))
}

func deleteKedaAndCheckThatObjectsDisappear(h testHelper, kedaName string, startedDeploymentCount int) {
	// initial assert
	// maybe we should check also other kinds of kubernetes objects
	// Service, CRD(keda), ServiceAccount, ClusterRole, ClusterRoleBinding
	Expect(h.getKubernetesDeploymentCount()).To(Equal(startedDeploymentCount))

	// act
	var keda v1alpha1.Keda
	Eventually(h.createGetKubernetesObjectFunc(kedaName, &keda)).
		WithPolling(time.Second * 2).
		WithTimeout(time.Second * 10).
		Should(BeTrue())
	Expect(k8sClient.Delete(h.ctx, &keda)).To(Succeed())

	// assert
	Eventually(h.getKubernetesDeploymentCount).
		WithPolling(time.Second * 2).
		WithTimeout(time.Second * 10).
		Should(Equal(0))
}

func updateKedaAndCheckIfIsUpdated(h testHelper, kedaName string, kedaDeploymentName string) {
	// arrange
	var keda v1alpha1.Keda
	Eventually(h.createGetKubernetesObjectFunc(kedaName, &keda)).
		WithPolling(time.Second * 2).
		WithTimeout(time.Second * 10).
		Should(BeTrue())

	const (
		envKey = "update-test-env-key"
		envVal = "update-test-env-value"
	)
	keda.Spec.Env = append(keda.Spec.Env, v1alpha1.NameValue{
		Name:  envKey,
		Value: envVal,
	})

	// act
	Expect(k8sClient.Update(h.ctx, &keda)).To(Succeed())

	// assert
	var deployment appsv1.Deployment
	Eventually(h.createGetKubernetesObjectFunc(kedaDeploymentName, &deployment)).
		WithPolling(time.Second * 2).
		WithTimeout(time.Second * 10).
		Should(BeTrue())

	envIndex := -1
	for i, env := range deployment.Spec.Template.Spec.Containers[0].Env {
		if env.Name == envKey {
			envIndex = i
			break
		}
	}
	Expect(envIndex).To(Not(Equal(-1)))
	Expect(deployment.Spec.Template.Spec.Containers[0].Env[envIndex]).To(Equal(envVal))
}

func checkIfServiceAccountExists(h testHelper, serviceAccountName string, expectedLabelCount int) {
	// assert
	var serviceAccount corev1.ServiceAccount
	Eventually(h.createGetKubernetesObjectFunc(serviceAccountName, &serviceAccount)).
		WithPolling(time.Second * 2).
		WithTimeout(time.Second * 10).
		Should(BeTrue())

	Expect(len(serviceAccount.Labels)).To(Equal(expectedLabelCount))
}

func checkIfKedaCrdSpecIsPassedToObject(h testHelper, kedaDeploymentName string, metricsDeploymentName string, kedaSpec v1alpha1.KedaSpec) {
	// act
	var kedaDeployment appsv1.Deployment
	Eventually(h.createGetKubernetesObjectFunc(kedaDeploymentName, &kedaDeployment)).
		WithPolling(time.Second * 2).
		WithTimeout(time.Second * 10).
		Should(BeTrue())
	var metricsDeployment appsv1.Deployment
	Eventually(h.createGetKubernetesObjectFunc(metricsDeploymentName, &metricsDeployment)).
		WithPolling(time.Second * 2).
		WithTimeout(time.Second * 10).
		Should(BeTrue())

	expectedEnvs := ToEnvVar(kedaSpec.Env)

	//assert
	firstKedaContainer := kedaDeployment.Spec.Template.Spec.Containers[0]
	Expect(firstKedaContainer.Args).
		To(ContainElement(fmt.Sprintf("--zap-log-level=%s", *kedaSpec.Logging.Operator.Level)))
	Expect(firstKedaContainer.Args).
		To(ContainElement(fmt.Sprintf("--zap-encoder=%s", *kedaSpec.Logging.Operator.Format)))
	Expect(firstKedaContainer.Args).
		To(ContainElement(fmt.Sprintf("--zap-time-encoding=%s", *kedaSpec.Logging.Operator.TimeEncoding)))

	Expect(firstKedaContainer.Resources).To(Equal(*kedaSpec.Resources.Operator))

	Expect(firstKedaContainer.Env).To(ContainElements(expectedEnvs))

	firstMetricsContainer := metricsDeployment.Spec.Template.Spec.Containers[0]
	//TODO: resolve it
	// disabled because of bug (probably) - different enums 0-4 vs debug, info, error
	//Expect(firstMetricsContainer.Args).
	//	To(ContainElement(fmt.Sprintf("--v=%s", *kedaSpec.Logging.MetricsServer.Level)))

	Expect(firstMetricsContainer.Resources).To(Equal(*kedaSpec.Resources.MetricsServer))

	Expect(firstMetricsContainer.Env).To(ContainElements(expectedEnvs))
}

func ToEnvVar(nvs []v1alpha1.NameValue) []corev1.EnvVar {
	var result []corev1.EnvVar
	for _, nv := range nvs {
		result = append(result, corev1.EnvVar{
			Name:  nv.Name,
			Value: nv.Value,
		})
	}
	return result
}

type testHelper struct {
	ctx           context.Context
	namespaceName string
}

func (h *testHelper) getKubernetesDeploymentCount() int {
	var objectList appsv1.DeploymentList
	Expect(k8sClient.List(h.ctx, &objectList)).To(Succeed())
	return len(objectList.Items)
}

func (h *testHelper) createKymaObjectListIsEmptyFunc() func() (bool, error) {
	return func() (bool, error) {
		var nsList appsv1.DeploymentList
		err := k8sClient.List(h.ctx, &nsList)
		if err != nil {
			return false, err
		}
		return len(nsList.Items) == 0, nil
	}
}

func (h *testHelper) createGetKedaStateFunc(kedaName string) func() (rtypes.State, error) {
	return func() (rtypes.State, error) {
		var emptyState = rtypes.State("")
		var keda v1alpha1.Keda
		key := types.NamespacedName{
			Name:      kedaName,
			Namespace: h.namespaceName,
		}
		err := k8sClient.Get(h.ctx, key, &keda)
		if err != nil {
			return emptyState, err
		}
		return keda.Status.State, nil
	}
}

func (h *testHelper) createGetKubernetesObjectFunc(serviceAccountName string, obj client.Object) func() (bool, error) {
	return func() (bool, error) {
		key := types.NamespacedName{
			Name:      serviceAccountName,
			Namespace: h.namespaceName,
		}
		err := k8sClient.Get(h.ctx, key, obj)
		if err != nil {
			return false, err
		}
		return true, err
	}
}

func (h *testHelper) updateDeploymentStatus(deploymentName string) {
	By(fmt.Sprintf("Updating deployment status: %s", deploymentName))
	var deployment appsv1.Deployment
	Eventually(h.createGetKubernetesObjectFunc(deploymentName, &deployment)).
		WithPolling(time.Second * 2).
		WithTimeout(time.Second * 10).
		Should(BeTrue())

	deployment.Status.Conditions = append(deployment.Status.Conditions, appsv1.DeploymentCondition{
		Type:    appsv1.DeploymentAvailable,
		Status:  corev1.ConditionTrue,
		Reason:  "test-reason",
		Message: "test-message",
	})
	deployment.Status.Replicas = 1
	Expect(k8sClient.Status().Update(h.ctx, &deployment)).To(Succeed())

	replicaSetName := h.createReplicaSetForDeployment(deployment)

	var replicaSet appsv1.ReplicaSet
	Eventually(h.createGetKubernetesObjectFunc(replicaSetName, &replicaSet)).
		WithPolling(time.Second * 2).
		WithTimeout(time.Second * 10).
		Should(BeTrue())

	replicaSet.Status.ReadyReplicas = 1
	replicaSet.Status.Replicas = 1
	Expect(k8sClient.Status().Update(h.ctx, &replicaSet)).To(Succeed())

	By(fmt.Sprintf("Deployment status updated: %s", deploymentName))
}

func (h *testHelper) createReplicaSetForDeployment(deployment appsv1.Deployment) string {
	replicaSetName := fmt.Sprintf("%s-replica-set", deployment.Name)
	By(fmt.Sprintf("Creating replica set (for deployment): %s", replicaSetName))
	var (
		trueValue = true
		one       = int32(1)
	)
	replicaSet := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      replicaSetName,
			Namespace: h.namespaceName,
			Labels: map[string]string{
				"app": deployment.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       deployment.Name,
					UID:        deployment.GetUID(),
					Controller: &trueValue,
				},
			},
		},
		// dummy values
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &one,
			Selector: deployment.Spec.Selector,
			Template: deployment.Spec.Template,
		},
	}
	Expect(k8sClient.Create(h.ctx, &replicaSet)).To(Succeed())
	By(fmt.Sprintf("Replica set (for deployment) created: %s", replicaSetName))
	return replicaSetName
}

func (h *testHelper) createKeda(kedaName string, spec v1alpha1.KedaSpec) {
	By(fmt.Sprintf("Creating crd: %s", kedaName))
	keda := v1alpha1.Keda{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kedaName,
			Namespace: h.namespaceName,
		},
		Spec: spec,
	}
	Expect(k8sClient.Create(h.ctx, &keda)).To(Succeed())
	By(fmt.Sprintf("Crd created: %s", kedaName))
}

func (h *testHelper) createNamespace() {
	By(fmt.Sprintf("Creating namespace: %s", h.namespaceName))
	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: h.namespaceName,
		},
	}
	Expect(k8sClient.Create(h.ctx, &namespace)).To(Succeed())
	By(fmt.Sprintf("Namespace created: %s", h.namespaceName))
}
