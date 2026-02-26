# cloneproxy

Golang reverse proxy with support to clone requests to a second (duplicate) destination for debugging etc...

- [cloneproxy](#cloneproxy)
  - [Requirements](#requirements)
  - [Quick Start](#quick-start)
  - [Running Tests](#running-tests)
  - [Configuring cloneproxy](#configuring-cloneproxy)

## Requirements

- [Go 1.8 or greater](https://golang.org/doc/install)

## Quick Start

1. Clone the repository:

   ```bash
   $ git clone git@github.com:Jeff457/cloneproxy.git

    Cloning into 'cloneproxy'...
    remote: Enumerating objects: 83, done.
    remote: Counting objects: 100% (83/83), done.
    remote: Compressing objects: 100% (58/58), done.
    remote: Total 6736 (delta 42), reused 48 (delta 24), pack-reused 6653
    Receiving objects: 100% (6736/6736), 4.64 MiB | 11.25 MiB/s, done.
    Resolving deltas: 100% (4412/4412), done.
   ```

1. Download the dependencies:

   ```bash
   # option 1
   $ make install

   # option 2
   $ go get -d ./...
   ```

1. Build the binary and start the server:

   ```bash
   # builds the binary for linux
   $ make release
   env GOOS=linux GOARCH=amd64 go build -ldflags "-X main.VERSION=4.0.2 -X main.minversion=`date -u +%Y%m%d.%H%M%S`" -o build/cloneproxy cloneproxy.go

   $ ./build/cloneproxy
   {"Version":"4.0.2","BuildDate":"20200508.233324"}
   ```

## Running Tests

Make sure you've gone through and completed the steps outlined in [Quick Start](#quick-start)

```bash
$ go test
========TESTING REWRITE========
Testing regex...
        Testing rewrite Rules: 'wrong_queue_name, right_queue_name'... passed
        Testing rewrite Rules: 'wrong_queue_name, '... passed
        Testing rewrite Rules: '/[a-z]+_[a-z]+_[a-z]+$, /right_queue_name'... passed
        Testing rewrite Rules: '/project/[A-Z0-9]{16}/queues/wrong_queue_name, /project/6AF308SDF093JF03/queues/right_queue_name'... passed

Testing invalid regex...
        Testing rewrite Rules: '/queues/[0-9]++, /right_queue_name'... passed

Testing sequential rules... passed

Testing rules matching (each pattern must have a corresponding substitution)...
        Testing rewrite Rules: 'wrong_queue_name'... passed
        Testing empty RewriteRules slice... passed

========TESTING MATCHINGRULE========
Testing inclusion rule... passed
Testing exlcusion rule... passed
Testing no rule... passed
Testing invalid rule... passed

========TESTING CLONEPROXY========
Testing GET with MatchingRule /
---> 8080 GET /test Go-http-client/1.1
---> 8081 GET /test Go-http-client/1.1
WARN[0000] CloneProxy Responses Mismatch                 a_response_code=200 a_response_length=229 a_sha1=83b5d06f0b9dfdb81ebc8e54e41c45e2724374a1 b_response_code=200 b_response_length=247 b_sha1=d45bfdda95535841926c6791404b166bf9d2007b duration=3 request_method=GET request_path=/test uuid=bc161f8f-7503-4b6b-8056-a6009d4942b4
Testing POST with MatchingRule /
---> 8080 POST /test Go-http-client/1.1
---> 8081 POST /test Go-http-client/1.1
WARN[0000] CloneProxy Responses Mismatch                 a_response_code=200 a_response_length=249 a_sha1=d7f5022165fda7dc305239d94e15985562af17be b_response_code=200 b_response_length=267 b_sha1=2b2444a7c4bd04a24eeb47f464866291d1653eaa duration=2 request_method=POST request_path=/test uuid=8576588d-d7d3-43bf-a83b-4dae7b6e5abb
Testing GET with MatchingRule !/
---> 8080 GET /test Go-http-client/1.1
INFO[0000] Proxy Clone Request Skipped                   clone_request=false duration=1 request_contentlength=0 request_method=GET request_path=/test response_code=200 response_length=229 sha1=de0b1227c7235950290726cdebc7f387790bfdf7 uuid=27c5dc71-d05a-4540-9fe6-8a290ee456c2
Testing POST with MatchingRule !/
---> 8080 POST /test Go-http-client/1.1
INFO[0000] Proxy Clone Request Skipped                   clone_request=false duration=1 request_contentlength=0 request_method=POST request_path=/test response_code=200 response_length=249 sha1=c3338cd1c62e72b6403e6859a43a68be0fc9b09d uuid=b2d79785-dd1d-4664-82c0-12eef8dc262d

========TESTING CLONEPROXY HOPS========
INFO[0000] Cloneproxied traffic counter exceeds maximum at 3  cloneproxied traffic count=3
Cloneproxied traffic exceed maximum at: 3
INFO[0000] Proxy Clone Request Skipped                   clone_request=false duration=2 request_contentlength=0 request_method=GET request_path=/hops response_code=200 response_length=0 sha1=da39a3ee5e6b4b0d3255bfef95601890afd80709 uuid=607e77d2-f9be-4a5b-8739-7d416a2273c3
INFO[0000] Proxy Clone Request Skipped                   clone_request=false duration=3 request_contentlength=0 request_method=GET request_path=/hops response_code=200 response_length=0 sha1=da39a3ee5e6b4b0d3255bfef95601890afd80709 uuid=f77f4c30-d503-4e22-828b-ebf8f3e2eb91
---> 8080 GET /hops Go-http-client/1.1
WARN[0000] CloneProxy Responses Mismatch                 a_response_code=200 a_response_length=0 a_sha1=da39a3ee5e6b4b0d3255bfef95601890afd80709 b_response_code=200 b_response_length=246 b_sha1=acbce4b9418939d5ca37e7c8b230b7213145e435 duration=9 request_method=GET request_path=/hops uuid=f1194687-632e-4026-8d16-c9d1f0255277

========TESTING /service/ping========
INFO[0000] request for '/service/ping' took 0 ms
passed

========TESTING Missing Path From Config Request========
INFO[0000] no path contains '/notInConfig' in the config file
INFO[0000] request for '/notInConfig' took 0 ms
passed

PASS
ok      _/cloneproxy      0.237s
```

## Configuring cloneproxy

CloneProxy has a number of configurable parameters, including the path to the configuration file itself, which must be hjson (human-readable JSON).

```bash
# specify the path to the hjson configuration file (the default is config.hjson)
$ ./build/cloneproxy -config-file <path-to-config>
```

You can find a description for each configurable parameter in `config.hjson`, but you will probably find the `Paths` field to be the most interesting, as it maps the requested path to the target (A-side) and clone (B-side); essentially, it tells cloneproxy how to transform the requests it receives.

