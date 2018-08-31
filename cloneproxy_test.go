package main

import (
	"testing"
	"os"
	"fmt"
	"net/http"
	"net/http/httputil"
	"log"
	"io/ioutil"
	"net/http/httptest"
	"io"
	"github.com/hjson/hjson-go"
	"encoding/json"
)

var (
	configFilename = "configTest.hjson"
	testPath = "/test"
)

func populateConfig() {
	raw, err := ioutil.ReadFile(configFilename)
	if err != nil {
		fmt.Printf("Error, missing %s file", configFilename)
		os.Exit(1)
	}

	hjson.Unmarshal(raw, &configData)
	configJson, _ := json.Marshal(configData)
	json.Unmarshal(configJson, &config)
}

func TestRewrite(t *testing.T) {
	fmt.Println("========TESTING REWRITE========")
	populateConfig()

	// test regex
	fmt.Println("Testing regex...")

	path := "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	configRewriteRules := []string{"wrong_queue_name", "right_queue_name"}
	fmt.Printf("\tTesting Rewrite Rules: '%s, %s'... ", configRewriteRules[0], configRewriteRules[1])

	cloneURL, err := Rewrite(path)
	if cloneURL.Path != "/project/5AF308SDF093JF02/queues/right_queue_name" {
		t.Error("expected", "/project/5AF308SDF093JF02/queues/wrong_queue_name", "got", cloneURL.Path)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	configRewriteRules = []string{"wrong_queue_name", "",}
	config.Paths[path]["rewriteRules"] = []interface{}{"wrong_queue_name", "",}
	fmt.Printf("\tTesting Rewrite Rules: '%s, %s'... ", configRewriteRules[0], configRewriteRules[1])
	cloneURL, err = Rewrite(path)
	if cloneURL.Path != "/project/5AF308SDF093JF02/queues/" {
		t.Error("expected", "/project/5AF308SDF093JF02/queues/", "got", cloneURL.Path)
	} else if err != nil {
		t.Errorf("%s", err)
	}  else {
		fmt.Println("passed")
	}

	configRewriteRules = []string{"/[a-z]+_[a-z]+_[a-z]+$", "/right_queue_name"}
	config.Paths[path]["rewriteRules"] = []interface{}{"/[a-z]+_[a-z]+_[a-z]+$", "/right_queue_name",}
	fmt.Printf("\tTesting Rewrite Rules: '%s, %s'... ", configRewriteRules[0], configRewriteRules[1])
	cloneURL, err = Rewrite(path)
	if cloneURL.Path != "/project/5AF308SDF093JF02/queues/right_queue_name" {
		t.Error("expected", "/project/5AF308SDF093JF02/queues/right_queue_name", "got", cloneURL.Path)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	configRewriteRules = []string{"/project/[A-Z0-9]{16}/queues/wrong_queue_name", "/project/6AF308SDF093JF03/queues/right_queue_name"}
	config.Paths[path]["rewriteRules"] = []interface{}{"/project/[A-Z0-9]{16}/queues/wrong_queue_name", "/project/6AF308SDF093JF03/queues/right_queue_name",}
	fmt.Printf("\tTesting Rewrite Rules: '%s, %s'... ", configRewriteRules[0], configRewriteRules[1])
	cloneURL, err = Rewrite(path)
	if cloneURL.Path != "/project/6AF308SDF093JF03/queues/right_queue_name" {
		t.Error("expected", "/project/6AF308SDF093JF03/queues/right_queue_name", "got", cloneURL.Path)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	fmt.Println("\nTesting invalid regex...")
	configRewriteRules = []string{"/queues/[0-9]++", "/right_queue_name"}
	config.Paths[path]["rewriteRules"] = []interface{}{"/queues/[0-9]++", "/right_queue_name",}
	fmt.Printf("\tTesting Rewrite Rules: '%s, %s'... ", configRewriteRules[0], configRewriteRules[1])
	cloneURL, err = Rewrite(path)
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
	cloneURL, err = Rewrite(path)
	if cloneURL.Path != "/localhost:8080/bye" {
		t.Error("expected", "/localhost:8080/bye", "got", cloneURL.Path)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	// test rule matching
	fmt.Println("\nTesting rules matching (each pattern must have a corresponding substitution)...")
	configRewriteRules = []string{"wrong_queue_name"}
	config.Paths[path]["rewriteRules"] = []interface{}{"wrong_queue_name",}
	fmt.Printf("\tTesting Rewrite Rules: '%s'... ", configRewriteRules[0])
	cloneURL, err = Rewrite(path)
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
	cloneURL, err = Rewrite(path)
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
	counter.target += 1

	fmt.Printf("---> %s %s %s %s\n", "8080", req.Method, req.URL.String(), req.UserAgent())
}

func serverB(w http.ResponseWriter, req *http.Request) {
	dump, err := httputil.DumpRequest(req, false)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Fprintf(w, "%s", dump)
	counter.clone += 1

	fmt.Printf("---> %s %s %s %s\n", "8081", req.Method, req.URL.String(), req.UserAgent())
}

func CloneProxy() http.Handler {
	targetURL := parseUrlWithDefaults(config.Paths[testPath]["target"].(string))
	cloneURL := parseUrlWithDefaults(config.Paths[testPath]["clone"].(string))

	return NewCloneProxy(targetURL, config.TargetTimeout, config.TargetRewrite, config.TargetInsecure, cloneURL, config.CloneTimeout, config.CloneRewrite, config.CloneInsecure)
	//return &baseHandle{}
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

	newReq := func(method, url string, body io.Reader) *http.Request {
		req, err := http.NewRequest(method, url, body)
		if err != nil {
			fmt.Println(err)
		}
		return req
	}

	targetRequests := 4
	cloneRequests := 2
	configurations := []struct{
		rewriteRules []string
		matchingRule string
	}{
		{matchingRule: "/"},
		{matchingRule: "!/"},
	}

	for _, configuration := range configurations {
		t.Run("Testing configurations..." , func(tst *testing.T) {
			config.Paths[testPath]["matchingRule"] = configuration.matchingRule

			ts := httptest.NewServer(CloneProxy())
			defer ts.Close()

			tests := []struct {
				name string
				req *http.Request
			}{
				{name: "Testing GET with MatchingRule " + configuration.matchingRule, req: newReq("GET", ts.URL + "/test", nil)},
				{name: "Testing POST with MatchingRule " + configuration.matchingRule, req: newReq("POST", ts.URL + "/test", nil)},
			}

			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					fmt.Println(test.name)

					res, err := http.DefaultClient.Do(test.req)
					defer res.Body.Close()
					if err != nil {
						t.Error(err)
					}
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

func testHops(t * testing.T) {
	fmt.Println("========TESTING CLONEPROXY HOPS========")
	populateConfig()
}

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}