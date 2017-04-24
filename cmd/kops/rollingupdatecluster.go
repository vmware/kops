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
	"io"
	"os"
	"strconv"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kops/cmd/kops/util"
	api "k8s.io/kops/pkg/apis/kops"
	"k8s.io/kops/pkg/featureflag"
	"k8s.io/kops/upup/pkg/fi/cloudup"
	"k8s.io/kops/upup/pkg/kutil"
	"k8s.io/kops/util/pkg/tables"
)

// Command Object for a Rolling Update.
type RollingUpdateOptions struct {
	Yes       bool
	Force     bool
	CloudOnly bool

	// The following two variables are when kops is validating a cluster
	// during a rolling update.

	// FailOnDrainError fail rolling-update if drain errors.
	FailOnDrainError bool

	// FailOnValidate fail the cluster rolling-update when the cluster
	// does not validate, after a validation period.
	FailOnValidate bool

	DrainInterval time.Duration

	ValidateRetries int

	MasterInterval  time.Duration
	NodeInterval    time.Duration
	BastionInterval time.Duration

	ClusterName string

	// InstanceGroups is the list of instance groups to rolling-update;
	// if not specified all instance groups will be updated
	InstanceGroups []string
}

func (o *RollingUpdateOptions) InitDefaults() {
	o.Yes = false
	o.Force = false
	o.CloudOnly = false
	o.FailOnDrainError = false
	o.FailOnValidate = true

	o.MasterInterval = 5 * time.Minute
	o.NodeInterval = 2 * time.Minute
	o.BastionInterval = 5 * time.Minute

	o.ValidateRetries = 8

	o.DrainInterval = 90 * time.Second

}

func NewCmdRollingUpdateCluster(f *util.Factory, out io.Writer) *cobra.Command {

	var options RollingUpdateOptions
	options.InitDefaults()

	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Rolling update a cluster",
		Long: `Rolling update a cluster instance groups.

This command updates a kubernetes cluster to match the cloud, and kops specifications.

To perform rolling update, you need to update the cloud resources first with "kops update cluster"

Use KOPS_FEATURE_FLAGS="+DrainAndValidateRollingUpdate" to use beta code that drains the nodes
and validates the cluser.  New flags for Drain and Validation operations will be shown when
the environment variable is set.`,
	}

	cmd.Flags().BoolVar(&options.Yes, "yes", options.Yes, "perform rolling update without confirmation")
	cmd.Flags().BoolVar(&options.Force, "force", options.Force, "Force rolling update, even if no changes")
	cmd.Flags().BoolVar(&options.CloudOnly, "cloudonly", options.CloudOnly, "Perform rolling update without confirming progress with k8s")

	cmd.Flags().DurationVar(&options.MasterInterval, "master-interval", options.MasterInterval, "Time to wait between restarting masters")
	cmd.Flags().DurationVar(&options.NodeInterval, "node-interval", options.NodeInterval, "Time to wait between restarting nodes")
	cmd.Flags().DurationVar(&options.BastionInterval, "bastion-interval", options.BastionInterval, "Time to wait between restarting bastions")
	cmd.Flags().StringSliceVar(&options.InstanceGroups, "instance-group", options.InstanceGroups, "List of instance groups to update (defaults to all if not specified)")

	if featureflag.DrainAndValidateRollingUpdate.Enabled() {
		cmd.Flags().BoolVar(&options.FailOnDrainError, "fail-on-drain-error", true, "The rolling-update will fail if draining a node fails.")
		cmd.Flags().BoolVar(&options.FailOnValidate, "fail-on-validate-error", true, "The rolling-update will fail if the cluster fails to validate.")
		cmd.Flags().IntVar(&options.ValidateRetries, "validate-retries", options.ValidateRetries, "The number of times that a node will be validated.  Between validation kops sleeps the master-interval/2 or node-interval/2 duration.")
		cmd.Flags().DurationVar(&options.DrainInterval, "drain-interval", options.DrainInterval, "The duration that a rolling-update will wait after the node is drained.")
	}

	cmd.Run = func(cmd *cobra.Command, args []string) {
		err := rootCommand.ProcessArgs(args)
		if err != nil {
			exitWithError(err)
			return
		}

		clusterName := rootCommand.ClusterName()
		if clusterName == "" {
			exitWithError(fmt.Errorf("--name is required"))
			return
		}

		options.ClusterName = clusterName

		err = RunRollingUpdateCluster(f, os.Stdout, &options)
		if err != nil {
			exitWithError(err)
			return
		}

	}

	return cmd
}

