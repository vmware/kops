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

import (
	"encoding/base64"
	"fmt"
	"runtime"
	"strings"
	"text/template"

	"github.com/golang/glog"
	"k8s.io/kops"
	api "k8s.io/kops/pkg/apis/kops"
	"k8s.io/kops/pkg/flagbuilder"
	"k8s.io/kops/upup/pkg/fi"
	"k8s.io/kops/upup/pkg/fi/secrets"
	"k8s.io/kops/util/pkg/vfs"
	"k8s.io/kubernetes/pkg/util/sets"
)

const TagMaster = "_kubernetes_master"

// templateFunctions is a simple helper-class for the functions accessible to templates
type templateFunctions struct {
	nodeupConfig *NodeUpConfig

	// cluster is populated with the current cluster
	cluster *api.Cluster
	// instanceGroup is populated with this node's instance group
	instanceGroup *api.InstanceGroup

	// keyStore is populated with a KeyStore, if KeyStore is set
	keyStore fi.CAStore
	// secretStore is populated with a SecretStore, if SecretStore is set
	secretStore fi.SecretStore

	tags sets.String
}

// newTemplateFunctions is the constructor for templateFunctions
func newTemplateFunctions(nodeupConfig *NodeUpConfig, cluster *api.Cluster, instanceGroup *api.InstanceGroup, tags sets.String) (*templateFunctions, error) {
	t := &templateFunctions{
		nodeupConfig:  nodeupConfig,
		cluster:       cluster,
		instanceGroup: instanceGroup,
		tags:          tags,
	}

	if cluster.Spec.SecretStore != "" {
		glog.Infof("Building SecretStore at %q", cluster.Spec.SecretStore)
		p, err := vfs.Context.BuildVfsPath(cluster.Spec.SecretStore)
		if err != nil {
			return nil, fmt.Errorf("error building secret store path: %v", err)
		}

		t.secretStore = secrets.NewVFSSecretStore(p)
	} else {
		return nil, fmt.Errorf("SecretStore not set")
	}

	if cluster.Spec.KeyStore != "" {
		glog.Infof("Building KeyStore at %q", cluster.Spec.KeyStore)
		p, err := vfs.Context.BuildVfsPath(cluster.Spec.KeyStore)
		if err != nil {
			return nil, fmt.Errorf("error building key store path: %v", err)
		}

		t.keyStore = fi.NewVFSCAStore(p)
	} else {
		return nil, fmt.Errorf("KeyStore not set")
	}

	return t, nil
}

func (t *templateFunctions) populate(dest template.FuncMap) {
	dest["Arch"] = func() string {
		return runtime.GOARCH
	}

	dest["CACertificatePool"] = t.CACertificatePool
	dest["CACertificate"] = t.CACertificate
	dest["PrivateKey"] = t.PrivateKey
	dest["Certificate"] = t.Certificate
	dest["AllTokens"] = t.AllTokens
	dest["GetToken"] = t.GetToken

	dest["BuildFlags"] = flagbuilder.BuildFlags
	dest["Base64Encode"] = func(s string) string {
		return base64.StdEncoding.EncodeToString([]byte(s))
	}

	// TODO: We may want to move these to a nodeset / masterset specific thing
	dest["KubeDNS"] = func() *api.KubeDNSConfig {
		return t.cluster.Spec.KubeDNS
	}
	dest["KubeScheduler"] = func() *api.KubeSchedulerConfig {
		return t.cluster.Spec.KubeScheduler
	}
	dest["KubeAPIServer"] = func() *api.KubeAPIServerConfig {
		return t.cluster.Spec.KubeAPIServer
	}
	dest["KubeControllerManager"] = func() *api.KubeControllerManagerConfig {
		return t.cluster.Spec.KubeControllerManager
	}
	dest["KubeProxy"] = t.KubeProxyConfig

	dest["ClusterName"] = func() string {
		return t.cluster.ObjectMeta.Name
	}

	dest["ProtokubeImageName"] = t.ProtokubeImageName
	dest["ProtokubeImagePullCommand"] = t.ProtokubeImagePullCommand

	dest["ProtokubeFlags"] = t.ProtokubeFlags
}

// CACertificatePool returns the set of valid CA certificates for the cluster
func (t *templateFunctions) CACertificatePool() (*fi.CertificatePool, error) {
	if t.keyStore != nil {
		return t.keyStore.CertificatePool(fi.CertificateId_CA)
	}

	// Fallback to direct properties
	glog.Infof("Falling back to direct configuration for keystore")
	cert, err := t.CACertificate()
	if err != nil {
		return nil, err
	}
	if cert == nil {
		return nil, fmt.Errorf("CA certificate not found (with fallback)")
	}
	pool := &fi.CertificatePool{}
	pool.Primary = cert
	return pool, nil
}

// CACertificate returns the primary CA certificate for the cluster
func (t *templateFunctions) CACertificate() (*fi.Certificate, error) {
	return t.keyStore.Cert(fi.CertificateId_CA)
}

