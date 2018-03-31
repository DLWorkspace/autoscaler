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

package aztools

import (
	"reflect"
	"testing"
)

func TestInitSupportedMachineTypes(t *testing.T) {
	vmTypes, err := initSupportedMachineTypes()
	if err != nil {
		t.Fatal(err)
	}

	testTypes := MachineInfo{
		"Standard_NC6": VmSpec{
			Cpu:    6,
			Memory: 56339,
			Gpu:    1,
		},
	}

	if !reflect.DeepEqual(vmTypes, testTypes) {
		t.Fatalf("expected: %#v, but got: %#v", testTypes, vmTypes)
	}
}
