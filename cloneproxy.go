/*
ReverseCloneProxy
- A reverse proxy with a forking of traffic to a clone

You can proxy traffic to production & staging simultaneously.
This can be used for development/testing/benchmarking, it can
also be used to replicate traffic while moving across clouds.

TODO:
-[Done] Create cli with simple reverse proxy (no clone)
-[Done] <<Testing/Checkpoint>>
-[Done] Add struct/interface model for ReverseCloneProxy
-[Done] Should use ServeHTTP which copies the req and calls ServeTargetHTTP
-[Done] <<Testing/Checkpoint>>
-[Done] Add sequential calling of ServeCloneHTTP
-[Done] <<Testing/Checkpoint>>
-[Done] Add support for timeouts on a & b side
-[Done] Sync calling of ServeTargetHTTP & only on success call ServeCloneHTTP
-[Done] <<Testing/Checkpoint>>
-[Done] Cleanup loglevelging & Add logging similar to what was done for our custom teeproxy
-[Done] <<Testing/Checkpoint>>
-[Done] Add in support for percentage of traffic to clone
-[Done] <<Testing/Checkpoint>>
-[Done] Add separate context for clone to prevent context cancel exits.
-[Done-0328] Cleanup context logging & logging in general
-[Done-0328] Add support for Proxy so I can test this thing from my cube
-[Done-0328] Add support for detecting mismatch in target/clone and generate warning
-[Done-0328] Fixed a bug with XFF handling on B side
-[Done-0328] Add very basic timing information for each side & total
-[Done-0331] Add support for debug/service endpoint on /debug/vars
-[Done-0331] Add support regular status log messages (every 15min)
-[Done-0331] Add tracking for matches/mismatches/unfulfilled/skipped
-[Done-0403] Add tracking of duration for all cases
-[Done-0403] Triple check close handling for requests
-[Done-0403] Adjust default params for Transport
-[Done-0403] Add support for increasing socket limits
-[Done-0404] Set client readtimeout & writetimeout
- (Defer to 2.0) Add support for retry on BadGateway on clone (Wait for go 1.9)
- (Defer to 2.0) Add support for detailed performance metrics on target/clone responses (see davecheney/httpstat)
-[Done-0418] Regular status messages as part of default level 1 (Warn) setting
*/

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/sha1"
	"expvar"
	"flag"
	"fmt"
	"github.com/hjson/hjson-go"
	log "github.com/Sirupsen/logrus"
	"github.com/robfig/cron"
	"github.com/satori/go.uuid"
	"encoding/json"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"encoding/hex"
	"strconv"
)

type Config struct {
	Version			bool
	ExpandMaxTcp	uint64
	JsonLogging		bool
	LogLevel		int
	LogFilePath		string

	ListenPort		string
	ListenTimeout	int
	TlsCert			string
	TlsKey			string

	TargetTimeout	int
	TargetRewrite	bool
	TargetInsecure	bool

	CloneTimeout	int
	CloneRewrite	bool
	CloneInsecure	bool
	ClonePercent	float64

	Paths			map[string]map[string]interface{}
}

const exclusionFlag = "!"

var (
	VERSION				string
	minversion			string
	version_str		  = "20180808.0 (cavanaug)"

	configData map[string]interface{}
	config Config
	cloneproxyHeader  = "X-Cloneproxy-Request"
	sideServedheader  = "X-Cloneproxy-Served"

	configFile		  = flag.String("config-file", "config.hjson", "path to the hjson configuration file")
	version			  = flag.Bool("version", false, VERSION)

	total_matches     = expvar.NewInt("total_matches")
	total_mismatches  = expvar.NewInt("total_mismatches")
	total_unfulfilled = expvar.NewInt("total_unfulfilled")
	total_skipped     = expvar.NewInt("total_skipped")
	t_origin          = time.Now()
)


func configuration(configFile string) {
	raw, err := ioutil.ReadFile(configFile)
	if err != nil {
		fmt.Printf("Error, missing %s file", configFile)
		os.Exit(1)
	}

	hjson.Unmarshal(raw, &configData)
	configJson, _ := json.Marshal(configData)
	json.Unmarshal(configJson, &config)
}

// **********************************************************************************
// Begin:  Package components  (TODO: Should probably packagize this...)
// **********************************************************************************

// Heavily derived from
// HTTP reverse proxy handler

// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// onExitFlushLoop is a callback set by tests to detect the state of the
// flushLoop() goroutine.
var onExitFlushLoop func()

