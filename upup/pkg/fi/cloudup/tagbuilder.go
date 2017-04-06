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

/******************************************************************************
* The Kops Tag Builder
*
* Tags are how we manage kops functionality.
*
******************************************************************************/

package cloudup

import (
	"fmt"

	"github.com/golang/glog"
	api "k8s.io/kops/pkg/apis/kops"
	"k8s.io/kops/pkg/apis/kops/util"
	"k8s.io/kops/upup/pkg/fi"
	"k8s.io/kubernetes/pkg/util/sets"
)

func buildCloudupTags(cluster *api.Cluster) (sets.String, error) {
	tags := sets.NewString()

	networking := cluster.Spec.Networking

	if networking == nil || networking.Classic != nil {
		tags.Insert("_networking_classic")
	} else if networking.Kubenet != nil {
		tags.Insert("_networking_kubenet")
	} else if networking.External != nil {
		// external is based on kubenet
		tags.Insert("_networking_kubenet", "_networking_external")
	} else if networking.CNI != nil || networking.Weave != nil || networking.Flannel != nil || networking.Calico != nil || networking.Canal != nil {
		tags.Insert("_networking_cni")
	} else if networking.Kopeio != nil {
		// TODO combine with the External
		// Kopeio is based on kubenet / external
		// TODO combine with External
		tags.Insert("_networking_kubenet", "_networking_external")
	} else {
		return nil, fmt.Errorf("No networking mode set")
	}

	switch cluster.Spec.CloudProvider {
	case "gce":
		{
			tags.Insert("_gce")
		}

	case "aws":
		{
			tags.Insert("_aws")
		}
	case "vsphere":
		{
			tags.Insert("_vsphere")
		}

	default:
		return nil, fmt.Errorf("unknown CloudProvider %q", cluster.Spec.CloudProvider)
	}

	versionTag := ""
	if cluster.Spec.KubernetesVersion != "" {
		sv, err := util.ParseKubernetesVersion(cluster.Spec.KubernetesVersion)
		if err != nil {
			return nil, fmt.Errorf("unable to determine kubernetes version from %q", cluster.Spec.KubernetesVersion)
		}

		if sv.Major == 1 && sv.Minor >= 5 {
			versionTag = "_k8s_1_5"
		} else if sv.Major == 1 && sv.Minor == 4 {
			versionTag = "_k8s_1_4"
		} else {
			// We don't differentiate between these older versions
			versionTag = "_k8s_1_3"
		}
	}
	if versionTag == "" {
		return nil, fmt.Errorf("unable to determine kubernetes version from %q", cluster.Spec.KubernetesVersion)
	} else {
		tags.Insert(versionTag)
	}

	glog.V(4).Infof("tags: %s", tags.List())

	return tags, nil
}

func buildNodeupTags(role api.InstanceGroupRole, cluster *api.Cluster, clusterTags sets.String) (sets.String, error) {
	tags := sets.NewString()

	networking := cluster.Spec.Networking

	if networking == nil {
		return nil, fmt.Errorf("Networking is not set, and should not be nil here")
	}

	if networking.CNI != nil || networking.Weave != nil || networking.Flannel != nil || networking.Calico != nil || networking.Canal != nil {
		// external is based on cni, weave, flannel, calico, etc
		tags.Insert("_networking_cni")
	}

	switch role {
	case api.InstanceGroupRoleNode:
		tags.Insert("_kubernetes_pool")

		// TODO: Should we run _protokube on the nodes?
		tags.Insert("_protokube")

	case api.InstanceGroupRoleMaster:
		tags.Insert("_kubernetes_master")

		if !fi.BoolValue(cluster.Spec.IsolateMasters) {
			// Run this master as a pool node also (start kube-proxy etc)
			tags.Insert("_kubernetes_pool")
		}

		tags.Insert("_protokube")

	case api.InstanceGroupRoleBastion:
		// No tags

	default:
		return nil, fmt.Errorf("Unrecognized role: %v", role)
	}

	// TODO: Replace with list of CNI plugins ?
	if usesCNI(cluster) {
		tags.Insert("_cni_bridge", "_cni_host_local", "_cni_loopback", "_cni_ptp", "_cni_flannel")
		//tags.Insert("_cni_tuning")
	}

	switch fi.StringValue(cluster.Spec.UpdatePolicy) {
	case "": // default
		tags.Insert("_automatic_upgrades")
	case api.UpdatePolicyExternal:
	// Skip applying the tag
	default:
		glog.Warningf("Unrecognized value for UpdatePolicy: %v", fi.StringValue(cluster.Spec.UpdatePolicy))
	}

	if clusterTags.Has("_gce") {
		tags.Insert("_gce")
	}
	if clusterTags.Has("_aws") {
		tags.Insert("_aws")
	}

	return tags, nil
}