// PrivateKey returns the specified private key
func (t *templateFunctions) PrivateKey(id string) (*fi.PrivateKey, error) {
	return t.keyStore.PrivateKey(id)
}

// Certificate returns the specified private key
func (t *templateFunctions) Certificate(id string) (*fi.Certificate, error) {
	return t.keyStore.Cert(id)
}

// AllTokens returns a map of all tokens
func (t *templateFunctions) AllTokens() (map[string]string, error) {
	tokens := make(map[string]string)
	ids, err := t.secretStore.ListSecrets()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		token, err := t.secretStore.FindSecret(id)
		if err != nil {
			return nil, err
		}
		tokens[id] = string(token.Data)
	}
	return tokens, nil
}

// GetToken returns the specified token
func (t *templateFunctions) GetToken(key string) (string, error) {
	token, err := t.secretStore.FindSecret(key)
	if err != nil {
		return "", err
	}
	if token == nil {
		return "", fmt.Errorf("token not found: %q", key)
	}
	return string(token.Data), nil
}

// ProtokubeImageName returns the docker image for protokube
func (t *templateFunctions) ProtokubeImageName() string {
	name := ""
	if t.nodeupConfig.ProtokubeImage != nil && t.nodeupConfig.ProtokubeImage.Name != "" {
		name = t.nodeupConfig.ProtokubeImage.Name
	}
	if name == "" {
		// use current default corresponding to this version of nodeup
		name = kops.DefaultProtokubeImageName()
	}
	return name
}

// ProtokubeImagePullCommand returns the command to pull the image
func (t *templateFunctions) ProtokubeImagePullCommand() string {
	source := ""
	if t.nodeupConfig.ProtokubeImage != nil {
		source = t.nodeupConfig.ProtokubeImage.Source
	}
	if source == "" {
		// Nothing to pull; return dummy value
		return "/bin/true"
	}
	if strings.HasPrefix(source, "http:") || strings.HasPrefix(source, "https:") || strings.HasPrefix(source, "s3:") {
		// We preloaded the image; return a dummy value
		return "/bin/true"
	}
	return "/usr/bin/docker pull " + t.nodeupConfig.ProtokubeImage.Source
}

// IsMaster returns true if we are tagged as a master
func (t *templateFunctions) isMaster() bool {
	return t.hasTag(TagMaster)
}

// Tag returns true if we are tagged with the specified tag
func (t *templateFunctions) hasTag(tag string) bool {
	_, found := t.tags[tag]
	return found
}

// ProtokubeFlags returns the flags object for protokube
func (t *templateFunctions) ProtokubeFlags() *ProtokubeFlags {
	f := &ProtokubeFlags{}

	master := t.isMaster()

	f.Master = fi.Bool(master)
	if master {
		f.Channels = t.nodeupConfig.Channels
	}

	f.LogLevel = fi.Int32(4)
	f.Containerized = fi.Bool(true)

	zone := t.cluster.Spec.DNSZone
	if zone != "" {
		if strings.Contains(zone, ".") {
			// match by name
			f.Zone = append(f.Zone, zone)
		} else {
			// match by id
			f.Zone = append(f.Zone, "*/"+zone)
		}
	} else {
		glog.Warningf("DNSZone not specified; protokube won't be able to update DNS")
		// TODO: Should we permit wildcard updates if zone is not specified?
		//argv = append(argv, "--zone=*/*")
	}

	if t.cluster.Spec.CloudProvider != "" {
		f.Cloud = fi.String(t.cluster.Spec.CloudProvider)

		switch fi.CloudProviderID(t.cluster.Spec.CloudProvider) {
		case fi.CloudProviderAWS:
			f.DNSProvider = fi.String("aws-route53")
		case fi.CloudProviderGCE:
			f.DNSProvider = fi.String("google-clouddns")
		case fi.CloudProviderVSphere:
			f.DNSProvider = fi.String("aws-route53")
			f.ClusterId = fi.String(t.cluster.ObjectMeta.Name)
		default:
			glog.Warningf("Unknown cloudprovider %q; won't set DNS provider")
		}
	}

	f.DNSInternalSuffix = fi.String(".internal." + t.cluster.ObjectMeta.Name)

	return f
}

// KubeProxyConfig builds the KubeProxyConfig configuration object
func (t *templateFunctions) KubeProxyConfig() *api.KubeProxyConfig {
	config := &api.KubeProxyConfig{}
	*config = *t.cluster.Spec.KubeProxy

	// As a special case, if this is the master, we point kube-proxy to the local IP
	// This prevents a circular dependency where kube-proxy can't come up until DNS comes up,
	// which would mean that DNS can't rely on API to come up
	if t.isMaster() {
		glog.Infof("kube-proxy running on the master; setting API endpoint to localhost")
		config.Master = "http://127.0.0.1:8080"
	}

	return config
}
