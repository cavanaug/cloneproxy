// ReverseCloneProxy
// - A reverse proxy with a forking of traffic to a clone
//
// You can proxy traffic to production & staging simultaneously.
// This can be used for development/testing/benchmarking, it can
// also be used to replicate traffic while moving across clouds.
//
// TODO:
// -[Done] Create cli with simple reverse proxy (no clone)
// -[Done] <<Testing/Checkpoint>>
// -[Done] Add struct/interface model for ReverseCloneProxy
// -[Done] Should use ServeHTTP which copies the req and calls ServeTargetHTTP
// -[Done] <<Testing/Checkpoint>>
// -[Done] Add sequential calling of ServeCloneHTTP
// -[Done] <<Testing/Checkpoint>>
// -[Done] Add support for timeouts on a & b side
// -[Done] Sync calling of ServeTargetHTTP & only on success call ServeCloneHTTP
// -[Done] <<Testing/Checkpoint>>
// -[Done] Cleanup loglevelging & Add logging similar to what was done for our custom teeproxy
// -[Done] <<Testing/Checkpoint>>
// -[Done] Add in support for percentage of traffic to clone
// -[Done] <<Testing/Checkpoint>>
// -[Done] Add separate context for clone to prevent context cancel exits.
// -[Done-0328] Cleanup context logging & logging in general
// -[Done-0328] Add support for Proxy so I can test this thing from my cube
// -[Done-0328] Add support for detecting mismatch in target/clone and generate warning
// -[Done-0328] Fixed a bug with XFF handling on B side
// -[Done-0328] Add very basic timing information for each side & total
// -[Done-0331] Add support for debug/service endpoint on /debug/vars
// -[Done-0331] Add support regular status log messages (every 15min)
// -[Done-0331] Add tracking for matches/mismatches/unfulfilled/skipped
// -[Done-0403] Add tracking of duration for all cases
// -[Done-0403] Triple check close handling for requests
// -[Done-0403] Adjust default params for Transport
// -[Done-0403] Add support for increasing socket limits
// -[Done-0404] Set client readtimeout & writetimeout
// - (Defer to 2.0) Add support for retry on BadGateway on clone (Wait for go 1.9)
// - (Defer to 2.0) Add support for detailed performance metrics on target/clone responses (see davecheney/httpstat)

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"expvar"
	"flag"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/robfig/cron"
	"github.com/satori/go.uuid"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	//"net/http/httputil"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	version_str = "20170401.2 (cavanaug)"

	// Console flags
	version       = flag.Bool("v", false, "show version number")
	expand_maxtcp = flag.Uint64("custom.maxtcp", 4096, "cloneproxy maximum tcp sockets for use")
	jsonLogging   = flag.Bool("j", false, "write the logs in json for easier processing")
	loglevel      = flag.Int("loglevel", 1, "loglevel log level 0=Error, 1=Warning, 2=Info, 3=Debug, 5=VerboseDebug")

	listen_port    = flag.String("s", ":8888", "server port to listen for requests")
	listen_timeout = flag.Int("s.timeout", 900, "server timeout for clients communicating to server")
	tls_cert       = flag.String("s.tlscert", "", "path to the TLS certificate file")
	tls_key        = flag.String("s.tlskey", "", "path to the TLS private key file")

	target_url      = flag.String("a", "http://localhost:8080", "where target (A-Side) traffic goes")
	target_timeout  = flag.Int("a.timeout", 5, "timeout in seconds for target (A-Side) traffic")
	target_rewrite  = flag.Bool("a.rewrite", false, "rewrite the host header when proxying target (A-Side) traffic")
	target_insecure = flag.Bool("a.insecure", false, "insecure SSL validation for target (A-Side) traffic")

	clone_url      = flag.String("b", "http://localhost:8081", "where clone (B-Side) traffic goes")
	clone_timeout  = flag.Int("b.timeout", 5, "timeout in seconds for clone (B-Side) traffic")
	clone_rewrite  = flag.Bool("b.rewrite", false, "rewrite the host header when proxying clone (B-Side) traffic")
	clone_insecure = flag.Bool("b.insecure", false, "insecure SSL validation for clone (B-Side) traffic")
	clone_percent  = flag.Float64("b.percent", 100.0, "float64 percentage of traffic to send to clone (B Side)")

	total_matches     = expvar.NewInt("total_matches")
	total_mismatches  = expvar.NewInt("total_mismatches")
	total_unfulfilled = expvar.NewInt("total_unfulfilled")
	total_skipped     = expvar.NewInt("total_skipped")
	t_origin          = time.Now()
)

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

