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
)

func TestRewrite(t *testing.T) {
	fmt.Println("========TESTING REWRITE========")
	config.Rewrite = true

	// test regex
	fmt.Println("Testing regex...")
	config.TargetUrl = "/project/6ADF0FLKJ2F2LKJSF01/queues/some_queue"
	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"wrong_queue_name", "right_queue_name"}
	fmt.Printf("\tTesting Rewrite Rules: '%s, %s'... ", config.RewriteRules[0], config.RewriteRules[1])

	cloneURL, err := Rewrite(config.CloneUrl)
	if cloneURL.Path != "/project/5AF308SDF093JF02/queues/right_queue_name" {
		t.Error("expected", "/project/5AF308SDF093JF02/queues/wrong_queue_name", "got", cloneURL.Path)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"wrong_queue_name", ""}
	fmt.Printf("\tTesting Rewrite Rules: '%s, %s'... ", config.RewriteRules[0], config.RewriteRules[1])
	cloneURL, err = Rewrite(config.CloneUrl)
	if cloneURL.Path != "/project/5AF308SDF093JF02/queues/" {
		t.Error("expected", "/project/5AF308SDF093JF02/queues/", "got", cloneURL.Path)
	} else if err != nil {
		t.Errorf("%s", err)
	}  else {
		fmt.Println("passed")
	}

	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"/[a-z]+_[a-z]+_[a-z]+$", "/right_queue_name"}
	fmt.Printf("\tTesting Rewrite Rules: '%s, %s'... ", config.RewriteRules[0], config.RewriteRules[1])
	cloneURL, err = Rewrite(config.CloneUrl)
	if cloneURL.Path != "/project/5AF308SDF093JF02/queues/right_queue_name" {
		t.Error("expected", "/project/5AF308SDF093JF02/queues/right_queue_name", "got", cloneURL.Path)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"/project/[A-Z0-9]{16}/queues/wrong_queue_name", "/project/6AF308SDF093JF03/queues/right_queue_name"}
	fmt.Printf("\tTesting Rewrite Rules: '%s, %s'... ", config.RewriteRules[0], config.RewriteRules[1])
	cloneURL, err = Rewrite(config.CloneUrl)
	if cloneURL.Path != "/project/6AF308SDF093JF03/queues/right_queue_name" {
		t.Error("expected", "/project/6AF308SDF093JF03/queues/right_queue_name", "got", cloneURL.Path)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	fmt.Println("\nTesting invalid regex...")
	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"/queues/[0-9]++", "/right_queue_name"}
	fmt.Printf("\tTesting Rewrite Rules: '%s, %s'... ", config.RewriteRules[0], config.RewriteRules[1])
	cloneURL, err = Rewrite(config.CloneUrl)
	if cloneURL != nil {
		t.Error("expected cloneURL to be", nil, "got", cloneURL)
	} else if err == nil {
		t.Errorf("Expected to receive an error message")
	} else {
		fmt.Println("passed")
	}

	fmt.Print("\nTesting sequential rules... ")
	config.CloneUrl = "/localhost:8080"
	config.RewriteRules = []string{":\\d+$", ":8080/hi", ":\\d+/[a-z]{2}$", ":8080/bye"}
	cloneURL, err = Rewrite(config.CloneUrl)
	if cloneURL.Path != "/localhost:8080/bye" {
		t.Error("expected", "/localhost:8080/bye", "got", cloneURL.Path)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	// test rule matching
	fmt.Println("\nTesting rules matching (each pattern must have a corresponding substitution)...")
	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"wrong_queue_name"}
	fmt.Printf("\tTesting Rewrite Rules: '%s'... ", config.RewriteRules[0])
	cloneURL, err = Rewrite(config.CloneUrl)
	if cloneURL != nil {
		t.Error("expected cloneURL to be", nil, "got", cloneURL)
	} else if err == nil {
		t.Errorf("Expected to receive an error message")
	} else {
		fmt.Println("passed")
	}

	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{}
	fmt.Printf("\tTesting empty RewriteRules slice... ")
	cloneURL, err = Rewrite(config.CloneUrl)
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

	config.TargetUrl = "http://localhost:8080"
	config.CloneUrl = "http://localhost:8081"


	fmt.Print("Testing inclusion rule... ")
	config.MatchingRule = "localhost"
	makeCloneRequest, err := MatchingRule(config.CloneUrl)
	if !makeCloneRequest {
		t.Error("expected", true, "got", false)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}


	fmt.Print("Testing exlcusion rule... ")
	config.MatchingRule = "!localhost"
	makeCloneRequest, err = MatchingRule(config.CloneUrl)
	if makeCloneRequest {
		t.Error("expected", false, "got", true)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}


	fmt.Print("Testing no rule... ")
	config.MatchingRule = ""
	makeCloneRequest, err = MatchingRule(config.CloneUrl)
	if !makeCloneRequest {
		t.Error("expected", true, "got", false)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}


	fmt.Print("Testing invalid rule... ")
	config.MatchingRule = "localhost:[0-9]++"
	makeCloneRequest, err = MatchingRule(config.CloneUrl)
	if makeCloneRequest {
		t.Error("expected", false, "got", true)
	} else if err == nil {
		t.Errorf("Expected to receive an error message")
	} else {
		fmt.Println("passed")
	}
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
	targetURL := parseUrlWithDefaults(config.TargetUrl)
	cloneURL := parseUrlWithDefaults(config.CloneUrl)

	return NewCloneProxy(targetURL, config.TargetTimeout, config.TargetRewrite, config.TargetInsecure, cloneURL, config.CloneTimeout, config.CloneRewrite, config.CloneInsecure)
}

func TestCloneProxy(t *testing.T) {
	config.TargetUrl = "http://localhost:8080"
	config.CloneUrl = "http://localhost:8081"
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
			config.MatchingRule = configuration.matchingRule

			ts := httptest.NewServer(CloneProxy())
			defer ts.Close()

			tests := []struct {
				name string
				req *http.Request
			}{
				{name: "Testing GET with MatchingRule " + configuration.matchingRule, req: newReq("GET", ts.URL + "/", nil)},
				{name: "Testing POST with MatchingRule " + configuration.matchingRule, req: newReq("POST", ts.URL + "/", nil)},
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
						t.Error(err)
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
}

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}