package k8s

import (
	"context"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const testProfile = `
runtimeClassName: "gVisor"
podSecurityContext:
    runAsUser: 1000
    runAsGroup: 3000
    fsGroup: 2000
affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key: kubernetes.io/e2e-az-name
            operator: In
            values:
            - e2e-az1
            - e2e-az2
tolerations:
- key: "key1"
  operator: "Equal"
  value: "value1"
  effect: "NoExecute"
dnsPolicy: "None"
dnsConfig:
  nameservers:
    - 1.2.3.4
`

// invalidProfileYAML has a missing colon
const invalidProfileYAML = `
tolerations:
- key: "key1"
  operator: "Equal"
  value: "value1"
  effect "NoExecute"
`

func Test_ParseProfileNames(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		expected    []string
	}{
		{
			name: "empty annotations returns nil list",
		},
		{
			name: "if annotation is missing, returns nil list",
			annotations: map[string]string{
				"something.else": "foo",
			},
		},
		{
			name: "parses profile csv string",
			annotations: map[string]string{
				ProfileAnnotationKey: "name1,name2",
			},
			expected: []string{"name1", "name2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseProfileNames(tc.annotations)
			if !reflect.DeepEqual(tc.expected, got) {
				t.Fatalf("expected %#v, got %#v", tc.expected, got)
			}
		})
	}
}

func Test_ProfilesToRemove(t *testing.T) {
	cases := []struct {
		name      string
		requested map[string]string
		existing  map[string]string
		expected  []string
	}{
		{
			name: "empty annotations returns nil list",
		},
		{
			name: "requested non-empty and existing is empty, returns nil list",
			requested: map[string]string{
				ProfileAnnotationKey: "name1,name2",
			},
		},
		{
			name: "missing profile annotation on request and existing, returns nil list",
			requested: map[string]string{
				"something.else": "name1,name2",
			},
			existing: map[string]string{
				"one.more": "here",
			},
		},
		{
			name: "matching profile annotation on request and existing, returns nil list",
			requested: map[string]string{
				ProfileAnnotationKey: "name1,name2",
			},
			existing: map[string]string{
				ProfileAnnotationKey: "name1,name2",
			},
		},
		{
			name: "overlapping annotations on request and existing, returns extras from existing",
			requested: map[string]string{
				ProfileAnnotationKey: "name2",
			},
			existing: map[string]string{
				ProfileAnnotationKey: "name1,name2",
			},
			expected: []string{"name1"},
		},
		{
			name: "empty annotation on request and non-empty existing, returns all existing names",
			existing: map[string]string{
				ProfileAnnotationKey: "name1,name2",
			},
			expected: []string{"name1", "name2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ProfilesToRemove(tc.requested, tc.existing)
			if !reflect.DeepEqual(tc.expected, got) {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func Test_TolerationsProfile_Apply(t *testing.T) {
	expectedTolerations := []corev1.Toleration{
		{
			Key:      "foo",
			Value:    "fooValue",
			Operator: apiv1.TolerationOpEqual,
		},
	}
	p := Profile{Tolerations: expectedTolerations}

	basicDeployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: apiv1.PodTemplateSpec{
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{Name: "testfunc", Image: "alpine:latest"},
					},
				},
			},
		},
	}

	factory := mockFactory()
	factory.ApplyProfile(p, basicDeployment)
	result := basicDeployment.Spec.Template.Spec.Tolerations
	if !reflect.DeepEqual(expectedTolerations, result) {
		t.Fatalf("expected %v, got %v", expectedTolerations, result)
	}
}

func Test_RunTimeClassProfile_Apply(t *testing.T) {
	expectedClass := "fastRunTime"
	p := Profile{RuntimeClassName: &expectedClass}

	basicDeployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: apiv1.PodTemplateSpec{
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{Name: "testfunc", Image: "alpine:latest"},
					},
				},
			},
		},
	}

	factory := mockFactory()
	factory.ApplyProfile(p, basicDeployment)
	result := basicDeployment.Spec.Template.Spec.RuntimeClassName
	if result == nil {
		t.Fatalf("expected %s, got nil", expectedClass)
	}
	if expectedClass != *result {
		t.Fatalf("expected %s, got %v", expectedClass, *result)
	}
}

