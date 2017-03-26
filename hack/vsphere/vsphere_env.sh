# If set, coredns will be used for vsphere cloud provider.
export VSPHERE_DNS=coredns

# If set, this dns controller image will be used.
# Leave this value unmodified if you are not building a new dns-controller image.
export VSPHERE_DNSCONTROLLER_IMAGE=luomiao/dns-controller

# S3 bucket that kops should use.
export KOPS_STATE_STORE=s3://your-obj-store

# AWS credentials
export AWS_REGION=us-west-2
export AWS_ACCESS_KEY_ID=something
export AWS_SECRET_ACCESS_KEY=something

# vSphere credentials
export VSPHERE_USERNAME=administrator@vsphere.local
export VSPHERE_PASSWORD=Admin!23

# Set TARGET and TARGET_PATH to values where you want nodeup and protokube binaries to get copied. 
# This should be same location as set for NODEUP_URL and PROTOKUBE_IMAGE. 
export TARGET=jdoe@pa-dbc1131.eng.vmware.com
export TARGET_PATH=/dbc/pa-dbc1131/jdoe/misc/kops/

export NODEUP_URL=http://pa-dbc1131.eng.vmware.com/jdoe/misc/kops/nodeup/nodeup
export PROTOKUBE_IMAGE=http://pa-dbc1131.eng.vmware.com/jdoe/misc/kops/protokube/protokube.tar.gz

echo "VSPHERE_DNS=${VSPHERE_DNS}"
echo "VSPHERE_DNSCONTROLLER_IMAGE=${VSPHERE_DNSCONTROLLER_IMAGE}"
echo "KOPS_STATE_STORE=${KOPS_STATE_STORE}"
echo "AWS_REGION=${AWS_REGION}"
echo "AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}"
echo "AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}"
echo "VSPHERE_USERNAME=${VSPHERE_USERNAME}"
echo "VSPHERE_PASSWORD=${VSPHERE_PASSWORD}"
echo "NODEUP_URL=${NODEUP_URL}"
echo "PROTOKUBE_IMAGE=${PROTOKUBE_IMAGE}"
echo "TARGET=${TARGET}"
echo "TARGET_PATH=${TARGET_PATH}"