type baseHandle struct {}

// ReverseClonedProxy is an HTTP Handler that takes an incoming request and
// sends it to another server, proxying the response back to the
// client.
type ReverseClonedProxy struct {
	// Director must be a function which modifies
	// the request into a new request to be sent
	// using Transport. Its response is then copied
	// back to the original client unmodified.
	// Director must not access the provided Request
	// after returning.
	Director      func(*http.Request)
	DirectorClone func(*http.Request)

	// The transport used to perform proxy requests.
	// If nil, http.DefaultTransport is used.
	Transport      http.RoundTripper
	TransportClone http.RoundTripper

	// FlushInterval specifies the flush interval
	// to flush to the client while copying the
	// response body.
	// If zero, no periodic flushing is done.
	FlushInterval time.Duration

	// ErrorLog specifies an optional logger for errors
	// that occur when attempting to proxy the request.
	// If nil, logging goes to os.Stderr via the log package's
	// standard logger.

	ErrorLog *log.Logger

	// BufferPool optionally specifies a buffer pool to
	// get byte slices for use by io.CopyBuffer when
	// copying HTTP response bodies.
	BufferPool BufferPool

	// ModifyResponse is an optional function that
	// modifies the Response from the backend.
	// If it returns an error, the proxy returns a StatusBadGateway error.
	ModifyResponse func(*http.Response) error
	//ModifyResponseClone func(*http.Response) error
}

// A BufferPool is an interface for getting and returning temporary
// byte slices for use by io.CopyBuffer.
type BufferPool interface {
	Get() []byte
	Put([]byte)
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// Hop-by-hop headers. These are removed when sent to the backend.
// http://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection", // non-standard but still sent by libcurl and rejected by e.g. google
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",      // canonicalized version of "TE"
	"Trailer", // not Trailers per URL above; http://www.rfc-editor.org/errata_search.php?eid=4522
	"Transfer-Encoding",
	"Upgrade",
}

func removeHeaders(body string, headers []string) (string) {
	bodyString := body
	for _, header := range headers {
		bodyString = strings.Replace(bodyString, header, "", -1)
	}
	return bodyString
}

func sha1Body(body []byte, headers []string) (string) {
	stringBody := string(body)
	//stringBody = removeHeaders(stringBody, headers)
	hasher := sha1.New()
	hasher.Write([]byte(stringBody))
	return hex.EncodeToString(hasher.Sum(nil))
}

func getConfigPath(requestURI string) (string, error) {
	var pathKey string
	for path := range config.Paths {
		if strings.Contains(requestURI, path) {
			pathKey = path
			return pathKey, nil
		}
	}
	return "", fmt.Errorf("Error: No path contains %s in the config file\n\n", requestURI)
}

func setCloneproxyHeader(reqHeader http.Header, outreq *http.Request) {
	targetServed := reqHeader.Get(cloneproxyHeader)
	if targetServed != "" {
		count, err := strconv.Atoi(targetServed)
		if err == nil {
			count++
			outreq.Header.Set(cloneproxyHeader, strconv.Itoa(count))
		}
	} else {
		outreq.Header.Set(cloneproxyHeader, "1")
	}
}

// Routes requests to appropriate ReverseCloneProxy handler
func (h *baseHandle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestURI := r.RequestURI

	pathKey, _ := getConfigPath(requestURI)

	if targetClone, ok := config.Paths[pathKey]; ok {
		configTargetUrl := targetClone["target"].(string)
		configCloneUrl := targetClone["clone"].(string)

		targetURL := parseUrlWithDefaults(configTargetUrl)
		cloneURL := parseUrlWithDefaults(configCloneUrl)

		if !strings.HasPrefix(configTargetUrl, "http") {
			fmt.Printf("Error: target url %s is invalid\n   URL's must have a scheme defined, either http or https\n\n", configTargetUrl)
			flag.Usage()
			os.Exit(1)
		}
		if configCloneUrl != "" && !strings.HasPrefix(configCloneUrl, "http") {
			fmt.Printf("Error: clone url %s is invalid\n   URL's must have a scheme defined, either http or https\n\n", configCloneUrl)
			flag.Usage()
			os.Exit(1)
		}

		proxy := NewCloneProxy(targetURL, config.TargetTimeout, config.TargetRewrite, config.TargetInsecure, cloneURL, config.CloneTimeout, config.CloneRewrite, config.CloneInsecure)
		proxy.ServeHTTP(w, r)
		return
	}
	log.Println("Unable to process request for", requestURI)
}