//
// Serve the http for the Target
// - This is unmodified from ReverseProxy.ServeHTTP except for logging
func (p *ReverseClonedProxy) ServeTargetHTTP(rw http.ResponseWriter, req *http.Request, uid uuid.UUID) (int, int64) {
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
		return int(http.StatusBadGateway), int64(0)
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
	res_length := p.copyResponse(rw, res.Body)
	res_time := time.Since(t).Nanoseconds() / 1000000
	if *loglevel > 4 {
		log.WithFields(log.Fields{
			"uuid":            uid,
			"side":            "A-Side",
			"response_code":   res.StatusCode,
			"response_time":   res_time,
			"response_length": res_length,
			"response_header": res.Header,
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
	res.Body.Close() // close now, instead of defer, to populate res.Trailer
	copyHeader(rw.Header(), res.Trailer)
	return res.StatusCode, res_length
}

//
// Serve the http for the Clone
// - Handles special casing for the clone (ie. No response back to client)
func (p *ReverseClonedProxy) ServeCloneHTTP(req *http.Request, uid uuid.UUID) (int, int64) {
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
		return http.StatusBadGateway, int64(0)
	}
	defer res.Body.Close() // ensure we dont bleed connections

	body, _ := ioutil.ReadAll(res.Body)
	res_length := int64(len(fmt.Sprintf("%s", body)))
	res_time := time.Since(t).Nanoseconds() / 1000000
	if *loglevel > 4 {
		log.WithFields(log.Fields{
			"uuid":            uid,
			"side":            "B-Side",
			"response_code":   res.StatusCode,
			"response_time":   res_time,
			"response_length": res_length,
			"response_header": res.Header,
			"response_body":   body, //fmt.Sprintf("%s", body),
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

	return res.StatusCode, res_length
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

	//
	// Normal mechanism for expvar support doesnt work with ReverseProxy
	if req.URL.Path == "/debug/vars" && req.Method == "GET" {
		expvar.Handler().ServeHTTP(rw, req)
		return
	}

	// Initialize tracking vars
	uid := uuid.NewV4()
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

	// Clone is a dep copy of original req
	clone_statuscode := 0
	clone_contentlength := int64(0)
	clone_random := rand.New(rand.NewSource(time.Now().UnixNano())).Float64() * 100
	//clone_req := new(http.Request)
	//*clone_req = *req
	//clone_req.Body = nopCloser{b2}
	clone_req := &http.Request{
		Method:        req.Method,
		URL:           req.URL,
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
	target_statuscode, target_contentlength := p.ServeTargetHTTP(rw, target_req, uid)
	req.Body.Close()

	// Process Clone
	//    iff Target returned without server error
	//        && random number is less than percent
	duration := time.Since(t).Nanoseconds() / 1000000
	switch {
	case target_statuscode < 500: // NON-SERVER ERROR
		if *clone_percent == 100.0 || clone_random < *clone_percent {
			clone_statuscode, clone_contentlength = p.ServeCloneHTTP(clone_req, uid)
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

	// Clone SERVER ERROR after procesed Target
	// - This means LOST data at clone
	if clone_statuscode >= 500 {
		total_unfulfilled.Add(1)
		log.WithFields(log.Fields{
			"uuid":            uid,
			"request_method":  req.Method,
			"request_path":    req.URL.RequestURI(),
			"a_response_code": target_statuscode,
			"b_response_code": clone_statuscode,
			"duration":        duration,
		}).Error("Proxy Clone Request Unfulfilled")
		return
	}

	// Clone/Target Mismatch
	// - This means disagreement between Clone & Target
	// - This could be completely ok dependent on how responses are handled
	if (clone_statuscode != target_statuscode) || (clone_contentlength != target_contentlength) {
		total_mismatches.Add(1)
		log.WithFields(log.Fields{
			"uuid":              uid,
			"request_method":    req.Method,
			"request_path":      req.URL.RequestURI(),
			"a_response_code":   target_statuscode,
			"b_response_code":   clone_statuscode,
			"a_response_length": target_contentlength,
			"b_response_length": clone_contentlength,
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
			"duration":              duration,
		}).Info("CloneProxy Responses Match")
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
	}).Info("Cloneproxy Status")
	return
}

func increaseTCPLimits() {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		fmt.Printf("Error: Initialization (%s)\n", err)
		os.Exit(1)
	}
	rLimit.Cur = *expand_maxtcp
	err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		fmt.Printf("Error: Initialization (%s)\n", err)
		os.Exit(1)
	}
}

func main() {
	//
	// Handle Option Processing
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Version: %s\n\n", version_str)
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
		fmt.Printf("cloneproxy version: %s\n", version_str)
		os.Exit(0)
	}

	log.SetOutput(os.Stdout)
	// Log as JSON instead of the default ASCII formatter
	if *jsonLogging {
		log.SetFormatter(&log.JSONFormatter{})
	}
	// Set appropriate logging level
	switch {
	case *loglevel == 0:
		log.SetLevel(log.ErrorLevel)
	case *loglevel == 1:
		log.SetLevel(log.WarnLevel)
	case *loglevel == 2:
		log.SetLevel(log.InfoLevel)
	case *loglevel >= 3:
		log.SetLevel(log.DebugLevel)
	}

	if !strings.HasPrefix(*target_url, "http") {
		fmt.Printf("Error: target url %s is invalid\n   URL's must have a scheme defined, either http or https\n\n", *target_url)
		flag.Usage()
		os.Exit(1)
	}
	if !strings.HasPrefix(*clone_url, "http") {
		fmt.Printf("Error: clone url %s is invalid\n   URL's must have a scheme defined, either http or https\n\n", *clone_url)
		flag.Usage()
		os.Exit(1)
	}

	//
	// Begin actual main function
	increaseTCPLimits()
	runtime.GOMAXPROCS(runtime.NumCPU() * 2)

	targetURL := parseUrlWithDefaults(*target_url)
	cloneURL := parseUrlWithDefaults(*clone_url)
	proxy := NewCloneProxy(targetURL, *target_timeout, *target_rewrite, *target_insecure, cloneURL, *clone_timeout, *clone_rewrite, *clone_insecure)

	//
	// Regular publication of status messages
	c := cron.New()
	c.AddFunc("0 0/15 * * *", logStatus)
	c.Start()

	logStatus()
	s := &http.Server{
		Addr:         *listen_port,
		WriteTimeout: time.Duration(time.Duration(*listen_timeout) * time.Second),
		ReadTimeout:  time.Duration(time.Duration(*listen_timeout) * time.Second),
		Handler:      proxy,
		// Probably should add some denial of service max sizes etc...
	}
	if len(*tls_key) > 0 {
		log.Fatal(s.ListenAndServeTLS(*tls_cert, *tls_key))
	} else {
		log.Fatal(s.ListenAndServe())
	}
}
