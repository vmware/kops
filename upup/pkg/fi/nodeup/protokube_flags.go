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

package nodeup

type ProtokubeFlags struct {
	Master        *bool  `json:"master,omitempty" flag:"master"`
	Containerized *bool  `json:"containerized,omitempty" flag:"containerized"`
	LogLevel      *int32 `json:"logLevel,omitempty" flag:"v"`

	DNSProvider *string `json:"dnsProvider,omitempty" flag:"dns"`

	Zone []string `json:"zone,omitempty" flag:"zone"`

	Channels []string `json:"channels,omitempty" flag:"channels"`

	DNSInternalSuffix *string `json:"dnsInternalSuffix,omitempty" flag:"dns-internal-suffix"`
	Cloud             *string `json:"cloud,omitempty" flag:"cloud"`
	// ClusterId flag is required only for vSphere cloud type, to pass cluster id information to protokube. AWS and GCE workflows ignore this flag.
	ClusterId *string `json:"cluster-id,omitempty" flag:"cluster-id"`
	DNSServer *string `json:"dns-server,omitempty" flag:"dns-server"`
}