//
// Serve the http for the Target
// - This is unmodified from ReverseProxy.ServeHTTP except for logging
func (p *ReverseClonedProxy) ServeTargetHTTP(rw http.ResponseWriter, req *http.Request, uid uuid.UUID) (int, int64, string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered: %s\n", r)
		}
	}()

	t := time.Now()
	transport := p.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	ctx := req.Context()
	if cn, ok := rw.(http.CloseNotifier); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		notifyChan := cn.CloseNotify()
		go func() {
			select {
			case <-notifyChan:
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	outreq := new(http.Request)
	*outreq = *req // includes shallow copies of maps, but okay
	if req.ContentLength == 0 {
		outreq.Body = nil // Issue 16036: nil Body for http.Transport retries
	}
	outreq = outreq.WithContext(ctx)

	p.Director(outreq)
	outreq.Close = false

	// We are modifying the same underlying map from req (shallow
	// copied above) so we only copy it if necessary.
	copiedHeaders := false

	// Remove hop-by-hop headers listed in the "Connection" header.
	// See RFC 2616, section 14.10.
	if c := outreq.Header.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				if !copiedHeaders {
					outreq.Header = make(http.Header)
					copyHeader(outreq.Header, req.Header)
					copiedHeaders = true
				}
				outreq.Header.Del(f)
			}
		}
	}

	// Remove hop-by-hop headers to the backend. Especially
	// important is "Connection" because we want a persistent
	// connection, regardless of what the client sent to us.
	for _, h := range hopHeaders {
		if outreq.Header.Get(h) != "" {
			if !copiedHeaders {
				outreq.Header = make(http.Header)
				copyHeader(outreq.Header, req.Header)
				copiedHeaders = true
			}
			outreq.Header.Del(h)
		}
	}

	if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		// If we aren't the first proxy retain prior
		// X-Forwarded-For information as a comma+space
		// separated list and fold multiple headers into one.
		if prior, ok := outreq.Header["X-Forwarded-For"]; ok {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		outreq.Header.Set("X-Forwarded-For", clientIP)
	}
	setCloneproxyHeader(req.Header, outreq)
	outreq.Header.Set(sideServedheader, "target (a-side)")

	log.WithFields(log.Fields{
		"uuid":           uid,
		"side":           "A-Side",
		"request_method": outreq.Method,
		"request_path":   outreq.URL.RequestURI(),
		"request_proto":  outreq.Proto,
		"request_host":   outreq.Host,
		//		"request_header":        outreq.Header,
		"request_contentlength": outreq.ContentLength,
	}).Debug("Proxy Request")

	res, err := transport.RoundTrip(outreq)
	if err != nil {
		log.WithFields(log.Fields{
			"uuid":          uid,
			"side":          "A-Side",
			"response_code": http.StatusBadGateway,
			"error":         err,
		}).Error("Proxy Response")
		rw.WriteHeader(http.StatusBadGateway)
		return int(http.StatusBadGateway), int64(0), ""
	}

	// Remove hop-by-hop headers listed in the
	// "Connection" header of the response.
	if c := res.Header.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				res.Header.Del(f)
			}
		}
	}

	for _, h := range hopHeaders {
		res.Header.Del(h)
	}

	copyHeader(rw.Header(), res.Header)

	// The "Trailer" header isn't included in the Transport's response,
	// at least for *http.Transport. Build it up from Trailer.
	if len(res.Trailer) > 0 {
		trailerKeys := make([]string, 0, len(res.Trailer))
		for k := range res.Trailer {
			trailerKeys = append(trailerKeys, k)
		}
		rw.Header().Add("Trailer", strings.Join(trailerKeys, ", "))
	}

	rw.WriteHeader(res.StatusCode)
	if len(res.Trailer) > 0 {
		// Force chunking if we saw a response trailer.
		// This prevents net/http from calculating the length for short
		// bodies and adding a Content-Length.
		if fl, ok := rw.(http.Flusher); ok {
			fl.Flush()
		}
	}

	body, _ := ioutil.ReadAll(res.Body)
	p.copyResponse(rw, res.Body)
	res_length := int64(len(fmt.Sprintf("%s", body)))
	res_time := time.Since(t).Nanoseconds() / 1000000

	headersToRemove := []string{
		// xff is present for target but not in clone and must be removed to do a proper comparison
		"X-Forwarded-For: " + res.Request.Header["X-Forwarded-For"][0] + "\r\n",
	}
	if config.LogLevel > 4 {
		log.WithFields(log.Fields{
			"uuid":            uid,
			"side":            "A-Side",
			"response_code":   res.StatusCode,
			"response_time":   res_time,
			"response_length": res_length,
			"response_header": res.Header,
			"response_body":   string(body),
		}).Debug("Proxy Response (loglevel)")
	} else {
		log.WithFields(log.Fields{
			"uuid":            uid,
			"side":            "A-Side",
			"response_time":   res_time,
			"response_code":   res.StatusCode,
			"response_length": res_length,
		}).Debug("Proxy Response")
	}

	sha := sha1Body(body, headersToRemove)
	fmt.Fprintf(rw, string(body))

	res.Body.Close() // close now, instead of defer, to populate res.Trailer
	copyHeader(rw.Header(), res.Trailer)
	return res.StatusCode, res_length, sha
}

