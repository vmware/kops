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

package vfs

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/golang/glog"
)

type S3Context struct {
	mutex           sync.Mutex
	clients         map[string]*s3.S3
	bucketLocations map[string]string
}

func NewS3Context() *S3Context {
	return &S3Context{
		clients:         make(map[string]*s3.S3),
		bucketLocations: make(map[string]string),
	}
}

func (s *S3Context) getClient(region string) (*s3.S3, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s3Client := s.clients[region]
	if s3Client == nil {
		var config *aws.Config
		var err error
		endpoint := os.Getenv("S3_ENDPOINT")
		if endpoint == "" {
			config = aws.NewConfig().WithRegion(region)
			config = config.WithCredentialsChainVerboseErrors(true)
		} else {
			// Use customized S3 storage
			glog.Infof("Found S3_ENDPOINT=%q, using as non-AWS S3 backend", endpoint)
			config, err = getCustomS3Config(endpoint, region)
			if err != nil {
				return nil, err
			}
		}

		session := session.New()
		s3Client = s3.New(session, config)
		s.clients[region] = s3Client
	}

	return s3Client, nil
}

func getCustomS3Config(endpoint string, region string) (*aws.Config, error) {
	accessKeyID := os.Getenv("S3_ACCESS_KEY_ID")
	if accessKeyID == "" {
		return nil, fmt.Errorf("S3_ACCESS_KEY_ID cannot be empty when S3_ENDPOINT is not empty")
	}
	secretAccessKey := os.Getenv("S3_SECRET_ACCESS_KEY")
	if secretAccessKey == "" {
		return nil, fmt.Errorf("S3_SECRET_ACCESS_KEY cannot be empty when S3_ENDPOINT is not empty")
	}

	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(accessKeyID, secretAccessKey, ""),
		Endpoint:         aws.String(endpoint),
		Region:           aws.String(region),
		S3ForcePathStyle: aws.Bool(true),
	}

	return s3Config, nil
}

func (s *S3Context) getRegionForBucket(bucket string) (string, error) {
	region := func() string {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		return s.bucketLocations[bucket]
	}()

	if region != "" {
		return region, nil
	}

	// Probe to find correct region for bucket
	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint != "" {
		// If customized S3 storage is set, return user-defined region
		region = os.Getenv("S3_REGION")
		if region == "" {
			region = "us-east-1"
		}
		return region, nil
	}

	awsRegion := os.Getenv("AWS_REGION")
	if awsRegion == "" {
		awsRegion = "us-east-1"
	}

	if err := validateRegion(awsRegion); err != nil {
		return "", err
	}

	request := &s3.GetBucketLocationInput{
		Bucket: &bucket,
	}
	var response *s3.GetBucketLocationOutput

	s3Client, err := s.getClient(awsRegion)

	// Attempt one GetBucketLocation call the "normal" way (i.e. as the bucket owner)
	response, err = s3Client.GetBucketLocation(request)

	// and fallback to brute-forcing if it fails
	if err != nil {
		glog.V(2).Infof("unable to get bucket location from region %q; scanning all regions: %v", awsRegion, err)
		response, err = bruteforceBucketLocation(&awsRegion, request)
	}

	if err != nil {
		return "", err
	}

	if response.LocationConstraint == nil {
		// US Classic does not return a region
		region = "us-east-1"
	} else {
		region = *response.LocationConstraint
		// Another special case: "EU" can mean eu-west-1
		if region == "EU" {
			region = "eu-west-1"
		}
	}
	glog.V(2).Infof("Found bucket %q in region %q", bucket, region)

	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.bucketLocations[bucket] = region

	return region, nil
}

/*
Amazon's S3 API provides the GetBucketLocation call to determine the region in which a bucket is located.
This call can however only be used globally by the owner of the bucket, as mentioned on the documentation page.

For S3 buckets that are shared across multiple AWS accounts using bucket policies the call will only work if it is sent
to the correct region in the first place.

This method will attempt to "bruteforce" the bucket location by sending a request to every available region and picking
out the first result.

See also: https://docs.aws.amazon.com/goto/WebAPI/s3-2006-03-01/GetBucketLocationRequest
*/
func bruteforceBucketLocation(region *string, request *s3.GetBucketLocationInput) (*s3.GetBucketLocationOutput, error) {
	session, _ := session.NewSession(&aws.Config{Region: region})

	regions, err := ec2.New(session).DescribeRegions(nil)
	if err != nil {
		return nil, fmt.Errorf("Unable to list AWS regions: %v", err)
	}

	glog.V(2).Infof("Querying S3 for bucket location for %s", *request.Bucket)

	out := make(chan *s3.GetBucketLocationOutput, len(regions.Regions))
	for _, region := range regions.Regions {
		go func(regionName string) {
			glog.V(8).Infof("Doing GetBucketLocation in %q", regionName)
			s3Client := s3.New(session, &aws.Config{Region: aws.String(regionName)})
			result, bucketError := s3Client.GetBucketLocation(request)
			if bucketError == nil {
				glog.V(8).Infof("GetBucketLocation succeeded in %q", regionName)
				out <- result
			}
		}(*region.RegionName)
	}

	select {
	case bucketLocation := <-out:
		return bucketLocation, nil
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("Could not retrieve location for AWS bucket %s", *request.Bucket)
	}
}

func validateRegion(region string) error {
	resolver := endpoints.DefaultResolver()
	partitions := resolver.(endpoints.EnumPartitions).Partitions()
	for _, p := range partitions {
		for _, r := range p.Regions() {
			if r.ID() == region {
				return nil
			}
		}
	}
	return fmt.Errorf("%s is not a valid region\nPlease check that your region is formatted correctly (i.e. us-east-1)", region)
}
