package vcr

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

// Interaction records a single HTTP request/response pair.
type Interaction struct {
	Request  Request  `json:"request"`
	Response Response `json:"response"`
}

// Request is the recorded HTTP request.
type Request struct {
	Method string      `json:"method"`
	URL    string      `json:"url"`
	Header http.Header `json:"header,omitempty"`
	Body   string      `json:"body,omitempty"`
}

// Response is the recorded HTTP response.
type Response struct {
	StatusCode int         `json:"status_code"`
	Header     http.Header `json:"header,omitempty"`
	Body       string      `json:"body"`
}

// Cassette holds a sequence of recorded interactions.
type Cassette struct {
	Interactions []Interaction `json:"interactions"`
}

// Mode controls whether the recorder records or replays.
type Mode int

const (
	ModeReplay Mode = iota
	ModeRecord
)

// ScrubFunc modifies an interaction before saving (e.g., to remove secrets).
type ScrubFunc func(*Interaction)

// MatchFunc determines whether a live request matches a recorded one.
type MatchFunc func(r *http.Request, rec Request) bool

// Recorder intercepts HTTP calls for recording or replaying.
type Recorder struct {
	mode     Mode
	path     string
	cassette Cassette
	cursor   int
	mu       sync.Mutex
	scrub    ScrubFunc
	match    MatchFunc
}

// DefaultMatch matches on method + URL with API key normalization.
func DefaultMatch(r *http.Request, rec Request) bool {
	return r.Method == rec.Method && stripKey(r.URL.String()) == stripKey(rec.URL)
}

func stripKey(u string) string {
	if idx := strings.Index(u, "key="); idx > 0 {
		end := strings.IndexByte(u[idx:], '&')
		if end == -1 {
			return u[:idx] + "key=X"
		}
		return u[:idx] + "key=X" + u[idx+end:]
	}
	return u
}

// Option configures a Recorder.
type Option func(*Recorder)

// WithScrub sets a function to scrub sensitive data before saving.
func WithScrub(fn ScrubFunc) Option {
	return func(r *Recorder) { r.scrub = fn }
}

// WithMatch sets a custom request matcher.
func WithMatch(fn MatchFunc) Option {
	return func(r *Recorder) { r.match = fn }
}

// New creates a Recorder. Path should omit the extension (.json.gz is appended).
// In replay mode, the cassette is loaded from disk.
// In record mode, interactions are accumulated and saved on Stop().
func New(path string, mode Mode, opts ...Option) (*Recorder, error) {
	r := &Recorder{
		mode:  mode,
		path:  path + ".json.gz",
		match: DefaultMatch,
	}
	for _, opt := range opts {
		opt(r)
	}

	if mode == ModeReplay {
		data, err := readGzip(r.path)
		if err != nil {
			return nil, fmt.Errorf("vcr: load cassette: %w", err)
		}
		if err := json.Unmarshal(data, &r.cassette); err != nil {
			return nil, fmt.Errorf("vcr: parse cassette: %w", err)
		}
	}
	return r, nil
}

// HTTPClient returns an *http.Client that uses this recorder as transport.
func (r *Recorder) HTTPClient() *http.Client {
	return &http.Client{Transport: r}
}

// RoundTrip implements http.RoundTripper.
func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	if r.mode == ModeRecord {
		return r.record(req)
	}
	return r.replay(req)
}

func (r *Recorder) record(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	var reqBody string
	if req.Body != nil {
		rb, _ := io.ReadAll(req.Body)
		reqBody = string(rb)
		req.Body = io.NopCloser(bytes.NewReader(rb))
	}

	ix := Interaction{
		Request: Request{
			Method: req.Method,
			URL:    req.URL.String(),
			Header: scrubHeaders(req.Header),
			Body:   reqBody,
		},
		Response: Response{
			StatusCode: resp.StatusCode,
			Header:     resp.Header.Clone(),
			Body:       string(body),
		},
	}
	if r.scrub != nil {
		r.scrub(&ix)
	}

	r.mu.Lock()
	r.cassette.Interactions = append(r.cassette.Interactions, ix)
	r.mu.Unlock()

	return resp, nil
}

func (r *Recorder) replay(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Sequential matching: try from cursor first, then scan all.
	for i := r.cursor; i < len(r.cassette.Interactions); i++ {
		if r.match(req, r.cassette.Interactions[i].Request) {
			r.cursor = i + 1
			return toHTTPResponse(r.cassette.Interactions[i].Response), nil
		}
	}
	// Fallback: scan from beginning (handles out-of-order replays).
	for i := 0; i < r.cursor && i < len(r.cassette.Interactions); i++ {
		if r.match(req, r.cassette.Interactions[i].Request) {
			return toHTTPResponse(r.cassette.Interactions[i].Response), nil
		}
	}

	return nil, fmt.Errorf("vcr: no matching interaction for %s %s (%d recorded)",
		req.Method, req.URL, len(r.cassette.Interactions))
}

// Stop saves the cassette to disk (record mode only).
func (r *Recorder) Stop() error {
	if r.mode != ModeRecord {
		return nil
	}
	data, err := json.Marshal(r.cassette)
	if err != nil {
		return err
	}
	return writeGzip(r.path, data)
}

func toHTTPResponse(rec Response) *http.Response {
	return &http.Response{
		StatusCode: rec.StatusCode,
		Header:     rec.Header.Clone(),
		Body:       io.NopCloser(strings.NewReader(rec.Body)),
	}
}

func scrubHeaders(h http.Header) http.Header {
	out := h.Clone()
	out.Del("Authorization")
	out.Del("X-Goog-Api-Key")
	return out
}

func readGzip(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gr.Close() }()
	return io.ReadAll(gr)
}

func writeGzip(path string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gw, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := gw.Write(data); err != nil {
		_ = gw.Close()
		return err
	}
	return gw.Close()
}