//
// Serve the http for the Clone
// - Handles special casing for the clone (ie. No response back to client)
func (p *ReverseClonedProxy) ServeCloneHTTP(req *http.Request, uid uuid.UUID) (int, int64, string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered: %s\n", r)
		}
	}()

	t := time.Now()
	transport := p.TransportClone
	if transport == nil {
		transport = http.DefaultTransport
	}

	outreq := new(http.Request)
	*outreq = *req // includes shallow copies of maps, but okay
	if req.ContentLength == 0 {
		outreq.Body = nil // Issue 16036: nil Body for http.Transport retries
	}

	// Hmm.   Im not an expert on how contexts & cancels are handled.
	// Im making potentially a dangerous assumption that giving the clone
	// side a new context, this wont get cancelled on a client.Done.  In essence
	// no cancellation on clone if the target & client are complete.
	outreq = outreq.WithContext(context.TODO())

	p.DirectorClone(outreq)
	outreq.Close = false

	// We are modifying the same underlying map from req (shallow
	// copied above) so we only copy it if necessary.
	copiedHeaders := false

	// Remove hop-by-hop headers listed in the "Connection" header.
	// See RFC 2616, section 14.10.
	if c := outreq.Header.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				if !copiedHeaders {
					outreq.Header = make(http.Header)
					copyHeader(outreq.Header, req.Header)
					copiedHeaders = true
				}
				outreq.Header.Del(f)
			}
		}
	}

	// Remove hop-by-hop headers to the backend. Especially
	// important is "Connection" because we want a persistent
	// connection, regardless of what the client sent to us.
	for _, h := range hopHeaders {
		if outreq.Header.Get(h) != "" {
			if !copiedHeaders {
				outreq.Header = make(http.Header)
				copyHeader(outreq.Header, req.Header)
				copiedHeaders = true
			}
			outreq.Header.Del(h)
		}
	}

	if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		// If we aren't the first proxy retain prior
		// X-Forwarded-For information as a comma+space
		// separated list and fold multiple headers into one.
		if prior, ok := outreq.Header["X-Forwarded-For"]; ok {
			clientIP = strings.Join(prior, ", ") + ", " + clientIP
		}
		outreq.Header.Set("X-Forwarded-For", clientIP)
	}

	setCloneproxyHeader(req.Header, outreq)
	outreq.Header.Set(sideServedheader, "clone (b-side)")

	log.WithFields(log.Fields{
		"uuid":                  uid,
		"side":                  "B-Side",
		"request_method":        outreq.Method,
		"request_path":          outreq.URL.RequestURI(),
		"request_proto":         outreq.Proto,
		"request_host":          outreq.Host,
		"request_contentlength": outreq.ContentLength,
	}).Debug("Proxy Request")

	res, err := transport.RoundTrip(outreq)
	if err != nil {
		log.WithFields(log.Fields{
			"uuid":          uid,
			"side":          "B-Side",
			"response_code": http.StatusBadGateway,
			"error":         err,
		}).Error("Proxy Response")
		return http.StatusBadGateway, int64(0), ""
	}
	defer res.Body.Close() // ensure we dont bleed connections

	body, _ := ioutil.ReadAll(res.Body)
	res_length := int64(len(fmt.Sprintf("%s", body)))
	res_time := time.Since(t).Nanoseconds() / 1000000

	var headersToRemove []string

	if config.LogLevel > 4 {
		log.WithFields(log.Fields{
			"uuid":            uid,
			"side":            "B-Side",
			"response_code":   res.StatusCode,
			"response_time":   res_time,
			"response_length": res_length,
			"response_header": res.Header,
			"response_body":   string(body),
		}).Debug("Proxy Response (Details)")
	} else {
		log.WithFields(log.Fields{
			"uuid":            uid,
			"side":            "B-Side",
			"response_code":   res.StatusCode,
			"response_time":   res_time,
			"response_length": res_length,
		}).Debug("Proxy Response")
	}

	// Remove hop-by-hop headers listed in the
	// "Connection" header of the response.
	if c := res.Header.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				res.Header.Del(f)
			}
		}
	}

	for _, h := range hopHeaders {
		res.Header.Del(h)
	}

	sha := sha1Body(body, headersToRemove)

	return res.StatusCode, res_length, sha
}

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

