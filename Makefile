# Copyright 2016 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

all: kops

.PHONY: channels examples

DOCKER_REGISTRY?=gcr.io/must-override
S3_BUCKET?=s3://must-override/
GCS_LOCATION?=gs://must-override
GCS_URL=$(GCS_LOCATION:gs://%=https://storage.googleapis.com/%)
LATEST_FILE?=latest-ci.txt
GOPATH_1ST=$(shell echo ${GOPATH} | cut -d : -f 1)
UNIQUE:=$(shell date +%s)
GOVERSION=1.7.4

# See http://stackoverflow.com/questions/18136918/how-to-get-current-relative-directory-of-your-makefile
MAKEDIR:=$(strip $(shell dirname "$(realpath $(lastword $(MAKEFILE_LIST)))"))

# Keep in sync with upup/models/cloudup/resources/addons/dns-controller/
DNS_CONTROLLER_TAG=1.5.2

KOPS_RELEASE_VERSION=1.5.2-beta.2
KOPS_CI_VERSION=1.6.0-alpha.0

GITSHA := $(shell cd ${GOPATH_1ST}/src/k8s.io/kops; git describe --always)

ifndef VERSION
  # To keep both CI and end-users building from source happy,
  # we expect that CI sets CI=1.
  #
  # For end users, they need only build kops, and they can use the last
  # released version of nodeup/protokube.
  # For CI, we continue to build a synthetic version from the git SHA, so
  # we never cross versions.
  #
  # We expect that if you are uploading nodeup/protokube, you will set
  # VERSION (along with S3_BUCKET), either directly or by setting CI=1
  ifndef CI
    VERSION=${KOPS_RELEASE_VERSION}
  else
    VERSION := ${KOPS_CI_VERSION}+${GITSHA}
  endif
endif

# + is valid in semver, but not in docker tags. Fixup CI versions.
# Note that this mirrors the logic in DefaultProtokubeImageName
PROTOKUBE_TAG := $(subst +,-,${VERSION})

# Go exports:

GO15VENDOREXPERIMENT=1
export GO15VENDOREXPERIMENT

ifdef STATIC_BUILD
  CGO_ENABLED=0
  export CGO_ENABLED
  EXTRA_BUILDFLAGS=-installsuffix cgo
  EXTRA_LDFLAGS=-s
endif

SHASUMCMD := $(shell sha1sum --help 2> /dev/null)

ifndef SHASUMCMD
  $(error "sha1sum command is not available (on MacOS try 'brew install md5sha1sum')")
endif

kops: kops-gobindata
	go install ${EXTRA_BUILDFLAGS} -ldflags "-X k8s.io/kops.Version=${VERSION} -X k8s.io/kops.GitVersion=${GITSHA} ${EXTRA_LDFLAGS}" k8s.io/kops/cmd/kops/...

gobindata-tool:
	go build ${EXTRA_BUILDFLAGS} -ldflags "${EXTRA_LDFLAGS}" -o ${GOPATH_1ST}/bin/go-bindata k8s.io/kops/vendor/github.com/jteeuwen/go-bindata/go-bindata

kops-gobindata: gobindata-tool
	cd ${GOPATH_1ST}/src/k8s.io/kops; ${GOPATH_1ST}/bin/go-bindata -o upup/models/bindata.go -pkg models -ignore="\\.DS_Store" -ignore="bindata\\.go" -ignore="vfs\\.go" -prefix upup/models/ upup/models/...
	cd ${GOPATH_1ST}/src/k8s.io/kops; ${GOPATH_1ST}/bin/go-bindata -o federation/model/bindata.go -pkg model -ignore="\\.DS_Store" -ignore="bindata\\.go" -prefix federation/model/ federation/model/...

# Build in a docker container with golang 1.X
# Used to test we have not broken 1.X
check-builds-in-go15:
	docker run -v ${GOPATH_1ST}/src/k8s.io/kops:/go/src/k8s.io/kops golang:1.5 make -f /go/src/k8s.io/kops/Makefile kops

check-builds-in-go16:
	docker run -v ${GOPATH_1ST}/src/k8s.io/kops:/go/src/k8s.io/kops golang:1.6 make -f /go/src/k8s.io/kops/Makefile kops

check-builds-in-go17:
	docker run -v ${GOPATH_1ST}/src/k8s.io/kops:/go/src/k8s.io/kops golang:1.7 make -f /go/src/k8s.io/kops/Makefile kops

codegen: kops-gobindata
	go install k8s.io/kops/upup/tools/generators/...
	PATH=${GOPATH_1ST}/bin:${PATH} go generate k8s.io/kops/upup/pkg/fi/cloudup/awstasks
	PATH=${GOPATH_1ST}/bin:${PATH} go generate k8s.io/kops/upup/pkg/fi/cloudup/gcetasks
	PATH=${GOPATH_1ST}/bin:${PATH} go generate k8s.io/kops/upup/pkg/fi/fitasks

test:
	go test k8s.io/kops/pkg/... -args -v=1 -logtostderr
	go test k8s.io/kops/upup/pkg/... -args -v=1 -logtostderr
	go test k8s.io/kops/protokube/... -args -v=1 -logtostderr
	go test k8s.io/kops/dns-controller/pkg/... -args -v=1 -logtostderr
	go test k8s.io/kops/cmd/... -args -v=1 -logtostderr
	go test k8s.io/kops/tests/... -args -v=1 -logtostderr
	go test k8s.io/kops/util/... -args -v=1 -logtostderr

crossbuild-nodeup:
	mkdir -p .build/dist/
	GOOS=linux GOARCH=amd64 go build -a ${EXTRA_BUILDFLAGS} -o .build/dist/linux/amd64/nodeup -ldflags "${EXTRA_LDFLAGS} -X k8s.io/kops.Version=${VERSION} -X k8s.io/kops.GitVersion=${GITSHA}" k8s.io/kops/cmd/nodeup

crossbuild-nodeup-in-docker:
	docker pull golang:${GOVERSION} # Keep golang image up to date
	docker run --name=nodeup-build-${UNIQUE} -e STATIC_BUILD=yes -e VERSION=${VERSION} -v ${MAKEDIR}:/go/src/k8s.io/kops golang:${GOVERSION} make -f /go/src/k8s.io/kops/Makefile crossbuild-nodeup
	docker cp nodeup-build-${UNIQUE}:/go/.build .

crossbuild:
	mkdir -p .build/dist/
	GOOS=darwin GOARCH=amd64 go build -a ${EXTRA_BUILDFLAGS} -o .build/dist/darwin/amd64/kops -ldflags "${EXTRA_LDFLAGS} -X k8s.io/kops.Version=${VERSION} -X k8s.io/kops.GitVersion=${GITSHA}" k8s.io/kops/cmd/kops
	GOOS=linux GOARCH=amd64 go build -a ${EXTRA_BUILDFLAGS} -o .build/dist/linux/amd64/kops -ldflags "${EXTRA_LDFLAGS} -X k8s.io/kops.Version=${VERSION} -X k8s.io/kops.GitVersion=${GITSHA}" k8s.io/kops/cmd/kops

crossbuild-in-docker:
	docker pull golang:${GOVERSION} # Keep golang image up to date
	docker run --name=kops-build-${UNIQUE} -e STATIC_BUILD=yes -e VERSION=${VERSION} -v ${MAKEDIR}:/go/src/k8s.io/kops golang:${GOVERSION} make -f /go/src/k8s.io/kops/Makefile crossbuild
	docker cp kops-build-${UNIQUE}:/go/.build .

kops-dist: crossbuild-in-docker
	mkdir -p .build/dist/
	(sha1sum .build/dist/darwin/amd64/kops | cut -d' ' -f1) > .build/dist/darwin/amd64/kops.sha1
	(sha1sum .build/dist/linux/amd64/kops | cut -d' ' -f1) > .build/dist/linux/amd64/kops.sha1

version-dist: nodeup-dist kops-dist protokube-export utils-dist
	rm -rf .build/upload
	mkdir -p .build/upload/kops/${VERSION}/linux/amd64/
	mkdir -p .build/upload/kops/${VERSION}/darwin/amd64/
	mkdir -p .build/upload/kops/${VERSION}/images/
	mkdir -p .build/upload/utils/${VERSION}/linux/amd64/
	cp .build/dist/nodeup .build/upload/kops/${VERSION}/linux/amd64/nodeup
	cp .build/dist/nodeup.sha1 .build/upload/kops/${VERSION}/linux/amd64/nodeup.sha1
	cp .build/dist/images/protokube.tar.gz .build/upload/kops/${VERSION}/images/protokube.tar.gz
	cp .build/dist/images/protokube.tar.gz.sha1 .build/upload/kops/${VERSION}/images/protokube.tar.gz.sha1
	cp .build/dist/linux/amd64/kops .build/upload/kops/${VERSION}/linux/amd64/kops
	cp .build/dist/linux/amd64/kops.sha1 .build/upload/kops/${VERSION}/linux/amd64/kops.sha1
	cp .build/dist/darwin/amd64/kops .build/upload/kops/${VERSION}/darwin/amd64/kops
	cp .build/dist/darwin/amd64/kops.sha1 .build/upload/kops/${VERSION}/darwin/amd64/kops.sha1
	cp .build/dist/linux/amd64/utils.tar.gz .build/upload/kops/${VERSION}/linux/amd64/utils.tar.gz
	cp .build/dist/linux/amd64/utils.tar.gz.sha1 .build/upload/kops/${VERSION}/linux/amd64/utils.tar.gz.sha1

upload: kops version-dist
	aws s3 sync --acl public-read .build/upload/ ${S3_BUCKET}

gcs-upload: version-dist
	@echo "== Uploading kops =="
	gsutil -h "Cache-Control:private, max-age=0, no-transform" -m cp -n -r .build/upload/kops/* ${GCS_LOCATION}

# In CI testing, always upload the CI version.
gcs-publish-ci: VERSION := ${KOPS_CI_VERSION}+${GITSHA}
gcs-publish-ci: PROTOKUBE_TAG := $(subst +,-,${VERSION})
gcs-publish-ci: gcs-upload
	echo "VERSION: ${VERSION}"
	echo "PROTOKUBE_TAG: ${PROTOKUBE_TAG}"
	echo "${GCS_URL}/${VERSION}" > .build/upload/${LATEST_FILE}
	gsutil -h "Cache-Control:private, max-age=0, no-transform" cp .build/upload/${LATEST_FILE} ${GCS_LOCATION}

gen-cli-docs:
	KOPS_STATE_STORE= kops genhelpdocs --out docs/cli

# Will always push a linux-based build up to the server
push: crossbuild-nodeup
	scp -C .build/dist/linux/amd64/nodeup  ${TARGET}:/tmp/

push-gce-dry: push
	ssh ${TARGET} sudo SKIP_PACKAGE_UPDATE=1 /tmp/nodeup --conf=metadata://gce/config --dryrun --v=8

push-aws-dry: push
	ssh ${TARGET} sudo SKIP_PACKAGE_UPDATE=1 /tmp/nodeup --conf=/var/cache/kubernetes-install/kube_env.yaml --dryrun --v=8

push-gce-run: push
	ssh ${TARGET} sudo SKIP_PACKAGE_UPDATE=1 /tmp/nodeup --conf=metadata://gce/config --v=8

# -t is for CentOS http://unix.stackexchange.com/questions/122616/why-do-i-need-a-tty-to-run-sudo-if-i-can-sudo-without-a-password
push-aws-run: push
	ssh -t ${TARGET} sudo SKIP_PACKAGE_UPDATE=1 /tmp/nodeup --conf=/var/cache/kubernetes-install/kube_env.yaml --v=8

# Please read docs/development/vsphere-dev.md before trying this out.
# Build protokube and nodeup. Pushes protokube to DOCKER_REGISTRY and scp nodeup binary to the TARGET. Uncomment ssh part to run nodeup at set TARGET.
push-vsphere: nodeup protokube-push
	scp -C .build/dist/nodeup  ${TARGET}:~/
	# ssh -t root@10.161.56.108 'export AWS_ACCESS_KEY_ID="AKIAJ6UOKB6BGYLIQJEQ"; export AWS_SECRET_ACCESS_KEY="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"; export AWS_REGION="us-west-2"; SKIP_PACKAGE_UPDATE=1 ~/nodeup --conf=/var/cache/kubernetes-install/kube_env.yaml --v=8'

protokube-gocode:
	go install k8s.io/kops/protokube/cmd/protokube

protokube-builder-image:
	docker build -t protokube-builder images/protokube-builder

protokube-build-in-docker: protokube-builder-image
	docker run -t -e VERSION=${VERSION} -v `pwd`:/src protokube-builder /onbuild.sh

protokube-image: protokube-build-in-docker
	docker build -t protokube:${PROTOKUBE_TAG} -f images/protokube/Dockerfile .

protokube-export: protokube-image
	mkdir -p .build/dist/images
	docker save protokube:${PROTOKUBE_TAG} > .build/dist/images/protokube.tar
	gzip --force --best .build/dist/images/protokube.tar
	(sha1sum .build/dist/images/protokube.tar.gz | cut -d' ' -f1) > .build/dist/images/protokube.tar.gz.sha1

# protokube-push is no longer used (we upload a docker image tar file to S3 instead),
# but we're keeping it around in case it is useful for development etc
protokube-push: protokube-image
	docker tag protokube:${PROTOKUBE_TAG} ${DOCKER_REGISTRY}/protokube:${PROTOKUBE_TAG}
	docker push ${DOCKER_REGISTRY}/protokube:${PROTOKUBE_TAG}

nodeup: nodeup-dist

nodeup-gocode: kops-gobindata
	go install ${EXTRA_BUILDFLAGS} -ldflags "${EXTRA_LDFLAGS} -X k8s.io/kops.Version=${VERSION} -X k8s.io/kops.GitVersion=${GITSHA}" k8s.io/kops/cmd/nodeup

nodeup-dist:
	docker pull golang:${GOVERSION} # Keep golang image up to date
	docker run --name=nodeup-build-${UNIQUE} -e STATIC_BUILD=yes -e VERSION=${VERSION} -v ${MAKEDIR}:/go/src/k8s.io/kops golang:${GOVERSION} make -f /go/src/k8s.io/kops/Makefile nodeup-gocode
	mkdir -p .build/dist
	docker cp nodeup-build-${UNIQUE}:/go/bin/nodeup .build/dist/
	(sha1sum .build/dist/nodeup | cut -d' ' -f1) > .build/dist/nodeup.sha1

dns-controller-gocode:
	go install -ldflags "${EXTRA_LDFLAGS} -X main.BuildVersion=${DNS_CONTROLLER_TAG}" k8s.io/kops/dns-controller/cmd/dns-controller

dns-controller-builder-image:
	docker build -t dns-controller-builder images/dns-controller-builder

dns-controller-build-in-docker: dns-controller-builder-image
	docker run -t -v `pwd`:/src dns-controller-builder /onbuild.sh

dns-controller-image: dns-controller-build-in-docker
	docker build -t ${DOCKER_REGISTRY}/dns-controller:${DNS_CONTROLLER_TAG}  -f images/dns-controller/Dockerfile .

dns-controller-push: dns-controller-image
	docker push ${DOCKER_REGISTRY}/dns-controller:${DNS_CONTROLLER_TAG}

# --------------------------------------------------
# static utils

utils-dist:
	docker build -t utils-builder images/utils-builder
	mkdir -p .build/dist/linux/amd64/
	docker run -v `pwd`/.build/dist/linux/amd64/:/dist utils-builder /extract.sh

# --------------------------------------------------
# development targets

# See docs/development/dependencies.md
copydeps:
	rsync -avz _vendor/ vendor/ --delete --exclude vendor/  --exclude .git

gofmt:
	gofmt -w -s channels/
	gofmt -w -s cloudmock/
	gofmt -w -s cmd/
	gofmt -w -s examples/
	gofmt -w -s federation/
	gofmt -w -s nodeup/
	gofmt -w -s util/
	gofmt -w -s upup/pkg/
	gofmt -w -s pkg/
	gofmt -w -s tests/
	gofmt -w -s protokube/cmd
	gofmt -w -s protokube/pkg
	gofmt -w -s protokube/tests
	gofmt -w -s dns-controller/cmd
	gofmt -w -s dns-controller/pkg


govet:
	go vet \
	  k8s.io/kops/cmd/... \
	  k8s.io/kops/pkg/... \
	  k8s.io/kops/channels/... \
	  k8s.io/kops/examples/... \
	  k8s.io/kops/federation/... \
	  k8s.io/kops/nodeup/... \
	  k8s.io/kops/util/... \
	  k8s.io/kops/upup/... \
	  k8s.io/kops/protokube/... \
	  k8s.io/kops/dns-controller/...


# --------------------------------------------------
# Continuous integration targets

verify-boilerplate:
	sh -c hack/verify-boilerplate.sh

.PHONY: verify-gofmt
verify-gofmt:
	hack/verify-gofmt.sh

.PHONY: verify-packages
verify-packages:
	hack/verify-packages.sh

ci: govet verify-gofmt kops nodeup-gocode examples test verify-boilerplate verify-packages
	echo "Done!"

# --------------------------------------------------
# channel tool

channels: channels-gocode

channels-gocode:
	go install ${EXTRA_BUILDFLAGS} -ldflags "-X k8s.io/kops.Version=${VERSION} ${EXTRA_LDFLAGS}" k8s.io/kops/channels/cmd/channels

# --------------------------------------------------
# API / embedding examples

examples:
	go install k8s.io/kops/examples/kops-api-example/...

# -----------------------------------------------------
# api machinery regenerate

apimachinery:
	./hack/make-apimachinery.sh
	${GOPATH}/bin/conversion-gen --skip-unsafe=true --input-dirs k8s.io/kops/pkg/apis/kops/v1alpha1 --v=0  --output-file-base=zz_generated.conversion
	${GOPATH}/bin/conversion-gen --skip-unsafe=true --input-dirs k8s.io/kops/pkg/apis/kops/v1alpha2 --v=0  --output-file-base=zz_generated.conversion
	${GOPATH}/bin/defaulter-gen --input-dirs k8s.io/kops/pkg/apis/kops/v1alpha1 --v=0  --output-file-base=zz_generated.defaults
	${GOPATH}/bin/defaulter-gen --input-dirs k8s.io/kops/pkg/apis/kops/v1alpha2 --v=0  --output-file-base=zz_generated.defaults
	#go install github.com/ugorji/go/codec/codecgen
	# codecgen works only if invoked from directory where the file is located.
	#cd pkg/apis/kops/v1alpha2/ && ~/k8s/bin/codecgen -d 1234 -o types.generated.go instancegroup.go cluster.go federation.go
	#cd pkg/apis/kops/v1alpha1/ && ~/k8s/bin/codecgen -d 1234 -o types.generated.go instancegroup.go cluster.go federation.go
	#cd pkg/apis/kops/ && ~/k8s/bin/codecgen -d 1234 -o types.generated.go instancegroup.go cluster.go federation.go