func Test_RunTimeClassProfile_Remove(t *testing.T) {
	t.Run("remove matching runtime class ", func(t *testing.T) {
		expectedClass := "fastRunTime"
		p := Profile{RuntimeClassName: &expectedClass}

		basicDeployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: apiv1.PodTemplateSpec{
					Spec: apiv1.PodSpec{
						RuntimeClassName: &expectedClass,
						Containers: []apiv1.Container{
							{Name: "testfunc", Image: "alpine:latest"},
						},
					},
				},
			},
		}

		factory := mockFactory()
		factory.RemoveProfile(p, basicDeployment)
		result := basicDeployment.Spec.Template.Spec.RuntimeClassName
		if result != nil {
			t.Fatalf("expected nil, got %s", *result)
		}
	})

	t.Run("leaves runtime class that does not match", func(t *testing.T) {
		expectedClass := "fastRunTime"
		profileClass := "slowRunTime"
		p := Profile{RuntimeClassName: &profileClass}

		basicDeployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: apiv1.PodTemplateSpec{
					Spec: apiv1.PodSpec{
						RuntimeClassName: &expectedClass,
						Containers: []apiv1.Container{
							{Name: "testfunc", Image: "alpine:latest"},
						},
					},
				},
			},
		}

		factory := mockFactory()
		factory.RemoveProfile(p, basicDeployment)
		result := basicDeployment.Spec.Template.Spec.RuntimeClassName
		if !equalStrings(result, &expectedClass) {
			t.Fatalf("expected %s, got %v", expectedClass, result)
		}
	})
}

func Test_TolerationsProfile_Remove(t *testing.T) {
	tolerations := []corev1.Toleration{
		{
			Key:      "foo",
			Value:    "fooValue",
			Operator: apiv1.TolerationOpEqual,
		},
	}
	nonProfileToleration := corev1.Toleration{
		Key:      "second-key",
		Value:    "anotherValue",
		Operator: apiv1.TolerationOpEqual,
	}

	p := Profile{Tolerations: tolerations}

	basicDeployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: apiv1.PodTemplateSpec{
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{Name: "testfunc", Image: "alpine:latest"},
					},
					Tolerations: append(tolerations, nonProfileToleration),
				},
			},
		},
	}

	factory := mockFactory()
	factory.RemoveProfile(p, basicDeployment)

	got := basicDeployment.Spec.Template.Spec.Tolerations
	expected := []corev1.Toleration{nonProfileToleration}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
}

func Test_AffinityProfile_Apply(t *testing.T) {
	expectedAffinity := corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchFields: []corev1.NodeSelectorRequirement{
							{
								Key:      "gpu",
								Operator: apiv1.NodeSelectorOpExists,
							},
						},
					},
				},
			},
		},
	}
	p := Profile{Affinity: &expectedAffinity}

	basicDeployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: apiv1.PodTemplateSpec{
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{Name: "testfunc", Image: "alpine:latest"},
					},
				},
			},
		},
	}

	factory := mockFactory()
	factory.ApplyProfile(p, basicDeployment)
	result := basicDeployment.Spec.Template.Spec.Affinity
	if !reflect.DeepEqual(&expectedAffinity, result) {
		t.Fatalf("expected %+v\n got %+v", &expectedAffinity, result)
	}
}