// ***************************************************************************
// Handle umbrella ServeHTTP interface
// - Replicates the request
// - Call each of ServeTargetHTTP & ServeCloneHTTP asynchronously
// - Nothing else...
func (p *ReverseClonedProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered: %s\n", r)
		}
	}()

	// Normal mechanism for expvar support doesnt work with ReverseProxy
	if req.URL.Path == "/debug/vars" && req.Method == "GET" {
		expvar.Handler().ServeHTTP(rw, req)
		return
	}

	cloneURL, err := Rewrite(req.URL.RequestURI())
	if err != nil {
		fmt.Println(err)
	}
	if cloneURL == nil {
		cloneURL = req.URL
	}

	makeCloneRequest, err := MatchingRule(req.URL.RequestURI())
	if err != nil {
		fmt.Println(err)
	}

	targetServed := req.Header.Get(cloneproxyHeader)
	if targetServed != "" {
		//count, err := strconv.Atoi(targetServed)
		//if err == nil {
		//	if count > 1 {
		//		log.WithFields(log.Fields{
		//			"cloneproxied traffic count": targetServed,
		//		}).Info("Cloneproxied traffic counter exceeds maximum at ", count)
		//		fmt.Println("Cloneproxied traffic exceed maximum at:", targetServed)
		//		return
		//	}
		//	if count == 1 {
		//		// only serve a-side (target)
		//		makeCloneRequest = false
		//	}
		//}
		log.WithFields(log.Fields{
			"cloneproxied traffic count": targetServed,
		}).Info("Cloneproxied traffic counter: ", targetServed)
		fmt.Println("Cloneproxied traffic counter:", targetServed)
	}


	// Initialize tracking vars
	uid, _ := uuid.NewV4()
	t := time.Now()

	// Copy Body
	b1 := new(bytes.Buffer)
	b2 := new(bytes.Buffer)
	w := io.MultiWriter(b1, b2)
	io.Copy(w, req.Body)

	// Target is a pointer copy of original req with new iostream for Body
	target_req := new(http.Request)
	*target_req = *req
	target_req.Body = nopCloser{b1}

	// Clone is a deep copy of original req
	clone_statuscode := 0
	clone_contentlength := int64(0)
	clone_sha1 := ""
	clone_random := rand.New(rand.NewSource(time.Now().UnixNano())).Float64() * 100
	//clone_req := new(http.Request)
	//*clone_req = *req
	//clone_req.Body = nopCloser{b2}

	clone_req := &http.Request{
		Method:        req.Method,
		URL:           cloneURL,
		Proto:         req.Proto,
		ProtoMajor:    req.ProtoMajor,
		ProtoMinor:    req.ProtoMinor,
		Header:        req.Header,
		Body:          nopCloser{b2},
		Host:          req.Host,
		ContentLength: req.ContentLength,
		Close:         req.Close,
	}

	//defer target_req.Body.Close()
	//defer clone_req.Body.Close()

	// Process Target
	target_statuscode, target_contentlength, target_sha1 := p.ServeTargetHTTP(rw, target_req, uid)
	req.Body.Close()

	// Process Clone
	//    iff Target returned without server error
	//        && random number is less than percent
	duration := time.Since(t).Nanoseconds() / 1000000
	switch {
	case target_statuscode < 500: // NON-SERVER ERROR
		if makeCloneRequest && (config.ClonePercent == 100.0 || clone_random < config.ClonePercent) {
			clone_statuscode, clone_contentlength, clone_sha1 = p.ServeCloneHTTP(clone_req, uid)
			// Ultra simple timing information for total of both a & b
			duration = time.Since(t).Nanoseconds() / 1000000
		}
	case target_statuscode >= 500: // SERVER ERROR
		total_skipped.Add(1)
		log.WithFields(log.Fields{
			"uuid":            uid,
			"request_method":  req.Method,
			"request_path":    req.URL.RequestURI(),
			"a_response_code": target_statuscode,
			"b_response_code": 0,
			"duration":        duration,
		}).Info("Proxy Clone Request Skipped")
		return
	}

	// Clone SERVER ERROR after processed Target
	// - This means LOST data at clone
	if makeCloneRequest && clone_statuscode >= 500 {
		total_unfulfilled.Add(1)
		log.WithFields(log.Fields{
			"uuid":            uid,
			"request_method":  req.Method,
			"request_path":    cloneURL,
			"a_response_code": target_statuscode,
			"b_response_code": clone_statuscode,
			"duration":        duration,
		}).Error("Proxy Clone Request Unfulfilled")
		return
	}

	// Clone/Target Mismatch
	// - This means disagreement between Clone & Target
	// - This could be completely ok dependent on how responses are handled
	infoSuccess := "Proxy Clone Request Skipped"
	if makeCloneRequest && clone_statuscode > 0 {
		infoSuccess = "CloneProxy Responses Match"
	}

	if (makeCloneRequest && clone_statuscode > 0) && ((clone_statuscode != target_statuscode) || (clone_sha1 != target_sha1) || (clone_contentlength != target_contentlength)) {
		total_mismatches.Add(1)
		log.WithFields(log.Fields{
			"uuid":              uid,
			"request_method":    req.Method,
			"request_path":      req.URL.RequestURI(),
			"a_response_code":   target_statuscode,
			"b_response_code":   clone_statuscode,
			"a_response_length": target_contentlength,
			"b_response_length": clone_contentlength,
			"a_sha1":			 target_sha1,
			"b_sha1":			 clone_sha1,
			"duration":          duration,
		}).Warn("CloneProxy Responses Mismatch")
	} else {
		total_matches.Add(1)
		log.WithFields(log.Fields{
			"uuid":                  uid,
			"request_method":        req.Method,
			"request_path":          req.URL.RequestURI(),
			"request_contentlength": req.ContentLength,
			"response_code":         target_statuscode,
			"response_length":       target_contentlength,
			"sha1":					 target_sha1,
			"duration":              duration,
			"clone_request":		 makeCloneRequest,
		}).Info(infoSuccess)
	}

	return
}

