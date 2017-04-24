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

package terraform

import (
	"encoding/json"
	"fmt"
	"github.com/golang/glog"
	hcl_parser "github.com/hashicorp/hcl/json/parser"
	"io/ioutil"
	"k8s.io/kops/upup/pkg/fi"
	"os"
	"path"
	"strings"
	"sync"
)

type TerraformTarget struct {
	Cloud   fi.Cloud
	Region  string
	Project string

	ClusterName string

	outDir string

	// mutex protects the following items (resources & files)
	mutex sync.Mutex
	// resources is a list of TF items that should be created
	resources []*terraformResource
	// outputs is a list of our TF output variables
	outputs map[string]*terraformOutputVariable
	// files is a map of TF resource files that should be created
	files map[string][]byte
}

func NewTerraformTarget(cloud fi.Cloud, region, project string, outDir string) *TerraformTarget {
	return &TerraformTarget{
		Cloud:   cloud,
		Region:  region,
		Project: project,

		outDir:  outDir,
		files:   make(map[string][]byte),
		outputs: make(map[string]*terraformOutputVariable),
	}
}

var _ fi.Target = &TerraformTarget{}

type terraformResource struct {
	ResourceType string
	ResourceName string
	Item         interface{}
}

type terraformOutputVariable struct {
	Key        string
	Value      *Literal
	ValueArray []*Literal
}

// A TF name can't have dots in it (if we want to refer to it from a literal),
// so we replace them
func tfSanitize(name string) string {
	name = strings.Replace(name, ".", "-", -1)
	name = strings.Replace(name, "/", "--", -1)
	return name
}

func (t *TerraformTarget) AddFile(resourceType string, resourceName string, key string, r fi.Resource) (*Literal, error) {
	id := resourceType + "_" + resourceName + "_" + key

	d, err := fi.ResourceAsBytes(r)
	if err != nil {
		return nil, fmt.Errorf("error rending resource %s %v", id, err)
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	p := path.Join("data", id)
	t.files[p] = d

	l := LiteralExpression(fmt.Sprintf("${file(%q)}", path.Join("${path.module}", p)))
	return l, nil
}

func (t *TerraformTarget) ProcessDeletions() bool {
	// Terraform tracks & performs deletions itself
	return false
}

func (t *TerraformTarget) RenderResource(resourceType string, resourceName string, e interface{}) error {
	res := &terraformResource{
		ResourceType: resourceType,
		ResourceName: resourceName,
		Item:         e,
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.resources = append(t.resources, res)

	return nil
}

func (t *TerraformTarget) AddOutputVariable(key string, literal *Literal) error {
	v := &terraformOutputVariable{
		Key:   key,
		Value: literal,
	}

	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.outputs[key] != nil {
		return fmt.Errorf("duplicate variable: %q", key)
	}
	t.outputs[key] = v

	return nil
}

func (t *TerraformTarget) AddOutputVariableArray(key string, literal *Literal) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.outputs[key] == nil {
		v := &terraformOutputVariable{
			Key: key,
		}
		t.outputs[key] = v
	}
	if t.outputs[key].Value != nil {
		return fmt.Errorf("variable %q is both an array and a scalar", key)
	}

	t.outputs[key].ValueArray = append(t.outputs[key].ValueArray, literal)

	return nil
}

func (t *TerraformTarget) Finish(taskMap map[string]fi.Task) error {
	resourcesByType := make(map[string]map[string]interface{})

	for _, res := range t.resources {
		resources := resourcesByType[res.ResourceType]
		if resources == nil {
			resources = make(map[string]interface{})
			resourcesByType[res.ResourceType] = resources
		}

		tfName := tfSanitize(res.ResourceName)

		if resources[tfName] != nil {
			return fmt.Errorf("duplicate resource found: %s.%s", res.ResourceType, tfName)
		}

		resources[tfName] = res.Item
	}

	providersByName := make(map[string]map[string]interface{})
	if t.Cloud.ProviderID() == fi.CloudProviderGCE {
		providerGoogle := make(map[string]interface{})
		providerGoogle["project"] = t.Project
		providerGoogle["region"] = t.Region
		providersByName["google"] = providerGoogle
	} else if t.Cloud.ProviderID() == fi.CloudProviderAWS {
		providerAWS := make(map[string]interface{})
		providerAWS["region"] = t.Region
		providersByName["aws"] = providerAWS
	} else if t.Cloud.ProviderID() == fi.CloudProviderVSphere {
		providerVSphere := make(map[string]interface{})
		providerVSphere["region"] = t.Region
		providersByName["vsphere"] = providerVSphere
	}

	outputVariables := make(map[string]interface{})
	for _, v := range t.outputs {
		tfName := tfSanitize(v.Key)

		if outputVariables[tfName] != nil {
			return fmt.Errorf("duplicate variable found: %s", tfName)
		}

		tfVar := make(map[string]interface{})
		if v.Value != nil {
			tfVar["value"] = v.Value
		} else {
			dedup := true
			sorted, err := sortLiterals(v.ValueArray, dedup)
			if err != nil {
				return fmt.Errorf("error sorting literals: %v", err)
			}
			tfVar["value"] = sorted
		}
		outputVariables[tfName] = tfVar
	}

	data := make(map[string]interface{})
	data["resource"] = resourcesByType
	if len(providersByName) != 0 {
		data["provider"] = providersByName
	}
	if len(outputVariables) != 0 {
		data["output"] = outputVariables
	}

	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling terraform data to json: %v", err)
	}

	useJson := false

	if useJson {
		t.files["kubernetes.tf"] = jsonBytes
	} else {
		f, err := hcl_parser.Parse(jsonBytes)
		if err != nil {
			return fmt.Errorf("error parsing terraform json: %v", err)
		}

		b, err := hclPrint(f)
		if err != nil {
			return fmt.Errorf("error writing terraform data to output: %v", err)
		}

		t.files["kubernetes.tf"] = b

	}

	for relativePath, contents := range t.files {
		p := path.Join(t.outDir, relativePath)

		err = os.MkdirAll(path.Dir(p), os.FileMode(0755))
		if err != nil {
			return fmt.Errorf("error creating output directory %q: %v", path.Dir(p), err)
		}

		err = ioutil.WriteFile(p, contents, os.FileMode(0644))
		if err != nil {
			return fmt.Errorf("error writing terraform data to output file %q: %v", p, err)
		}
	}

	glog.Infof("Terraform output is in %s", t.outDir)

	return nil
}