func Test_AffinityProfile_Remove(t *testing.T) {
	t.Run("removes matching affinity definition", func(t *testing.T) {
		affinity := corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchFields: []corev1.NodeSelectorRequirement{
								{
									Key:      "gpu",
									Operator: apiv1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
		}
		p := Profile{Affinity: &affinity}

		basicDeployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: apiv1.PodTemplateSpec{
					Spec: apiv1.PodSpec{
						Affinity: &affinity,
						Containers: []apiv1.Container{
							{Name: "testfunc", Image: "alpine:latest"},
						},
					},
				},
			},
		}

		factory := mockFactory()
		factory.RemoveProfile(p, basicDeployment)
		result := basicDeployment.Spec.Template.Spec.Affinity
		if result != nil {
			t.Fatalf("expected nil\n got %+v", result)
		}
	})

	t.Run("does not remove non-matching affinity definition", func(t *testing.T) {
		profileAffinity := corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchFields: []corev1.NodeSelectorRequirement{
								{
									Key:      "gpu",
									Operator: apiv1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
		}
		p := Profile{Affinity: &profileAffinity}

		expectedAffinity := corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchFields: []corev1.NodeSelectorRequirement{
								{
									Key:      "bigcpu",
									Operator: apiv1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
		}
		basicDeployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: apiv1.PodTemplateSpec{
					Spec: apiv1.PodSpec{
						Affinity: &expectedAffinity,
						Containers: []apiv1.Container{
							{Name: "testfunc", Image: "alpine:latest"},
						},
					},
				},
			},
		}

		factory := mockFactory()
		factory.RemoveProfile(p, basicDeployment)
		result := basicDeployment.Spec.Template.Spec.Affinity

		// the GPU affinity profile _should not_ remove the bigcpu affinity
		if !reflect.DeepEqual(&expectedAffinity, result) {
			t.Fatalf("expected %+v\n got %+v", &expectedAffinity, result)
		}
	})
}

func Test_PodSecurityProfile_Apply(t *testing.T) {
	expectedProfile := apiv1.PodSecurityContext{
		RunAsUser:  intp(1001),
		RunAsGroup: intp(2002),
	}
	p := Profile{PodSecurityContext: &expectedProfile}

	basicDeployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: apiv1.PodTemplateSpec{
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{Name: "testfunc", Image: "alpine:latest"},
					},
				},
			},
		},
	}

	factory := mockFactory()
	factory.ApplyProfile(p, basicDeployment)
	result := basicDeployment.Spec.Template.Spec.SecurityContext
	if !reflect.DeepEqual(&expectedProfile, result) {
		t.Fatalf("expected %+v\n got %+v", &expectedProfile, result)
	}
}

func Test_PodSecurityProfile_Remove(t *testing.T) {
	p := Profile{PodSecurityContext: &apiv1.PodSecurityContext{
		RunAsUser:  intp(1001),
		RunAsGroup: intp(2002),
	}}

	runAsNonRoot := true
	expectedProfile := &apiv1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot}
	basicDeployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: apiv1.PodTemplateSpec{
				Spec: apiv1.PodSpec{
					SecurityContext: &apiv1.PodSecurityContext{
						RunAsUser:    intp(1001),
						RunAsGroup:   intp(2002),
						RunAsNonRoot: &runAsNonRoot,
					},
					Containers: []apiv1.Container{
						{Name: "testfunc", Image: "alpine:latest"},
					},
				},
			},
		},
	}

	factory := mockFactory()
	factory.RemoveProfile(p, basicDeployment)
	result := basicDeployment.Spec.Template.Spec.SecurityContext
	if !reflect.DeepEqual(expectedProfile, result) {
		t.Fatalf("expected %+v\n got %+v", &expectedProfile, result)
	}
}

func Test_DNSPolicyProfile_Apply(t *testing.T) {
	expectedPolicy := corev1.DNSDefault
	p := Profile{DNSPolicy: &expectedPolicy}

	basicDeployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: apiv1.PodTemplateSpec{
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{Name: "testfunc", Image: "alpine:latest"},
					},
				},
			},
		},
	}

	factory := mockFactory()
	factory.ApplyProfile(p, basicDeployment)
	result := basicDeployment.Spec.Template.Spec.DNSPolicy
	if expectedPolicy != result {
		t.Fatalf("expected %s, got %v", expectedPolicy, result)
	}
}