func (p *ReverseClonedProxy) copyResponse(dst io.Writer, src io.Reader) int64 {
	if p.FlushInterval != 0 {
		if wf, ok := dst.(writeFlusher); ok {
			mlw := &maxLatencyWriter{
				dst:     wf,
				latency: p.FlushInterval,
				done:    make(chan bool),
			}
			go mlw.flushLoop()
			defer mlw.stop()
			dst = mlw
		}
	}

	var buf []byte
	if p.BufferPool != nil {
		buf = p.BufferPool.Get()
	}
	written, _ := p.copyBuffer(dst, src, buf)
	if p.BufferPool != nil {
		p.BufferPool.Put(buf)
	}
	return written
}

func (p *ReverseClonedProxy) copyBuffer(dst io.Writer, src io.Reader, buf []byte) (int64, error) {
	if len(buf) == 0 {
		buf = make([]byte, 32*1024)
	}
	var written int64
	for {
		nr, rerr := src.Read(buf)
		if rerr != nil && rerr != io.EOF {
			log.Error("util: CloneProxy read error during resp body copy: %v", rerr)
		}
		if nr > 0 {
			nw, werr := dst.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if werr != nil {
				return written, werr
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}
		if rerr != nil {
			return written, rerr
		}
	}
}

type writeFlusher interface {
	io.Writer
	http.Flusher
}

type maxLatencyWriter struct {
	dst     writeFlusher
	latency time.Duration

	mu   sync.Mutex // protects Write + Flush
	done chan bool
}

func (m *maxLatencyWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dst.Write(p)
}

func (m *maxLatencyWriter) flushLoop() {
	t := time.NewTicker(m.latency)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			if onExitFlushLoop != nil {
				onExitFlushLoop()
			}
			return
		case <-t.C:
			m.mu.Lock()
			m.dst.Flush()
			m.mu.Unlock()
		}
	}
}

