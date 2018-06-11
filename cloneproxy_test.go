package main

import (
	"testing"
	"os"
	"fmt"
)

func TestRewrite(t *testing.T) {
	// test regex
	fmt.Println("Testing regex...")
	config.TargetUrl = "/project/6ADF0FLKJ2F2LKJSF01/queues/some_queue"
	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"wrong_queue_name", "right_queue_name"}
	fmt.Printf("\tTesting Rewrite Rules: %s, %s... ", config.RewriteRules[0], config.RewriteRules[1])
	err := Rewrite()
	if config.CloneUrl != "/project/5AF308SDF093JF02/queues/right_queue_name" {
		t.Errorf("Expected: /project/5AF308SDF093JF02/queues/wrong_queue_name,\ngot: %s\n", config.CloneUrl)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"wrong_queue_name", ""}
	fmt.Printf("\tTesting Rewrite Rules: %s, %s... ", config.RewriteRules[0], config.RewriteRules[1])
	err = Rewrite()
	if config.CloneUrl != "/project/5AF308SDF093JF02/queues/" {
		t.Errorf("Expected: /project/5AF308SDF093JF02/queues/,\ngot: %s\n", config.CloneUrl)
	} else if err != nil {
		t.Errorf("%s", err)
	}  else {
		fmt.Println("passed")
	}

	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"/[a-z]+_[a-z]+_[a-z]+$", "/right_queue_name"}
	fmt.Printf("\tTesting Rewrite Rules: %s, %s... ", config.RewriteRules[0], config.RewriteRules[1])
	err = Rewrite()
	if config.CloneUrl != "/project/5AF308SDF093JF02/queues/right_queue_name" {
		t.Errorf("Expected: /project/5AF308SDF093JF02/queues/right_queue_name,\ngot: %s\n", config.CloneUrl)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"/project/[A-Z0-9]{16}/queues/wrong_queue_name", "/project/6AF308SDF093JF03/queues/right_queue_name"}
	fmt.Printf("\tTesting Rewrite Rules: %s, %s... ", config.RewriteRules[0], config.RewriteRules[1])
	err = Rewrite()
	if config.CloneUrl != "/project/6AF308SDF093JF03/queues/right_queue_name" {
		t.Errorf("Expected: /project/6AF308SDF093JF03/queues/right_queue_name,\ngot: %s\n", config.CloneUrl)
	} else if err != nil {
		t.Errorf("%s", err)
	} else {
		fmt.Println("passed")
	}

	fmt.Println("\nTesting invalid regex...")
	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"/queues/[0-9]++", "/right_queue_name"}
	fmt.Printf("\tTesting Rewrite Rules: %s, %s... ", config.RewriteRules[0], config.RewriteRules[1])
	err = Rewrite()
	if config.CloneUrl != "/project/5AF308SDF093JF02/queues/wrong_queue_name" {
		t.Errorf("Expected: /project/5AF308SDF093JF02/queues/wrong_queue_name,\ngot: %s\n", config.CloneUrl)
	} else if err == nil {
		t.Errorf("Expected to receive an error message")
	} else {
		fmt.Println("passed")
	}


	// test rule matching
	fmt.Println("\nTesting rules matching (each pattern must have a corresponding substitution)...")
	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{"wrong_queue_name"}
	fmt.Printf("\tTesting Rewrite Rules: %s... ", config.RewriteRules[0])
	err = Rewrite()
	if config.CloneUrl != "/project/5AF308SDF093JF02/queues/wrong_queue_name" {
		t.Errorf("Expected: /project/5AF308SDF093JF02/queues/wrong_queue_name,\ngot: %s\n", config.CloneUrl)
	} else if err == nil {
		t.Errorf("Expected to receive an error message")
	} else {
		fmt.Println("passed")
	}

	config.CloneUrl = "/project/5AF308SDF093JF02/queues/wrong_queue_name"
	config.RewriteRules = []string{}
	fmt.Printf("\tTesting empty RewriteRules slice... ")
	err = Rewrite()
	if config.CloneUrl != "/project/5AF308SDF093JF02/queues/wrong_queue_name" {
		t.Errorf("Expected: /project/5AF308SDF093JF02/queues/wrong_queue_name,\ngot: %s\n", config.CloneUrl)
	} else if err == nil {
		t.Errorf("Expected to receive an error message")
	} else {
		fmt.Println("passed")
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	os.Exit(code)
}