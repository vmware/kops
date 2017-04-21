/*
Copyright 2017 The Kubernetes Authors.

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

package util

// validate_usage houses the logic to validate usage of a kops command for a given cloud provider.

import (
	"fmt"
	"k8s.io/kops/pkg/featureflag"
	"k8s.io/kops/upup/pkg/fi"
)

const (
	CreateWithFileName     = "create -f"
	CreateInstanceGroup    = "create ig"
	DeleteInstanceGroup    = "delete ig"
	DeleteWithFileName     = "delete -f"
	EditCluster            = "edit cluster"
	EditInstanceGroup      = "edit ig"
	EditFederation         = "edit federation"
	ImportCluster          = "import cluster"
	ReplaceUsingFileName   = "replace -f"
	RollingUpdate          = "rolling-update cluster"
	ToolboxConvertImported = "toolbox convert-imported"
	UpdateCluster          = "update cluster"
	UpdateFederation       = "update federation"
	UpgradeCluster         = "upgrade cluster"
)

var unsupportedCommandsForVSphere = map[string]bool{
	CreateWithFileName:     true,
	CreateInstanceGroup:    true,
	DeleteInstanceGroup:    true,
	DeleteWithFileName:     true,
	EditCluster:            true,
	EditInstanceGroup:      true,
	EditFederation:         true,
	ImportCluster:          true,
	ReplaceUsingFileName:   true,
	RollingUpdate:          true,
	ToolboxConvertImported: true,
	UpdateCluster:          true,
	UpdateFederation:       true,
	UpgradeCluster:         true,
}

// ValidateUsage checks if the given command is usable for the given cloud provider. In case it's not, error is returned, else nil.
func ValidateUsage(command, cloudProvider string) error {

	if fi.CloudProviderVSphere == fi.CloudProviderID(cloudProvider) {
		if !featureflag.VSphereCloudProvider.Enabled() {
			return fmt.Errorf("Feature flag VSphereCloudProvider is not set. Cloud vSphere will not be supported.")
		}
		if unsupportedCommandsForVSphere[command] {
			return fmt.Errorf("Command %v is not yet supported for vsphere cloud provider.", command)
		}
	}

	return nil
}
