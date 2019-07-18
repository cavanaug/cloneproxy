BINARY=cloneproxy

VERSION=4.0.1
BUILD=`date -u +%Y%m%d.%H%M%S`

LDFLAGS=-ldflags "-X main.VERSION=${VERSION} -X main.minversion=${BUILD}"

install:
	go get -d ./...

release:
	env GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o build/${BINARY} ${BINARY}.go

clean:
	if [ -f build/${BINARY} ] ; then rm build/${BINARY} ; fi