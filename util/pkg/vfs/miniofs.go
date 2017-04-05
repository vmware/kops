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
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/golang/glog"
	"io/ioutil"
	"k8s.io/kops/util/pkg/hashing"
	"os"
	"path"
	"strings"
	"sync"
)

type MinioPath struct {
	minioContext *MinioContext
	bucket       string
	region       string
	key          string
	etag         *string
}

type MinioContext struct {
	mutex           sync.Mutex
	clients         map[string]*s3.S3
	bucketLocations map[string]string
}

func NewMinioContext() *MinioContext {
	return &MinioContext{
		clients:         make(map[string]*s3.S3),
		bucketLocations: make(map[string]string),
	}
}

var _ Path = &MinioPath{}
var _ HasHash = &MinioPath{}

func newMinioPath(minioContext *MinioContext, bucket string, key string) *MinioPath {
	bucket = strings.TrimSuffix(bucket, "/")
	key = strings.TrimPrefix(key, "/")

	return &MinioPath{
		minioContext: minioContext,
		bucket:       bucket,
		key:          key,
	}
}

func (s *MinioContext) getClient(region string) (*s3.S3, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s3Client := s.clients[region]
	if s3Client == nil {
		minioAccessKeyID := os.Getenv("MINIO_ACCESS_KEY_ID")
		if minioAccessKeyID == "" {
			return nil, fmt.Errorf("MINIO_ACCESS_KEY_ID cannot be empty when KOPS_STATE_STORE is starting with minio")
		}
		minioSecretAccessKey := os.Getenv("MINIO_SECRET_ACCESS_KEY")
		if minioSecretAccessKey == "" {
			return nil, fmt.Errorf("MINIO_SECRET_ACCESS_KEY cannot be empty when KOPS_STATE_STORE is starting with minio")
		}
		minioEndpoint := os.Getenv("MINIO_ENDPOINT")
		if minioEndpoint == "" {
			return nil, fmt.Errorf("MINIO_ENDPOINT cannot be empty when KOPS_STATE_STORE is starting with minio")
		}

		s3Config := &aws.Config{
			Credentials:      credentials.NewStaticCredentials(minioAccessKeyID, minioSecretAccessKey, ""),
			Endpoint:         aws.String("http://" + minioEndpoint + ":9000"),
			Region:           aws.String(region),
			DisableSSL:       aws.Bool(true),
			S3ForcePathStyle: aws.Bool(true),
		}
		newSession := session.New(s3Config)
		s3Client = s3.New(newSession)
	}

	s.clients[region] = s3Client

	return s3Client, nil
}

func (p *MinioPath) Path() string {
	return "minio://" + p.bucket + "/" + p.key
}

func (p *MinioPath) Bucket() string {
	return p.bucket
}

func (p *MinioPath) Key() string {
	return p.key
}

func (p *MinioPath) String() string {
	return p.Path()
}

func (p *MinioPath) Remove() error {
	client, err := p.client()
	if err != nil {
		return err
	}

	request := &s3.DeleteObjectInput{}
	request.Bucket = aws.String(p.bucket)
	request.Key = aws.String(p.key)

	_, err = client.DeleteObject(request)
	if err != nil {
		// TODO: Check for not-exists, return os.NotExist

		return fmt.Errorf("error deleting %s: %v", p, err)
	}

	return nil
}

func (p *MinioPath) Join(relativePath ...string) Path {
	args := []string{p.key}
	args = append(args, relativePath...)
	joined := path.Join(args...)
	return &MinioPath{
		minioContext: p.minioContext,
		bucket:       p.bucket,
		key:          joined,
	}
}

func (p *MinioPath) WriteFile(data []byte) error {
	client, err := p.client()
	if err != nil {
		return err
	}

	glog.V(4).Infof("Writing file %q", p)

	// We always use server-side-encryption; it doesn't really cost us anything
	sse := "AES256"

	request := &s3.PutObjectInput{}
	request.Body = bytes.NewReader(data)
	request.Bucket = aws.String(p.bucket)
	request.Key = aws.String(p.key)
	request.ServerSideEncryption = aws.String(sse)

	acl := os.Getenv("KOPS_STATE_Minio_ACL")
	acl = strings.TrimSpace(acl)
	if acl != "" {
		glog.Infof("Using KOPS_STATE_Minio_ACL=%s", acl)
		request.ACL = aws.String(acl)
	}

	// We don't need Content-MD5: https://github.com/aws/aws-sdk-go/issues/208

	glog.V(8).Infof("Calling Minio PutObject Bucket=%q Key=%q SSE=%q ACL=%q BodyLen=%d", p.bucket, p.key, sse, acl, len(data))

	_, err = client.PutObject(request)
	if err != nil {
		if acl != "" {
			return fmt.Errorf("error writing %s (with ACL=%q): %v", p, acl, err)
		} else {
			return fmt.Errorf("error writing %s: %v", p, err)
		}
	}

	return nil
}