func (m *maxLatencyWriter) stop() { m.done <- true }

func parseUrlWithDefaults(ustr string) *url.URL {
	if ustr == "" {
		return new(url.URL)
	}
	u, err := url.ParseRequestURI(ustr)
	if err != nil {
		fmt.Printf("Error: Unable to parse url %s  (Ex.  http://localhost:9001)", ustr)
		os.Exit(1)
	}
	//if u.Port() == "" && u.Scheme == "https" {
	//	u.Host = fmt.Sprintf("%s:443", u.Host)
	//}
	//if u.Port() == "" && u.Scheme == "http" {
	//	u.Host = fmt.Sprintf("%s:80", u.Host)
	//}
	return u
}

// select a host from the passed `targets`
func NewCloneProxy(target *url.URL, target_timeout int, target_rewrite bool, target_insecure bool, clone *url.URL, clone_timeout int, clone_rewrite bool, clone_insecure bool) *ReverseClonedProxy {
	targetQuery := target.RawQuery
	cloneQuery := clone.RawQuery
	director := func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
		if target.Scheme == "https" || target_rewrite {
			req.Host = target.Host
		}
		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
	}
	directorclone := func(req *http.Request) {
		req.URL.Scheme = clone.Scheme
		req.URL.Host = clone.Host
		req.URL.Path = singleJoiningSlash(clone.Path, req.URL.Path)
		if clone.Scheme == "https" || clone_rewrite {
			req.Host = clone.Host
		}
		if cloneQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = cloneQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = cloneQuery + "&" + req.URL.RawQuery
		}
		if _, ok := req.Header["User-Agent"]; !ok {
			// explicitly disable User-Agent so it's not set to default value
			req.Header.Set("User-Agent", "")
		}
	}
	return &ReverseClonedProxy{
		Director:      director,
		DirectorClone: directorclone,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			Dial: (&net.Dialer{
				Timeout:   time.Duration(time.Duration(target_timeout) * time.Second),
				KeepAlive: 60 * time.Second,
			}).Dial,
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 50,
			TLSHandshakeTimeout: 5 * time.Second,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: target_insecure,
			},
		},
		TransportClone: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			Dial: (&net.Dialer{
				Timeout:   time.Duration(time.Duration(clone_timeout) * time.Second),
				KeepAlive: 60 * time.Second,
			}).Dial,
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 50,
			TLSHandshakeTimeout: 5 * time.Second,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: clone_insecure,
			},
		},
	}
}

//func expvarVersion() interface{} {
//	return version_str
//}
func init() {
	//	expvar.Publish("version", expvar.Func(expvarVersion))
	//	expvar.Publish("cmdline", Func(cmdline))
	//	expvar.Publish("memstats", Func(memstats))
	//	total_matches.Set(0)
	//	total_mismatches.Set(0)
}
func logStatus() {
	log.WithFields(log.Fields{
		"version":           version_str,
		"cli":               strings.Join(os.Args, " "),
		"total_matches":     total_matches.Value(),
		"total_mismatches":  total_mismatches.Value(),
		"total_unfulfilled": total_unfulfilled.Value(),
		"total_skipped":     total_skipped.Value(),
		"uptime":            time.Since(t_origin).String(),
	}).Warn("Cloneproxy Status")
	return
}

func increaseTCPLimits() {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		fmt.Printf("Error: Initialization (%s)\n", err)
		os.Exit(1)
	}
	rLimit.Cur = config.ExpandMaxTcp
	err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		fmt.Printf("Error: Initialization (%s)\n", err)
		os.Exit(1)
	}
}

func Rewrite(request string) (*url.URL, error) {
	pathKey, err := getConfigPath(request)

	if err != nil {
		return nil, err
	}

	configRewrite := config.Paths[pathKey]["rewrite"].(bool)
	if configRewrite {
		rewrite := request

		rewriteRules := config.Paths[pathKey]["rewriteRules"].([]interface{})
		configRewriteRules := make([]string, 0, len(rewriteRules))
		for _, rule := range rewriteRules {
			configRewriteRules = append(configRewriteRules, rule.(string))
		}
		if len(configRewriteRules) % 2 != 0 || len(configRewriteRules) < 1 {
			return nil, fmt.Errorf("Error: rewrite rule mismatch\n	Each pattern must have a corresponding substitution\n")
		}

		for i := 0; i < len(configRewriteRules) - 1; i += 2 {
			pattern, err := regexp.Compile(configRewriteRules[i])

			if err != nil {
				return nil, fmt.Errorf("Error: %s is an invalid regex, not rewriting URL\n\n", configRewriteRules[i])
			}

			rewrite = pattern.ReplaceAllString(rewrite, configRewriteRules[i+1])
		}

		return url.Parse(rewrite)
	}
	return nil, nil
}

