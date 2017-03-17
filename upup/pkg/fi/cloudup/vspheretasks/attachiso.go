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

package vspheretasks

import (
	"github.com/golang/glog"
	"k8s.io/kops/upup/pkg/fi"
	"k8s.io/kops/upup/pkg/fi/cloudup/vsphere"
	"k8s.io/kops/pkg/apis/kops"
	"fmt"
	"k8s.io/kops/pkg/model"
	"io/ioutil"
	"path/filepath"
	"os/exec"
	"bytes"
	"os"
)

// AttachISO represents the cloud-init ISO file attached to a VMware VM
//go:generate fitask -type=AttachISO
type AttachISO struct {
	Name *string
	VM   *string
	IG   *kops.InstanceGroup
	BootstrapScript *model.BootstrapScript
}

var _ fi.HasName = &AttachISO{}
var _ fi.HasDependencies = &AttachISO{}

func (o *AttachISO) GetDependencies(tasks map[string]fi.Task) []fi.Task {
	var deps []fi.Task
	vmCreateTask := tasks["VirtualMachine/" + *o.VM]
	if vmCreateTask == nil {
		glog.Fatalf("Unable to find create VM task %s dependency for AttachISO %s", *o.VM, *o.Name)
	}
	deps = append(deps, vmCreateTask)
	return deps
}

// GetName returns the Name of the object, implementing fi.HasName
func (o *AttachISO) GetName() *string {
	return o.Name
}

// SetName sets the Name of the object, implementing fi.SetName
func (o *AttachISO) SetName(name string) {
	o.Name = &name
}

func (e *AttachISO) Run(c *fi.Context) error {
	glog.Info("AttachISO.Run invoked!")
	return fi.DefaultDeltaRunMethod(e, c)
}

func (e *AttachISO) Find(c *fi.Context) (*AttachISO, error) {
	glog.Info("AttachISO.Find invoked!")
	return nil, nil
}

func (_ *AttachISO) CheckChanges(a, e, changes *AttachISO) error {
	glog.Info("AttachISO.CheckChanges invoked!")
	return nil
}

func (_ *AttachISO) RenderVC(t *vsphere.VSphereAPITarget, a, e, changes *AttachISO) error {
	startupScript, err := changes.BootstrapScript.ResourceNodeUp(changes.IG)
	startupStr, err := startupScript.AsString()
	if err != nil {
		return fmt.Errorf("error rendering startup script: %v", err)
	}
	dir, err := ioutil.TempDir("", *changes.VM)
	defer os.RemoveAll(dir)

	isoFile, err := createISO(changes, startupStr, dir)
	if err != nil {
		return fmt.Errorf("error creating cloud-init file: %v", err)
	}
	err = t.Cloud.UploadAndAttachISO(changes.VM, isoFile)
	if err != nil {
		return err
	}

	return nil
}

type WriteFiles struct {
	Content string `json:"content"`
	Owner string `json:"owner"`
	Permissions string `json:"permissions"`
	Path string `json:"path"`
}

type VSphereCloudInit struct {
	WriteFiles WriteFiles `json:"write_files"`
	Runcmd string `json:"runcmd"`
}

func createISO(changes *AttachISO, startupStr string, dir string) (string, error) {
	cloudInit := VSphereCloudInit{
		WriteFiles: WriteFiles{
			Content: startupStr,
			Owner: "root:root",
			Permissions: "0644",
			Path: "/tmp/script.sh",
		},
		Runcmd:"bash /tmp/script.sh > /var/log/script.log",
	}
	data, err := kops.ToRawYaml(cloudInit)
	if err != nil {
		return "", err
	}

	glog.V(4).Infof("Cloud-init file content: %s", string(data))


	if err != nil {
		glog.Fatalf("Could not create a temp directory for VM %s", *changes.VM)
	}
	userDataFile := filepath.Join(dir, "user-data")
	if err := ioutil.WriteFile(userDataFile, data, 0644); err != nil {
		glog.Fatalf("Unable to write user-data into file %s", userDataFile)
	}

	isoFile := filepath.Join(dir, *changes.VM + ".iso")
	cmd := exec.Command("genisoimage", "-o", isoFile, "-volid", "cidata", "-joliet", "-rock", dir)
	var out bytes.Buffer
	cmd.Stdout = &out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		glog.Fatalf("Error %s occurred while executing command %+v", err, cmd)
	}
	glog.V(4).Infof("genisoimage std output : %s\n", out.String())
	glog.V(4).Infof("genisoimage std error : %s\n", stderr.String())
	return isoFile, nil

}