// To prevent concurrent creates on the same file while maintaining atomicity of writes,
// we take a process-wide lock during the operation.
// Not a great approach, but fine for a single process (with low concurrency)
// TODO: should we enable versioning?
var createFileLockMinio sync.Mutex

func (p *MinioPath) CreateFile(data []byte) error {
	createFileLockMinio.Lock()
	defer createFileLockMinio.Unlock()

	// Check if exists
	_, err := p.ReadFile()
	if err == nil {
		return os.ErrExist
	}

	if !os.IsNotExist(err) {
		return err
	}

	return p.WriteFile(data)
}

func (p *MinioPath) ReadFile() ([]byte, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}

	glog.V(4).Infof("Reading file %q", p)

	request := &s3.GetObjectInput{}
	request.Bucket = aws.String(p.bucket)
	request.Key = aws.String(p.key)

	response, err := client.GetObject(request)
	if err != nil {
		if AWSErrorCode(err) == "NoSuchKey" {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("error fetching %s: %v", p, err)
	}
	defer response.Body.Close()

	d, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", p, err)
	}
	return d, nil
}

func (p *MinioPath) ReadDir() ([]Path, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}

	prefix := p.key
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	request := &s3.ListObjectsInput{}
	request.Bucket = aws.String(p.bucket)
	request.Prefix = aws.String(prefix)
	request.Delimiter = aws.String("/")

	glog.V(4).Infof("Listing objects in Minio bucket %q with prefix %q", p.bucket, prefix)
	var paths []Path
	err = client.ListObjectsPages(request, func(page *s3.ListObjectsOutput, lastPage bool) bool {
		for _, o := range page.Contents {
			key := aws.StringValue(o.Key)
			if key == prefix {
				// We have reports (#548 and #520) of the directory being returned as a file
				// And this will indeed happen if the directory has been created as a file,
				// which seems to happen if you use some external tools to manipulate the Minio bucket.
				// We need to tolerate that, so skip the parent directory.
				glog.V(4).Infof("Skipping read of directory: %q", key)
				continue
			}
			child := &MinioPath{
				minioContext: p.minioContext,
				bucket:       p.bucket,
				key:          key,
				etag:         o.ETag,
			}
			paths = append(paths, child)
		}
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("error listing %s: %v", p, err)
	}
	glog.V(8).Infof("Listed files in %v: %v", p, paths)
	return paths, nil
}

func (p *MinioPath) ReadTree() ([]Path, error) {
	client, err := p.client()
	if err != nil {
		return nil, err
	}

	request := &s3.ListObjectsInput{}
	request.Bucket = aws.String(p.bucket)
	prefix := p.key
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	request.Prefix = aws.String(prefix)
	// No delimiter for recursive search

	var paths []Path
	err = client.ListObjectsPages(request, func(page *s3.ListObjectsOutput, lastPage bool) bool {
		for _, o := range page.Contents {
			key := aws.StringValue(o.Key)
			child := &MinioPath{
				minioContext: p.minioContext,
				bucket:       p.bucket,
				key:          key,
				etag:         o.ETag,
			}
			paths = append(paths, child)
		}
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("error listing %s: %v", p, err)
	}
	return paths, nil
}

func (p *MinioPath) client() (*s3.S3, error) {
	if p.region == "" {
		p.region = os.Getenv("MINIO_REGION")
		if p.region == "" {
			p.region = "us-east-1"
		}
	}

	client, err := p.minioContext.getClient(p.region)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func (p *MinioPath) Base() string {
	return path.Base(p.key)
}

func (p *MinioPath) PreferredHash() (*hashing.Hash, error) {
	return p.Hash(hashing.HashAlgorithmMD5)
}

func (p *MinioPath) Hash(a hashing.HashAlgorithm) (*hashing.Hash, error) {
	if a != hashing.HashAlgorithmMD5 {
		return nil, nil
	}

	if p.etag == nil {
		return nil, nil
	}

	md5 := strings.Trim(*p.etag, "\"")

	md5Bytes, err := hex.DecodeString(md5)
	if err != nil {
		return nil, fmt.Errorf("Etag was not a valid MD5 sum: %q", *p.etag)
	}

	return &hashing.Hash{Algorithm: hashing.HashAlgorithmMD5, HashValue: md5Bytes}, nil
}