func MatchingRule(request string) (bool, error) {
	pathKey, err := getConfigPath(request)

	if err != nil {
		return false, err
	}

	configMatchingRule := config.Paths[pathKey]["matchingRule"].(string)
	configCloneUrl := config.Paths[pathKey]["clone"].(string)
	if configMatchingRule != "" {
		exclude := strings.Contains(configMatchingRule, exclusionFlag)
		matchingRule := strings.TrimPrefix(configMatchingRule, exclusionFlag)
		pattern, err := regexp.Compile(matchingRule)

		if err != nil {
			return false, fmt.Errorf("Error: %s is an invalid regex, not sending to %s\n\n", configMatchingRule, configCloneUrl)
		}

		matches := pattern.MatchString(request)
		if (exclude && matches) || (!exclude && !matches) {
			// exclude: targetURLs matching the pattern || include: targetURLs not matching the pattern do not go to the b-side
			return false, nil
		}
	}

	return true, nil
}

func main() {
	// Handle Option Processing
	flag.Usage = func() {
			   fmt.Fprintf(os.Stderr, "Version: %s\n\n", VERSION)
			   fmt.Fprintf(os.Stderr, "USAGE: %s [OPTIONS]\n\nOPTIONS:\n", os.Args[0])
			   flag.PrintDefaults()
			   fmt.Fprintf(os.Stderr, `
%s:
  HTTP_PROXY    proxy for HTTP requests; complete URL or HOST[:PORT]
                used for HTTPS requests if HTTPS_PROXY undefined
  HTTPS_PROXY   proxy for HTTPS requests; complete URL or HOST[:PORT]
  NO_PROXY      comma-separated list of hosts to exclude from proxy
`, "ENVIRONMENT")
			   fmt.Fprintf(os.Stderr, `
%s:
  WebSite       https://github.com/cavanaug/cloneproxy
`, "MISC")
	}
	flag.Parse()

	if *version {
		fmt.Printf("Version: %s\tBuild Date: %s\n", VERSION, minversion)
		return
	}

	fmt.Printf("Version: %s\tBuild Date: %s\n", VERSION, minversion)
	configuration(*configFile)

	if config.Version {
		fmt.Printf("cloneproxy version: %s\n", version_str)
		os.Exit(0)
	}

	log.SetOutput(os.Stdout)
	if config.LogFilePath != "" {
		file, err := os.OpenFile(config.LogFilePath, os.O_CREATE|os.O_WRONLY, 0666)
		if err == nil {
			multiWriter := io.MultiWriter(os.Stdout, file)
			log.SetOutput(multiWriter)
			defer file.Close()
		} else {
			fmt.Print("Failed to log to file, using default stderr")
		}
	}
	// Log as JSON instead of the default ASCII formatter
	if config.JsonLogging {
		log.SetFormatter(&log.JSONFormatter{})
	}
	// Set appropriate logging level
	switch {
	case config.LogLevel == 0:
		log.SetLevel(log.ErrorLevel)
	case config.LogLevel == 1:
		log.SetLevel(log.WarnLevel)
	case config.LogLevel == 2:
		log.SetLevel(log.InfoLevel)
	case config.LogLevel >= 3:
		log.SetLevel(log.DebugLevel)
	}

	// Begin actual main function
	increaseTCPLimits()
	runtime.GOMAXPROCS(runtime.NumCPU() * 2)

	// Regular publication of status messages
	c := cron.New()
	c.AddFunc("0 0/15 * * *", logStatus)
	c.Start()


	logStatus()
	s := &http.Server{
		Addr:         config.ListenPort,
		WriteTimeout: time.Duration(time.Duration(config.ListenTimeout) * time.Second),
		ReadTimeout:  time.Duration(time.Duration(config.ListenTimeout) * time.Second),
		Handler:      &baseHandle{},
		// TODO: Probably should add some denial of service max sizes etc...
	}
	if len(config.TlsKey) > 0 {
		log.Fatal(s.ListenAndServeTLS(config.TlsCert, config.TlsKey))
	} else {
		log.Fatal(s.ListenAndServe())
	}
}
