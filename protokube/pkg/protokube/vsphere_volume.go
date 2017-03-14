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

package protokube

import (
	"github.com/golang/glog"
	"net"
)

const EtcdDataKey = "01"
const EtcdDataVolPath = "/mnt/master-" + EtcdDataKey
const EtcdEventKey = "02"
const EtcdEventVolPath = "/mnt/master-" + EtcdEventKey

// TODO Use lsblk or counterpart command to find the actual device details.
const LocalDeviceForDataVol = "/dev/sdb1"
const LocalDeviceForEventsVol = "/dev/sdc1"
const AttachedToValue = "localhost"
const VolStatusValue = "attached"
const EtcdNodeName = "a"
const EtcdClusterName = "main"
const EtcdEventsClusterName = "events"

type VSphereVolumes struct {
	paths map[string]string
}

var _ Volumes = &VSphereVolumes{}

func NewVSphereVolumes() (*VSphereVolumes, error) {
	vsphereVolumes := &VSphereVolumes{
		paths: make(map[string]string),
	}
	vsphereVolumes.paths[EtcdDataKey] = EtcdDataVolPath
	//vsphereVolumes.paths[EtcdEventKey] = EtcdEventVolPath
	glog.Infof("Created new VSphereVolumes instance.")
	return vsphereVolumes, nil
}

func (v *VSphereVolumes) FindVolumes() ([]*Volume, error) {
	var volumes []*Volume
	// etcd data volume and etcd cluster spec.
	{
		vol := &Volume{
			ID:          EtcdDataKey,
			LocalDevice: LocalDeviceForDataVol,
			AttachedTo:  AttachedToValue,
			Mountpoint:  EtcdDataVolPath,
			Status:      VolStatusValue,
			Info: VolumeInfo{
				Description: EtcdClusterName,
			},
		}
		etcdSpec := &EtcdClusterSpec{
			ClusterKey: EtcdClusterName,
			NodeName:   EtcdNodeName,
			NodeNames:  []string{EtcdNodeName},
		}
		vol.Info.EtcdClusters = []*EtcdClusterSpec{etcdSpec}
		volumes = append(volumes, vol)
	}

	// etcd events volume and etcd events cluster spec.
	{
		vol := &Volume{
			ID:          EtcdEventKey,
			LocalDevice: LocalDeviceForEventsVol,
			AttachedTo:  AttachedToValue,
			Mountpoint:  EtcdEventVolPath,
			Status:      VolStatusValue,
			Info: VolumeInfo{
				Description: EtcdEventsClusterName,
			},
		}
		etcdSpec := &EtcdClusterSpec{
			ClusterKey: EtcdEventsClusterName,
			NodeName:   EtcdNodeName,
			NodeNames:  []string{EtcdNodeName},
		}
		vol.Info.EtcdClusters = []*EtcdClusterSpec{etcdSpec}
		volumes = append(volumes, vol)
	}
	glog.Infof("Found volumes: %v", volumes)
	return volumes, nil
}

func (v *VSphereVolumes) AttachVolume(volume *Volume) error {
	// Currently this is a no-op for vSphere. The virtual disks should already be mounted on this VM.
	glog.Infof("All volumes should already be attached. No operation done.")
	return nil
}

func (v *VSphereVolumes) InternalIp() net.IP {
	ipStr := "127.0.0.1"
	ip := net.ParseIP(ipStr)
	return ip
}
