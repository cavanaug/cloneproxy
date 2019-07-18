package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"reflect"
	"testing"

	"github.com/hjson/hjson-go"
)

var (
	configFilename = "configTest.hjson"
)

func populateConfig() {
	raw, err := ioutil.ReadFile(configFilename)
	if err != nil {
		fmt.Printf("Error, missing %s file", configFilename)
		os.Exit(1)
	}

	hjson.Unmarshal(raw, &configData)
	configJSON, _ := json.Marshal(configData)
	json.Unmarshal(configJSON, &config)
}

func TestRewrite(t *testing.T) {
	fmt.Println("========TESTING REWRITE========")
	populateConfig()

	// test regex
	fmt.Println("Testing regex...")

	path := "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	configRewriteRules := []string{"wrong_queue_name", "right_queue_name"}
	fmt.Printf("\tTesting rewrite Rules: '%s, %s'... ", configRewriteRules[0], configRewriteRules[1])

	cloneURL, err := rewrite(path)
	if err != nil {
		t.Errorf("%s", err)
	} else if cloneURL.Path != "/project/5AF308SDF093JF02/queues/right_queue_name" {
		t.Error("expected", "/project/5AF308SDF093JF02/queues/wrong_queue_name", "got", cloneURL.Path)
	} else {
		fmt.Println("passed")
	}

	configRewriteRules = []string{"wrong_queue_name", ""}
	config.Paths[path]["rewriteRules"] = []interface{}{"wrong_queue_name", ""}
	fmt.Printf("\tTesting rewrite Rules: '%s, %s'... ", configRewriteRules[0], configRewriteRules[1])
	cloneURL, err = rewrite(path)
	if err != nil {
		t.Errorf("%s", err)
	} else if cloneURL.Path != "/project/5AF308SDF093JF02/queues/" {
		t.Error("expected", "/project/5AF308SDF093JF02/queues/", "got", cloneURL.Path)
	} else {
		fmt.Println("passed")
	}

	configRewriteRules = []string{"/[a-z]+_[a-z]+_[a-z]+$", "/right_queue_name"}
	config.Paths[path]["rewriteRules"] = []interface{}{"/[a-z]+_[a-z]+_[a-z]+$", "/right_queue_name"}
	fmt.Printf("\tTesting rewrite Rules: '%s, %s'... ", configRewriteRules[0], configRewriteRules[1])
	cloneURL, err = rewrite(path)
	if err != nil {
		t.Errorf("%s", err)
	} else if cloneURL.Path != "/project/5AF308SDF093JF02/queues/right_queue_name" {
		t.Error("expected", "/project/5AF308SDF093JF02/queues/right_queue_name", "got", cloneURL.Path)
	} else {
		fmt.Println("passed")
	}

	configRewriteRules = []string{"/project/[A-Z0-9]{16}/queues/wrong_queue_name", "/project/6AF308SDF093JF03/queues/right_queue_name"}
	config.Paths[path]["rewriteRules"] = []interface{}{"/project/[A-Z0-9]{16}/queues/wrong_queue_name", "/project/6AF308SDF093JF03/queues/right_queue_name"}
	fmt.Printf("\tTesting rewrite Rules: '%s, %s'... ", configRewriteRules[0], configRewriteRules[1])
	cloneURL, err = rewrite(path)
	if err != nil {
		t.Errorf("%s", err)
	} else if cloneURL.Path != "/project/6AF308SDF093JF03/queues/right_queue_name" {
		t.Error("expected", "/project/6AF308SDF093JF03/queues/right_queue_name", "got", cloneURL.Path)
	} else {
		fmt.Println("passed")
	}

	fmt.Println("\nTesting invalid regex...")
	configRewriteRules = []string{"/queues/[0-9]++", "/right_queue_name"}
	config.Paths[path]["rewriteRules"] = []interface{}{"/queues/[0-9]++", "/right_queue_name"}
	fmt.Printf("\tTesting rewrite Rules: '%s, %s'... ", configRewriteRules[0], configRewriteRules[1])
	cloneURL, err = rewrite(path)
	if cloneURL != nil {
		t.Error("expected cloneURL to be", nil, "got", cloneURL)
	} else if err == nil {
		t.Errorf("Expected to receive an error message")
	} else {
		fmt.Println("passed")
	}

	fmt.Print("\nTesting sequential rules... ")
	path = "/localhost:8080"
	configRewriteRules = []string{":\\d+$", ":8080/hi", ":\\d+/[a-z]{2}$", ":8080/bye"}
	config.Paths[path]["rewriteRules"] = []interface{}{":\\d+$", ":8080/hi", ":\\d+/[a-z]{2}$", ":8080/bye"}
	cloneURL, err = rewrite(path)
	if err != nil {
		t.Errorf("%s", err)
	} else if cloneURL.Path != "/localhost:8080/bye" {
		t.Error("expected", "/localhost:8080/bye", "got", cloneURL.Path)
	} else {
		fmt.Println("passed")
	}

	// test rule matching
	fmt.Println("\nTesting rules matching (each pattern must have a corresponding substitution)...")
	configRewriteRules = []string{"wrong_queue_name"}
	config.Paths[path]["rewriteRules"] = []interface{}{"wrong_queue_name"}
	fmt.Printf("\tTesting rewrite Rules: '%s'... ", configRewriteRules[0])
	cloneURL, err = rewrite(path)
	if cloneURL != nil {
		t.Error("expected cloneURL to be", nil, "got", cloneURL)
	} else if err == nil {
		t.Errorf("Expected to receive an error message")
	} else {
		fmt.Println("passed")
	}

	configRewriteRules = []string{}
	config.Paths[path]["rewriteRules"] = []interface{}{}
	fmt.Printf("\tTesting empty RewriteRules slice... ")
	cloneURL, err = rewrite(path)
	if cloneURL != nil {
		t.Error("expected cloneURL to be", nil, "got", cloneURL)
	} else if err == nil {
		t.Errorf("Expected to receive an error message")
	} else {
		fmt.Println("passed")
	}

	fmt.Println()
}

