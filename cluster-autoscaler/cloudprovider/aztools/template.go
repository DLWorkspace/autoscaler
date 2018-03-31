/*
Copyright 2016 The Kubernetes Authors.

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

package aztools

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"math/rand"
	"regexp"
	"strings"

	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/autoscaler/cluster-autoscaler/utils/gpu"

	"github.com/golang/glog"
)

const (
	mbPerGB               = 1000
	millicoresPerCore     = 1000
	legacyGPUResourceName = "alpha.kubernetes.io/nvidia-gpu"
)

type VmSpec struct {
	Cpu    int64 `yaml:"cpu"`
	Memory int64 `yaml:"memoryInMb"`
	Gpu    int64 `yaml:"gpu"`
}

type MachineInfo map[string]VmSpec

// initSupportedMachineTypes returns supported vm types from configure file.
func initSupportedMachineTypes() (MachineInfo, error) {
	vmSpecs := MachineInfo{}

	yamlFile, err := ioutil.ReadFile(machineTypesFile)
	if err != nil {
		return nil, fmt.Errorf("yamlFile.Get err   #%v ", err)
	}
	err = yaml.Unmarshal(yamlFile, &vmSpecs)
	if err != nil {
		return nil, fmt.Errorf("Unmarshal: %v", err)
	}

	return vmSpecs, nil
}

func getCpuAndMemoryForMachineType(machineType VmSpec) (cpu int64, mem int64, err error) {
	return parseCustomMachineType(machineType)
}

func buildCapacity(machineType VmSpec) (apiv1.ResourceList, error) {
	capacity := apiv1.ResourceList{}
	// TODO: get a real value.
	capacity[apiv1.ResourcePods] = *resource.NewQuantity(110, resource.DecimalSI)

	cpu, mem, err := getCpuAndMemoryForMachineType(machineType)
	if err != nil {
		return apiv1.ResourceList{}, err
	}
	capacity[apiv1.ResourceCPU] = *resource.NewQuantity(cpu, resource.DecimalSI)
	capacity[apiv1.ResourceMemory] = *resource.NewQuantity(mem, resource.DecimalSI)

	if machineType.Gpu > 0 {
		capacity[gpu.ResourceNvidiaGPU] = *resource.NewQuantity(machineType.Gpu, resource.DecimalSI)
		capacity[legacyGPUResourceName] = *resource.NewQuantity(machineType.Gpu, resource.DecimalSI)
	}

	return capacity, nil
}

func buildNodeFromTemplate(grpID string, machineType VmSpec) (*apiv1.Node, error) {
	node := apiv1.Node{}
	nodeName := fmt.Sprintf("harrydevbox-worker-%s", grpID, rand.Int63())

	node.ObjectMeta = metav1.ObjectMeta{
		Name:     nodeName,
		SelfLink: fmt.Sprintf("/api/v1/nodes/%s", nodeName),
		// TODO(harry): do we really need all these labels?
		Labels: map[string]string{
			"FragmentGPUJob": "active",
			"all":            "active",
			"beta.kubernetes.io/arch":        "amd64",
			"beta.kubernetes.io/os":          "linux",
			"cloud-collectd-node-agent":      "active",
			"cloud-fluentd-es-config-v0.1.0": "active",
			"cloud-fluentd-es-v2.0.2":        "active",
			"collectd-node-agent":            "active",
			"datanode":                       "active",
			"default":                        "active",
			"dlws-grafana":                   "active",
			"elasticsearch-logging":          "active",
			"fluentd-es-config-v0.1.0":       "active",
			"fluentd-es-v2.0.2":              "active",
			"freeflowrouter":                 "active",
			"glusterfs":                      "active",
			"google-cadvisor":                "active",
			"hdfs":                           "active",
			"hdfsdatanode":                   "active",
			"hdfsformat":                     "active",
			"hdfsjournal":                    "active",
			"hdfsnn1":                        "active",
			"hdfsnn2":                        "active",
			"hdfsstandby":                    "active",
			"kubernetes.io/hostname":         nodeName,
			"nginx":                          "active",
			"nvidiaheartbeat":                "active",
			"recogserver":                    "active",
			"sparknode":                      "active",
			"worker":                         "active",
			"yarnnodemanager":                "active",
			"yarnrm1":                        "active",
			"yarnrm2":                        "active",
			"zk":                             "active",
			"zk-config":                      "active",
			"zk-headless":                    "active",
		},
	}

	capacity, err := buildCapacity(machineType)
	if err != nil {
		return nil, err
	}
	node.Status = apiv1.NodeStatus{
		Capacity: capacity,
	}

	glog.Warningf("kube-reserved is not available for aztools, setting allocatable to capacity.")
	node.Status.Allocatable = node.Status.Capacity

	// Ready status
	node.Status.Conditions = cloudprovider.BuildReadyConditions()

	glog.V(6).Infof("Build node: %#v from template %v", node, grpID)
	return &node, nil
}

func parseCustomMachineType(machineType VmSpec) (cpu, mem int64, err error) {
	// example cpu: 2 memory: 2816
	cpu = machineType.Cpu
	// Mb to bytes
	mem = machineType.Memory * 1024 * 1024
	return
}

func parseKubeReserved(kubeReserved string) (apiv1.ResourceList, error) {
	resourcesMap, err := parseKeyValueListToMap([]string{kubeReserved})
	if err != nil {
		return nil, fmt.Errorf("failed to extract kube-reserved from kube-env: %q", err)
	}
	reservedResources := apiv1.ResourceList{}
	for name, quantity := range resourcesMap {
		switch apiv1.ResourceName(name) {
		case apiv1.ResourceCPU, apiv1.ResourceMemory, apiv1.ResourceEphemeralStorage:
			if q, err := resource.ParseQuantity(quantity); err == nil && q.Sign() >= 0 {
				reservedResources[apiv1.ResourceName(name)] = q
			}
		default:
			glog.Warningf("ignoring resource from kube-reserved: %q", name)
		}
	}
	return reservedResources, nil
}

func extractLabelsFromKubeEnv(kubeEnv string) (map[string]string, error) {
	labels, err := extractFromKubeEnv(kubeEnv, "NODE_LABELS")
	if err != nil {
		return nil, err
	}
	return parseKeyValueListToMap(labels)
}

func extractTaintsFromKubeEnv(kubeEnv string) ([]apiv1.Taint, error) {
	taints, err := extractFromKubeEnv(kubeEnv, "NODE_TAINTS")
	if err != nil {
		return nil, err
	}
	taintMap, err := parseKeyValueListToMap(taints)
	if err != nil {
		return nil, err
	}
	return buildTaints(taintMap)
}

func extractKubeReservedFromKubeEnv(kubeEnv string) (string, error) {
	kubeletArgs, err := extractFromKubeEnv(kubeEnv, "KUBELET_TEST_ARGS")
	if err != nil {
		return "", err
	}
	resourcesRegexp := regexp.MustCompile(`--kube-reserved=([^ ]+)`)

	for _, value := range kubeletArgs {
		matches := resourcesRegexp.FindStringSubmatch(value)
		if len(matches) > 1 {
			return matches[1], nil
		}
	}
	return "", fmt.Errorf("kube-reserved not in kubelet args in kube-env: %q", strings.Join(kubeletArgs, " "))
}

func extractFromKubeEnv(kubeEnv, resource string) ([]string, error) {
	result := make([]string, 0)

	for line, env := range strings.Split(kubeEnv, "\n") {
		env = strings.Trim(env, " ")
		if len(env) == 0 {
			continue
		}
		items := strings.SplitN(env, ":", 2)
		if len(items) != 2 {
			return nil, fmt.Errorf("wrong content in kube-env at line: %d", line)
		}
		key := strings.Trim(items[0], " ")
		value := strings.Trim(items[1], " \"'")
		if key == resource {
			result = append(result, value)
		}
	}
	return result, nil
}

func parseKeyValueListToMap(values []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, value := range values {
		for _, val := range strings.Split(value, ",") {
			valItems := strings.SplitN(val, "=", 2)
			if len(valItems) != 2 {
				return nil, fmt.Errorf("error while parsing kube env value: %s", val)
			}
			result[valItems[0]] = valItems[1]
		}
	}
	return result, nil
}

func buildTaints(kubeEnvTaints map[string]string) ([]apiv1.Taint, error) {
	taints := make([]apiv1.Taint, 0)
	for key, value := range kubeEnvTaints {
		values := strings.SplitN(value, ":", 2)
		if len(values) != 2 {
			return nil, fmt.Errorf("error while parsing node taint value and effect: %s", value)
		}
		taints = append(taints, apiv1.Taint{
			Key:    key,
			Value:  values[0],
			Effect: apiv1.TaintEffect(values[1]),
		})
	}
	return taints, nil
}

type allocatableBracket struct {
	threshold            int64
	marginalReservedRate float64
}

func memoryReservedMB(memoryCapacityMB int64) int64 {
	if memoryCapacityMB <= 1*mbPerGB {
		// do not set any memory reserved for nodes with less than 1 Gb of capacity
		return 0
	}
	return calculateReserved(memoryCapacityMB, []allocatableBracket{
		{
			threshold:            0,
			marginalReservedRate: 0.25,
		},
		{
			threshold:            4 * mbPerGB,
			marginalReservedRate: 0.2,
		},
		{
			threshold:            8 * mbPerGB,
			marginalReservedRate: 0.1,
		},
		{
			threshold:            16 * mbPerGB,
			marginalReservedRate: 0.06,
		},
		{
			threshold:            128 * mbPerGB,
			marginalReservedRate: 0.02,
		},
	})
}

func cpuReservedMillicores(cpuCapacityMillicores int64) int64 {
	return calculateReserved(cpuCapacityMillicores, []allocatableBracket{
		{
			threshold:            0,
			marginalReservedRate: 0.06,
		},
		{
			threshold:            1 * millicoresPerCore,
			marginalReservedRate: 0.01,
		},
		{
			threshold:            2 * millicoresPerCore,
			marginalReservedRate: 0.005,
		},
		{
			threshold:            4 * millicoresPerCore,
			marginalReservedRate: 0.0025,
		},
	})
}

// calculateReserved calculates reserved using capacity and a series of
// brackets as follows:  the marginalReservedRate applies to all capacity
// greater than the bracket, but less than the next bracket.  For example, if
// the first bracket is threshold: 0, rate:0.1, and the second bracket has
// threshold: 100, rate: 0.4, a capacity of 100 results in a reserved of
// 100*0.1 = 10, but a capacity of 200 results in a reserved of
// 10 + (200-100)*.4 = 50.  Using brackets with marginal rates ensures that as
// capacity increases, reserved always increases, and never decreases.
func calculateReserved(capacity int64, brackets []allocatableBracket) int64 {
	var reserved float64
	for i, bracket := range brackets {
		c := capacity
		if i < len(brackets)-1 && brackets[i+1].threshold < capacity {
			c = brackets[i+1].threshold
		}
		additionalReserved := float64(c-bracket.threshold) * bracket.marginalReservedRate
		if additionalReserved > 0 {
			reserved += additionalReserved
		}
	}
	return int64(reserved)
}
