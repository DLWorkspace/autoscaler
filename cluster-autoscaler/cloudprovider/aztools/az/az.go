/*
Copyright 2018 The Kubernetes Authors.

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

package az

import (
	"fmt"
	"io/ioutil"

	"k8s.io/utils/exec"

	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
)

// GetWorkerList get the worker list from given cluster.
func GetWorkerList(clusterID string) ([]string, error) {
	data, err := ioutil.ReadFile("./cluster.yaml")
	if err != nil {
		return nil, err
	}

	m := make(map[interface{}]interface{})

	err = yaml.Unmarshal(data, &m)
	if err != nil {
		return nil, err
	}

	if m["cluster_name"] != clusterID {
		return nil, fmt.Errorf("cluster: %v is not found in cluster.yaml, got: %v", clusterID, m["cluster_name"])
	}

	machines := []string{}
	if machineMap, ok := m["machines"]; ok {
		for vm, roleMap := range machineMap.(map[string]interface{}) {
			if roleMap.(map[string]interface{})["role"] == "worker" {
				machines = append(machines, vm)
			}
		}
	}
	return machines, nil
}

// OnScaleUp is a function called on node group increase in AzToolsCloudProvider.
// First parameter is the NodeGroup id, second is the increase delta.
func OnScaleUp(id string, delta int) error {
	// Backup config.yaml
	output, err := execRun("cp", "config.yaml", "deploy/config.yaml.bak")
	if err != nil {
		return fmt.Errorf("%v, %v", err, output)
	}

	// 1. Modify worker number in config.yaml
	modifyConfigYaml(delta)

	// 2. Create new vm
	output, err = execRun("./az_tools.py", "scaleup")
	if err != nil {
		restoreConfig()
		return fmt.Errorf("%v, %v", err, output)
	}

	// Backup cluster.yaml
	output, err = execRun("cp", "cluster.yaml", "deploy/cluster.yaml.bak")
	if err != nil {
		restoreConfig()
		return fmt.Errorf("%v, %v", err, output)
	}

	// 3. Generate new cluster.yaml
	output, err = execRun("./az_tools.py", "genconfig")
	if err != nil {
		restoreConfig()
		restoreClusterConfig()
		return fmt.Errorf("%v, %v", err, output)
	}

	// 4. Run scripts in new workers
	output, err = execRun("./deploy.py", "scriptblocks", "add_scaled_worker")
	if err != nil {
		// TODO(harry): delete the new scaled node.
		restoreConfig()
		restoreClusterConfig()
		return fmt.Errorf("%v, %v", err, output)
	}
	// TODO(harry): should we handle labels separately for `kubernetes labels`
	glog.Infof("Scale up successfully with %v nodes added", delta)
	return nil
}

func restoreConfig() {
	// Restore config.yaml
	execRun("cp", "deploy/config.yaml.bak", "config.yaml")
}

func restoreClusterConfig() {
	// Restore config.yaml
	execRun("cp", "deploy/cluster.yaml.bak", "cluster.yaml")
}

// modifyConfigYaml modifies config.yaml
// TODO(harry): consider use `sed` so we don't need to regenerate yaml
func modifyConfigYaml(delta int) error {
	data, err := ioutil.ReadFile("./config.yaml")
	if err != nil {
		return err
	}

	m := make(map[interface{}]interface{})

	err = yaml.Unmarshal(data, &m)
	if err != nil {
		return err
	}

	if clusterMap, ok := m["azure_cluster"]; ok {
		for name, cluster := range clusterMap.(map[interface{}]interface{}) {
			if num, ok := cluster.(map[interface{}]interface{})["worker_node_num"]; ok {
				// Changes the nodes number to:
				//   worker_node_num: curr + delta
				cluster.(map[interface{}]interface{})["worker_node_num"] = num.(int) + delta
				// Add delta in the file:
				//   last_scaled_node_num: delta
				cluster.(map[interface{}]interface{})["last_scaled_node_num"] = delta
				break
			} else {
				return fmt.Errorf("cluster %v has no worker_node_num defined, autoscaling is not supported for it.", name)
			}
		}
	} else {
		return fmt.Errorf("no azure_cluster defined in config.yaml")
	}

	d, err := yaml.Marshal(&m)
	if err != nil {
		return err
	}

	// Write back
	err = ioutil.WriteFile("config.yaml", d, 0644)
	if err != nil {
		return err
	}

	glog.V(4).Infof("Updated config.yaml with new worker node number.")

	return nil
}

// OnScaleDown is a function called on cluster scale down
func OnScaleDown(id string, nodeName string) error {
	// Backup config.yaml
	output, err := execRun("cp", "config.yaml", "deploy/config.yaml.bak")
	if err != nil {
		return fmt.Errorf("%v, %v", err, output)
	}

	// 1. Modify worker number in config.yaml
	modifyConfigYaml(-1)

	// 2. Delete vm by name
	output, err = execRun("./az_tools.py", "scaledown", nodeName)
	if err != nil {
		restoreConfig()
		return fmt.Errorf("%v, %v", err, output)
	}

	// Backup cluster.yaml
	output, err = execRun("cp", "cluster.yaml", "deploy/cluster.yaml.bak")
	if err != nil {
		restoreConfig()
		return fmt.Errorf("%v, %v", err, output)
	}

	// 3. Generate new cluster.yaml
	output, err = execRun("./az_tools.py", "genconfig")
	if err != nil {
		restoreConfig()
		restoreClusterConfig()
		return fmt.Errorf("%v, %v", err, output)
	}

	// 4. Delete node from kubernetes cluster
	output, err = execRun("./deploy.py", "kubectl", "delete", "node", nodeName)
	if err != nil {
		return fmt.Errorf("%v, %v", err, output)
	}

	glog.Infof("Scale down node: %v successfully", nodeName)
	return nil
}

// OnNodeGroupCreate is a fuction called when a new node group is created.
func OnNodeGroupCreate(id string) error {
	return fmt.Errorf("Not implemented")
}

// OnNodeGroupDelete is a function called when a node group is deleted.
func OnNodeGroupDelete(id string) error {
	return fmt.Errorf("Not implemented")
}

// execRun execute command and return outputs.
func execRun(cmd string, args ...string) ([]byte, error) {
	exe := exec.New()
	glog.V(4).Infof("Executing: %v, %v", cmd, args)
	return exe.Command(cmd, args...).CombinedOutput()
}
