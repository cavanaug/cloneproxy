BINARY=cloneproxy
OSXBINARY=cloneproxy_osx
PROJECT=cloneproxy

VERSION=4.0.2
BUILD=`date -u +%Y%m%d.%H%M%S`

LDFLAGS=-ldflags "-X main.VERSION=${VERSION} -X main.minversion=${BUILD}"

# note!! for GOLANG version 1.16 and beyond
# export GO111MODULE=off

install:
	env GO111MODULE=off go get -d ./...

release:
	env GO111MODULE=off GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o build/${BINARY} ${PROJECT}.go

macrelease:
	env GO111MODULE=off GOOS=darwin GOARCH=amd64 go build ${LDFLAGS} -o build/${OSXBINARY} ${PROJECT}.go

clean:
	if [ -f build/${BINARY} ] ; then rm build/${BINARY} ; fi