func RunRollingUpdateCluster(f *util.Factory, out io.Writer, options *RollingUpdateOptions) error {

	clientset, err := f.Clientset()
	if err != nil {
		return err
	}

	cluster, err := GetCluster(f, options.ClusterName)
	if err != nil {
		return err
	}

	// Validate the usage of this command for the cloud provider.
	err = util.ValidateUsage(util.RollingUpdate, cluster.Spec.CloudProvider)
	if err != nil {
		return err
	}

	contextName := cluster.ObjectMeta.Name
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: contextName}).ClientConfig()
	if err != nil {
		return fmt.Errorf("cannot load kubecfg settings for %q: %v", contextName, err)
	}

	if options.ValidateRetries <= 0 {
		return fmt.Errorf("validate-retries flag cannot be 0 or smaller")
	}

	var nodes []v1.Node
	var k8sClient kubernetes.Interface
	if !options.CloudOnly {
		k8sClient, err = kubernetes.NewForConfig(config)
		if err != nil {
			return fmt.Errorf("cannot build kube client for %q: %v", contextName, err)
		}

		nodeList, err := k8sClient.CoreV1().Nodes().List(metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to reach the kubernetes API.\n")
			fmt.Fprintf(os.Stderr, "Use --cloudonly to do a rolling-update without confirming progress with the k8s API\n\n")
			return fmt.Errorf("error listing nodes in cluster: %v", err)
		}

		if nodeList != nil {
			nodes = nodeList.Items
		}
	}

	list, err := clientset.InstanceGroups(cluster.ObjectMeta.Name).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	var instanceGroups []*api.InstanceGroup
	for i := range list.Items {
		instanceGroups = append(instanceGroups, &list.Items[i])
	}

	if len(options.InstanceGroups) != 0 {
		var filtered []*api.InstanceGroup

		for _, instanceGroupName := range options.InstanceGroups {
			var found *api.InstanceGroup
			for _, ig := range instanceGroups {
				if ig.ObjectMeta.Name == instanceGroupName {
					found = ig
					break
				}
			}
			if found == nil {
				return fmt.Errorf("InstanceGroup %q not found", instanceGroupName)
			}

			filtered = append(filtered, found)
		}

		instanceGroups = filtered
	}

	cloud, err := cloudup.BuildCloud(cluster)
	if err != nil {
		return err
	}

	warnUnmatched := true
	groups, err := kutil.FindCloudInstanceGroups(cloud, cluster, instanceGroups, warnUnmatched, nodes)
	if err != nil {
		return err
	}

	{
		t := &tables.Table{}
		t.AddColumn("NAME", func(r *kutil.CloudInstanceGroup) string {
			return r.InstanceGroup.ObjectMeta.Name
		})
		t.AddColumn("STATUS", func(r *kutil.CloudInstanceGroup) string {
			return r.Status
		})
		t.AddColumn("NEEDUPDATE", func(r *kutil.CloudInstanceGroup) string {
			return strconv.Itoa(len(r.NeedUpdate))
		})
		t.AddColumn("READY", func(r *kutil.CloudInstanceGroup) string {
			return strconv.Itoa(len(r.Ready))
		})
		t.AddColumn("MIN", func(r *kutil.CloudInstanceGroup) string {
			return strconv.Itoa(r.MinSize())
		})
		t.AddColumn("MAX", func(r *kutil.CloudInstanceGroup) string {
			return strconv.Itoa(r.MaxSize())
		})
		t.AddColumn("NODES", func(r *kutil.CloudInstanceGroup) string {
			var nodes []*v1.Node
			for _, i := range r.Ready {
				if i.Node != nil {
					nodes = append(nodes, i.Node)
				}
			}
			for _, i := range r.NeedUpdate {
				if i.Node != nil {
					nodes = append(nodes, i.Node)
				}
			}
			return strconv.Itoa(len(nodes))
		})
		var l []*kutil.CloudInstanceGroup
		for _, v := range groups {
			l = append(l, v)
		}

		columns := []string{"NAME", "STATUS", "NEEDUPDATE", "READY", "MIN", "MAX"}
		if !options.CloudOnly {
			columns = append(columns, "NODES")
		}
		err := t.Render(l, out, columns...)
		if err != nil {
			return err
		}
	}

	needUpdate := false
	for _, group := range groups {
		if len(group.NeedUpdate) != 0 {
			needUpdate = true
		}
	}

	if !needUpdate && !options.Force {
		fmt.Printf("\nNo rolling-update required.\n")
		return nil
	}

	if !options.Yes {
		fmt.Printf("\nMust specify --yes to rolling-update.\n")
		return nil
	}

	if featureflag.DrainAndValidateRollingUpdate.Enabled() {
		glog.V(2).Infof("New rolling update with drain and validate enabled.")
	}
	d := &kutil.RollingUpdateCluster{
		MasterInterval:   options.MasterInterval,
		NodeInterval:     options.NodeInterval,
		Force:            options.Force,
		Cloud:            cloud,
		K8sClient:        k8sClient,
		ClientConfig:     kutil.NewClientConfig(config, "kube-system"),
		FailOnDrainError: options.FailOnDrainError,
		FailOnValidate:   options.FailOnValidate,
		CloudOnly:        options.CloudOnly,
		ClusterName:      options.ClusterName,
		ValidateRetries:  options.ValidateRetries,
		DrainInterval:    options.DrainInterval,
	}
	return d.RollingUpdate(groups, list)
}