func TestMatchingRule(t *testing.T) {
	fmt.Println("========TESTING MATCHINGRULE========")

	populateConfig()
	path := "http://localhost:8081"

	fmt.Print("Testing inclusion rule... ")
	configMatchingRule := "localhost"
	config.Paths[path]["matchingRule"] = configMatchingRule
	makeCloneRequest, err := MatchingRule(path)
	if !makeCloneRequest {
		t.Error("expected", true, "got", false)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	fmt.Print("Testing exlcusion rule... ")
	configMatchingRule = "!localhost"
	config.Paths[path]["matchingRule"] = configMatchingRule
	makeCloneRequest, err = MatchingRule(path)
	if makeCloneRequest {
		t.Error("expected", false, "got", true)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	fmt.Print("Testing no rule... ")
	configMatchingRule = ""
	config.Paths[path]["matchingRule"] = configMatchingRule
	makeCloneRequest, err = MatchingRule(path)
	if !makeCloneRequest {
		t.Error("expected", true, "got", false)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	fmt.Print("Testing invalid rule... ")
	configMatchingRule = "localhost:[0-9]++"
	config.Paths[path]["matchingRule"] = configMatchingRule
	makeCloneRequest, err = MatchingRule(path)
	if makeCloneRequest {
		t.Error("expected", false, "got", true)
	} else if err == nil {
		t.Errorf("Expected to receive an error message")
	} else {
		fmt.Println("passed")
	}

	fmt.Println()
}

var counter = struct {
	target int
	clone  int
}{target: 0, clone: 0}

func serverA(w http.ResponseWriter, req *http.Request) {
	dump, err := httputil.DumpRequest(req, false)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintf(w, "%s", dump)
	counter.target++
	fmt.Printf("---> %s %s %s %s\n", "8080", req.Method, req.URL.String(), req.UserAgent())
}

func serverB(w http.ResponseWriter, req *http.Request) {
	dump, err := httputil.DumpRequest(req, false)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintf(w, "%s", dump)
	counter.clone++

	fmt.Printf("---> %s %s %s %s\n", "8081", req.Method, req.URL.String(), req.UserAgent())
}

func CloneProxy(listenPort string, path string) http.Handler {
	targetURL := parseURLWithDefaults(config.Paths[path]["target"].(string))
	cloneURL := parseURLWithDefaults(config.Paths[path]["clone"].(string))

	if listenPort != "" {
		config.ListenPort = listenPort
	}

	return NewCloneProxy(targetURL, config.TargetTimeout, config.TargetRewrite, config.Paths[path]["targetInsecure"].(bool), cloneURL, config.CloneTimeout, config.CloneRewrite, config.Paths[path]["cloneInsecure"].(bool))
	//return &baseHandle{}
}

func makeReq(method, url string, body io.Reader) *http.Request {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		fmt.Println(err)
	}
	return req
}

func TestCloneProxy(t *testing.T) {
	fmt.Println("========TESTING CLONEPROXY========")
	populateConfig()
	config.ClonePercent = 100.0

	serverTarget := http.NewServeMux()
	serverTarget.HandleFunc("/", serverA)

	serverClone := http.NewServeMux()
	serverClone.HandleFunc("/", serverB)

	go func() {
		http.ListenAndServe("localhost:8080", serverTarget)
	}()
	go func() {
		http.ListenAndServe("localhost:8081", serverClone)
	}()

	targetRequests := 4
	cloneRequests := 2
	configurations := []struct {
		rewriteRules []string
		matchingRule string
	}{
		{matchingRule: "/"},
		{matchingRule: "!/"},
	}

	testPath := "/test"
	for _, configuration := range configurations {
		t.Run("Testing configurations...", func(tst *testing.T) {
			config.Paths[testPath]["matchingRule"] = configuration.matchingRule

			ts := httptest.NewServer(CloneProxy("", testPath))
			defer ts.Close()

			tests := []struct {
				name string
				req  *http.Request
			}{
				{name: "Testing GET with MatchingRule " + configuration.matchingRule, req: makeReq("GET", ts.URL+testPath, nil)},
				{name: "Testing POST with MatchingRule " + configuration.matchingRule, req: makeReq("POST", ts.URL+testPath, nil)},
			}

			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					fmt.Println(test.name)

					res, err := http.DefaultClient.Do(test.req)
					if err != nil {
						t.Error(err)
					}
					defer res.Body.Close()
					if _, err := ioutil.ReadAll(res.Body); err != nil {
						// unexpected EOF, known issue with go
					}
				})
			}
		})
	}

	// make sure counts are correct
	// requests are always sent to target (we make 4 requests)
	// we should only be making 2 requests to clone
	if counter.target != targetRequests || counter.clone != cloneRequests {
		t.Errorf("expected %d requests to target and %d requests to clone got %d target and %d clone\n", targetRequests, cloneRequests, counter.target, counter.clone)
	}
	fmt.Println()
}