func Test_DNSPolicyProfile_Remove(t *testing.T) {
	t.Run("remove matching DNS policy ", func(t *testing.T) {
		initialPolicy := corev1.DNSDefault
		p := Profile{DNSPolicy: &initialPolicy}

		basicDeployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: apiv1.PodTemplateSpec{
					Spec: apiv1.PodSpec{
						DNSPolicy: initialPolicy,
						Containers: []apiv1.Container{
							{Name: "testfunc", Image: "alpine:latest"},
						},
					},
				},
			},
		}

		factory := mockFactory()
		factory.RemoveProfile(p, basicDeployment)
		result := basicDeployment.Spec.Template.Spec.DNSPolicy
		if result != corev1.DNSClusterFirst {
			t.Fatalf("expected DNSClusterFirst, got %s", result)
		}
	})

	t.Run("leaves DNS Policy that does not match", func(t *testing.T) {
		expectedPolicy := corev1.DNSClusterFirst
		profilePolicy := corev1.DNSDefault
		p := Profile{DNSPolicy: &profilePolicy}

		basicDeployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: apiv1.PodTemplateSpec{
					Spec: apiv1.PodSpec{
						DNSPolicy: expectedPolicy,
						Containers: []apiv1.Container{
							{Name: "testfunc", Image: "alpine:latest"},
						},
					},
				},
			},
		}

		factory := mockFactory()
		factory.RemoveProfile(p, basicDeployment)
		result := basicDeployment.Spec.Template.Spec.DNSPolicy
		if result != expectedPolicy {
			t.Fatalf("expected %s, got %v", expectedPolicy, result)
		}
	})
}

func Test_DNSConfigProfile_Apply(t *testing.T) {
	optionValue := "2"
	expectedConfig := corev1.PodDNSConfig{
		Nameservers: []string{"1.2.3.4"},
		Searches: []string{
			"ns1.svc.cluster-domain.example",
			"my.dns.search.suffix",
		},
		Options: []corev1.PodDNSConfigOption{
			{Name: "ndots", Value: &optionValue},
			{Name: "edns0"},
		},
	}
	p := Profile{DNSConfig: &expectedConfig}

	basicDeployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: apiv1.PodTemplateSpec{
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{Name: "testfunc", Image: "alpine:latest"},
					},
				},
			},
		},
	}

	factory := mockFactory()
	factory.ApplyProfile(p, basicDeployment)
	result := basicDeployment.Spec.Template.Spec.DNSConfig
	if result == nil {
		t.Fatalf("expected %+v\n got nil", &expectedConfig)
	}
	if !reflect.DeepEqual(&expectedConfig, result) {
		t.Fatalf("expected %+v\n got %+v", &expectedConfig, result)
	}
}

func Test_DNSConfigProfile_Remove(t *testing.T) {
	t.Run("remove matching DNS Config ", func(t *testing.T) {
		expectedConfig := corev1.PodDNSConfig{
			Nameservers: []string{"1.2.3.4"},
		}
		p := Profile{DNSConfig: &expectedConfig}

		basicDeployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: apiv1.PodTemplateSpec{
					Spec: apiv1.PodSpec{
						DNSConfig: &expectedConfig,
						Containers: []apiv1.Container{
							{Name: "testfunc", Image: "alpine:latest"},
						},
					},
				},
			},
		}

		factory := mockFactory()
		factory.RemoveProfile(p, basicDeployment)
		result := basicDeployment.Spec.Template.Spec.DNSConfig
		if result != nil {
			t.Fatalf("expected nil, got %+v", result)
		}
	})

	t.Run("leaves DNS Config that does not match", func(t *testing.T) {
		expectedConfig := corev1.PodDNSConfig{
			Nameservers: []string{"1.2.3.4"},
		}
		profileConfig := corev1.PodDNSConfig{
			Nameservers: []string{"8.8.8.8"},
		}
		p := Profile{DNSConfig: &profileConfig}

		basicDeployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: apiv1.PodTemplateSpec{
					Spec: apiv1.PodSpec{
						DNSConfig: &expectedConfig,
						Containers: []apiv1.Container{
							{Name: "testfunc", Image: "alpine:latest"},
						},
					},
				},
			},
		}

		factory := mockFactory()
		factory.RemoveProfile(p, basicDeployment)
		result := basicDeployment.Spec.Template.Spec.DNSConfig
		if !reflect.DeepEqual(&expectedConfig, result) {
			t.Fatalf("expected %+v\n got %+v", &expectedConfig, result)
		}
	})
}

