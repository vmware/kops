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

package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"io"
	"k8s.io/kops/cmd/kops/util"
	"k8s.io/kops/upup/pkg/fi/cloudup"
	"k8s.io/kops/upup/pkg/kutil"
	"k8s.io/kops/util/pkg/ui"
	"os"
)

type DeleteInstanceGroupOptions struct {
	Yes         bool
	ClusterName string
	GroupName   string
}

func NewCmdDeleteInstanceGroup(f *util.Factory, out io.Writer) *cobra.Command {
	options := &DeleteInstanceGroupOptions{}

	cmd := &cobra.Command{
		Use:     "instancegroup",
		Aliases: []string{"instancegroups", "ig"},
		Short:   "Delete instancegroup",
		Long:    `Delete an instancegroup configuration.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				exitWithError(fmt.Errorf("Specify name of instance group to delete"))
			}
			if len(args) != 1 {
				exitWithError(fmt.Errorf("Can only edit one instance group at a time!"))
			}

			groupName := args[0]
			options.GroupName = groupName

			options.ClusterName = rootCommand.ClusterName()

			if !options.Yes {
				message := fmt.Sprintf("Do you really want to delete instance group %q? This action cannot be undone.", groupName)

				c := &ui.ConfirmArgs{
					Out:     out,
					Message: message,
					Default: "no",
					Retries: 2,
				}

				confirmed, err := ui.GetConfirm(c)
				if err != nil {
					exitWithError(err)
				}
				if !confirmed {
					os.Exit(1)
				} else {
					options.Yes = true
				}
			}

			err := RunDeleteInstanceGroup(f, out, options)
			if err != nil {
				exitWithError(err)
			}
		},
	}

	cmd.Flags().BoolVarP(&options.Yes, "yes", "y", options.Yes, "Specify --yes to immediately delete the instance group")

	return cmd
}

func RunDeleteInstanceGroup(f *util.Factory, out io.Writer, options *DeleteInstanceGroupOptions) error {
	groupName := options.GroupName
	if groupName == "" {
		return fmt.Errorf("GroupName is required")
	}

	clusterName := options.ClusterName
	if clusterName == "" {
		return fmt.Errorf("ClusterName is required")
	}

	if !options.Yes {
		// Just for sanity / safety
		return fmt.Errorf("Yes must be specified")
	}

	cluster, err := GetCluster(f, clusterName)
	if err != nil {
		return err
	}

	// Validate the usage of this command for the cloud provider.
	err = util.ValidateUsage(util.DeleteInstanceGroup, cluster.Spec.CloudProvider)
	if err != nil {
		return err
	}

	clientset, err := f.Clientset()
	if err != nil {
		return err
	}

	group, err := clientset.InstanceGroups(cluster.ObjectMeta.Name).Get(groupName)
	if err != nil {
		return fmt.Errorf("error reading InstanceGroup %q: %v", groupName, err)
	}
	if group == nil {
		return fmt.Errorf("InstanceGroup %q not found", groupName)
	}

	cloud, err := cloudup.BuildCloud(cluster)
	if err != nil {
		return err
	}

	d := &kutil.DeleteInstanceGroup{}
	d.Cluster = cluster
	d.Cloud = cloud
	d.Clientset = clientset

	err = d.DeleteInstanceGroup(group)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "InstanceGroup %q deleted\n", group.ObjectMeta.Name)

	return nil
}