func TestHops(t *testing.T) {
	// tests MaxCloneHops -- the maximum number of b-side requests to serve
	// this also implicitly tests MaxTotalHops -- if this wasn't working, this test would never end
	fmt.Println("========TESTING CLONEPROXY HOPS========")
	populateConfig()
	config.ClonePercent = 100.0
	path := "/hops"

	serverTarget := http.NewServeMux()
	serverTarget.HandleFunc("/", serverA)
	go func() {
		http.ListenAndServe("localhost:8080", serverTarget)
	}()

	listener, err := net.Listen("tcp", "127.0.0.1:8888")
	if err != nil {
		t.Error(err)
	}

	cloneproxy := httptest.NewUnstartedServer(CloneProxy("", path))
	cloneproxy.Listener.Close()
	cloneproxy.Listener = listener

	cloneproxy.Start()
	defer cloneproxy.Close()

	totalCloneHops := 1
	counter.target = 0
	req := makeReq("GET", cloneproxy.URL+path, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Error(err)
	}
	defer res.Body.Close()

	// we should only be making totalCloneHops number of requests to the b-side
	if counter.target != totalCloneHops {
		t.Errorf("expected %d hops, did %d instead\n", totalCloneHops, counter.target)
	}
	fmt.Println()
}

func TestServicePing(t *testing.T) {
	fmt.Println("========TESTING /service/ping========")

	populateConfig()
	servicePing := "/service/ping"

	cloneproxy := httptest.NewServer(&baseHandle{})

	req := makeReq("GET", cloneproxy.URL+servicePing, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Error(err)
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
	}

	response := map[string]string{}
	err = json.Unmarshal(body, &response)
	if err != nil {
		t.Error(err)
	}

	expected := map[string]string{"msg": "imok"}
	if eq := reflect.DeepEqual(response, expected); !eq {
		t.Errorf("server returned %v when it should have returned %v", response, expected)
	}

	fmt.Println()
}

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}