func Test_ConfigMapProfileParsing(t *testing.T) {
	ctx := context.Background()
	validConfig := corev1.ConfigMap{}
	validConfig.Name = "allowSpot"
	validConfig.Namespace = "functions"
	validConfig.Data = map[string]string{"profile": testProfile}

	runtime := "gVisor"
	dnsPolicy := corev1.DNSNone
	dnsConfig := corev1.PodDNSConfig{
		Nameservers: []string{"1.2.3.4"},
	}
	allowSpot := Profile{
		RuntimeClassName: &runtime,
		Tolerations: []apiv1.Toleration{
			{
				Key:               "key1",
				Value:             "value1",
				Operator:          apiv1.TolerationOpEqual,
				Effect:            apiv1.TaintEffectNoExecute,
				TolerationSeconds: nil,
			},
		},
		PodSecurityContext: &apiv1.PodSecurityContext{
			RunAsUser:  intp(1000),
			RunAsGroup: intp(3000),
			FSGroup:    intp(2000),
		},
		Affinity: &apiv1.Affinity{
			NodeAffinity: &apiv1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &apiv1.NodeSelector{
					NodeSelectorTerms: []apiv1.NodeSelectorTerm{
						{
							MatchExpressions: []apiv1.NodeSelectorRequirement{
								{
									Key:      "kubernetes.io/e2e-az-name",
									Operator: apiv1.NodeSelectorOpIn,
									Values:   []string{"e2e-az1", "e2e-az2"},
								},
							},
						},
					},
				},
			},
		},
		DNSPolicy: &dnsPolicy,
		DNSConfig: &dnsConfig,
	}

	invalidConfig := corev1.ConfigMap{}
	invalidConfig.Name = "allowSpot"
	invalidConfig.Namespace = "functions"
	invalidConfig.Data = map[string]string{"profile": invalidProfileYAML}

	cases := []struct {
		name        string
		namespace   string
		profileName string
		configmap   corev1.ConfigMap
		expected    []Profile
		err         string
	}{
		{
			name:        "unknown profile returns error",
			namespace:   "functions",
			profileName: "unknown",
			err:         `configmaps "unknown" not found`,
		},
		{
			name:        "yaml profile parsed correctly",
			namespace:   "functions",
			profileName: "allowSpot",
			configmap:   validConfig,
			expected:    []Profile{allowSpot},
		},
		{
			name:        "yaml parsing errors are returned",
			namespace:   "functions",
			profileName: "allowSpot",
			configmap:   invalidConfig,
			err:         `error converting YAML to JSON: yaml: line 7: could not find expected ':'`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			factory := FunctionFactory{
				Client: fake.NewSimpleClientset(&tc.configmap),
			}
			client := factory.NewConfigMapProfileClient()
			got, err := client.Get(ctx, tc.namespace, tc.profileName)
			if tc.err != "" {
				if err == nil {
					t.Fatalf("expected error %s, got nil", tc.err)
				}

				if tc.err != err.Error() {
					t.Fatalf("expected error %s, got %s", tc.err, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error %s", err.Error())
			}

			if !reflect.DeepEqual(tc.expected, got) {
				t.Fatalf("\nwant %#v\n got %#v", tc.expected, got)
			}

		})
	}
}

func intp(v int64) *int64 {
	return &v
}
