/*
 * Copyright 2025 The Go-Spring Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package httpsvr

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/lvan100/golib/errutil"
	"github.com/lvan100/golib/jsonflow"
)

// ErrorHandler is the default handler for reporting errors back to the client.
// By default, it responds with an HTTP 500 status and the error message.
// Users should implement their own error handling logic.
var ErrorHandler = func(r *http.Request, w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

type ctxKeyType struct{}

var ctxKey ctxKeyType

// httpReqResp wraps both *http.Request and http.ResponseWriter.
type httpReqResp struct {
	r *http.Request
	w http.ResponseWriter
}

// getHTTPReqResp retrieves the httpReqResp wrapper from the context.
func getHTTPReqResp(ctx context.Context) httpReqResp {
	req, _ := ctx.Value(&ctxKey).(httpReqResp)
	return req
}

// setHTTPReqResp stores the http.Request and http.ResponseWriter in the context.
func setHTTPReqResp(ctx context.Context, r *http.Request, w http.ResponseWriter) context.Context {
	return context.WithValue(ctx, &ctxKey, httpReqResp{r, w})
}

// GetReq retrieves the *http.Request from the context if available.
func GetReq(ctx context.Context) *http.Request {
	return getHTTPReqResp(ctx).r
}

// GetHeader retrieves a specific HTTP request header by key from the context.
func GetHeader(ctx context.Context, key string) string {
	if r := getHTTPReqResp(ctx).r; r != nil {
		return r.Header.Get(key)
	}
	return ""
}

// SetCode sets the HTTP response status code in the context.
func SetCode(ctx context.Context, httpCode int) {
	if w := getHTTPReqResp(ctx).w; w != nil {
		w.WriteHeader(httpCode)
	}
}

// SetHeader sets a response header key/value pair in the context.
func SetHeader(ctx context.Context, key, value string) {
	if w := getHTTPReqResp(ctx).w; w != nil {
		w.Header().Set(key, value)
	}
}

// SetCookie adds a Set-Cookie header to the HTTP response in the context.
func SetCookie(ctx context.Context, cookie *http.Cookie) {
	if cookie != nil {
		if w := getHTTPReqResp(ctx).w; w != nil {
			http.SetCookie(w, cookie)
		}
	}
}

// Router defines a single route with HTTP method, pattern, and handler.
type Router struct {
	Method  string
	Pattern string
	Handler http.HandlerFunc
}

// Server is an interface that defines a method to register routes.
type Server interface {
	Route(r Router)
}

// SimpleServer defines a basic HTTP server with an internal multiplexer.
type SimpleServer struct {
	*http.Server
	mux *http.ServeMux
}

// NewSimpleServer creates a new SimpleServer instance with the specified address.
func NewSimpleServer(addr string) *SimpleServer {
	mux := http.NewServeMux()
	svr := &http.Server{Addr: addr, Handler: mux}
	return &SimpleServer{Server: svr, mux: mux}
}

// Route registers a new route in the SimpleServer with the provided router.
func (s *SimpleServer) Route(r Router) {
	s.mux.HandleFunc(strings.TrimSpace(r.Method+" "+r.Pattern), r.Handler)
}

// RequestObject defines the interface that all request types must implement.
type RequestObject interface {
	// Bind binds query/path parameters to the struct.
	Bind(*http.Request) error
	// Validate validates the parameters.
	Validate() error
}

// shouldParseBody determines whether the incoming HTTP method
// is expected to carry a request body that should be parsed.
func shouldParseBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

// ReadBody reads the request body into a byte slice.
// Users can customize the ReadBody function.
var ReadBody = func(r *http.Request) ([]byte, error) {
	const maxBodySize = int64(10 << 20) // 10 MB is a lot of text
	defer func() { _ = r.Body.Close() }()

	reader := io.LimitReader(r.Body, maxBodySize+1)
	b, err := io.ReadAll(reader)
	if err != nil {
		return nil, errutil.Explain(err, "read body error")
	}
	if int64(len(b)) > maxBodySize {
		return nil, errutil.Explain(nil, "body too large")
	}
	return b, nil
}

// decodeBody reads and decodes the request body into the given
// RequestObject based on Content-Type.
func decodeBody(r *http.Request, i RequestObject) error {

	b, err := ReadBody(r)
	if err != nil {
		return err
	}

	contentType := r.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)

	var asJSON bool
	switch mediaType {
	case "application/json":
		asJSON = true
	case "application/x-www-form-urlencoded":
		asJSON = false
	default:
		if b = bytes.TrimSpace(b); len(b) > 0 {
			if b[0] == '{' || b[0] == '[' { // Looks like JSON
				asJSON = true
			} else {
				asJSON = false
			}
		}
	}

	if b = bytes.TrimSpace(b); len(b) > 0 {
		if asJSON {
			d := jsonflow.NewDecoder(bytes.NewReader(b))
			v, ok := i.(interface {
				DecodeJSON(d jsonflow.Decoder) error
			})
			if !ok {
				return errutil.Explain(nil, "decode form error: not a DecodeJSON implementer")
			}
			if err = v.DecodeJSON(d); err != nil {
				return errutil.Explain(err, "json decode error")
			}
		} else {
			v, ok := i.(interface{ DecodeForm(b []byte) error })
			if !ok {
				return errutil.Explain(nil, "decode form error: not a DecodeForm implementer")
			}
			if err = v.DecodeForm(b); err != nil {
				return errutil.Explain(err, "decode form error")
			}
		}
	}

	return nil
}

// ReadRequest parses the request body based on Content-Type and
// decodes it into the given RequestObject.
func ReadRequest(r *http.Request, i RequestObject) error {

	// Only parse body for methods that typically include a body
	if shouldParseBody(r.Method) {
		if err := decodeBody(r, i); err != nil {
			return err
		}
	}

	// Bind fields
	if err := i.Bind(r); err != nil {
		return errutil.Explain(err, "bind fields error")
	}

	// Validate fields
	if err := i.Validate(); err != nil {
		return errutil.Explain(err, "validate error")
	}
	return nil
}

type JSONHandler[Req any, Resp any] func(context.Context, Req) Resp

// HandleJSON wraps a JSONHandler into an http.HandlerFunc to handle JSON requests.
func HandleJSON[Req RequestObject, Resp any](w http.ResponseWriter, r *http.Request,
	req Req, h JSONHandler[Req, Resp]) {

	if err := ReadRequest(r, req); err != nil {
		ErrorHandler(r, w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	ctx := setHTTPReqResp(r.Context(), r, w)
	resp := h(ctx, req)

	if err := jsonflow.MarshalWrite(w, resp); err != nil {
		ErrorHandler(r, w, err)
	}
}

// Event represents an SSE (Server-Sent Event) with optional
// ID, event type, data, and retry interval.
type Event[T any] struct {
	id    *string
	event *string
	data  T
	retry *int
}

// NewEvent creates a new Event instance.
func NewEvent[T any]() *Event[T] {
	return &Event[T]{}
}

// ID sets the ID of the SSE event.
func (e *Event[T]) ID(id string) *Event[T] {
	e.id = &id
	return e
}

// Event sets the event type of the SSE event.
func (e *Event[T]) Event(event string) *Event[T] {
	e.event = &event
	return e
}

// Data sets the data of the SSE event.
func (e *Event[T]) Data(data T) *Event[T] {
	e.data = data
	return e
}

// Retry sets the retry interval of the SSE event.
func (e *Event[T]) Retry(retry int) *Event[T] {
	e.retry = &retry
	return e
}

// HasID returns true if the SSE event has an ID.
func (e *Event[T]) HasID() bool {
	return e.id != nil
}

// GetID returns the ID of the SSE event.
func (e *Event[T]) GetID() string {
	return *e.id
}

// HasEvent returns true if the SSE event has an event type.
func (e *Event[T]) HasEvent() bool {
	return e.event != nil
}

// GetEvent returns the event type of the SSE event.
func (e *Event[T]) GetEvent() string {
	return *e.event
}

// GetData returns the data of the SSE event.
func (e *Event[T]) GetData() any {
	return e.data
}

// HasRetry returns true if the SSE event has a retry interval.
func (e *Event[T]) HasRetry() bool {
	return e.retry != nil
}

// GetRetry returns the retry interval of the SSE event.
func (e *Event[T]) GetRetry() int {
	return *e.retry
}

type StreamHandler[Req any, Resp any] func(context.Context, Req, chan<- Resp)

// HandleStream wraps a StreamHandler into an http.HandlerFunc to
// handle streaming requests using Server-Sent Events (SSE).
func HandleStream[Req RequestObject, Resp *Event[T], T any](w http.ResponseWriter,
	r *http.Request, req Req, h StreamHandler[Req, Resp]) {

	// Ensure the response writer supports flushing (required for SSE)
	flusher, ok := w.(http.Flusher)
	if !ok {
		err := errutil.Explain(nil, "streaming not supported")
		ErrorHandler(r, w, err)
		return
	}

	if err := ReadRequest(r, req); err != nil {
		ErrorHandler(r, w, err)
		return
	}

	done := make(chan struct{})
	responses := make(chan Resp)

	go func() {
		defer close(done)
		var res *Event[T]
		for res = range responses {

			select {
			case <-r.Context().Done():
				return
			default: // for linter
			}

			// Write SSE event
			if res.HasID() {
				if _, err := w.Write([]byte("id: " + res.GetID() + "\n")); err != nil {
					ErrorHandler(r, w, err)
					continue
				}
			}

			// Write SSE event
			if res.HasEvent() {
				if _, err := w.Write([]byte("event: " + res.GetEvent() + "\n")); err != nil {
					ErrorHandler(r, w, err)
					continue
				}
			}

			// Write SSE event
			if _, err := w.Write([]byte("data: ")); err != nil {
				ErrorHandler(r, w, err)
				continue
			}
			if err := jsonflow.MarshalWrite(w, res.GetData()); err != nil {
				ErrorHandler(r, w, err)
				continue
			}
			if _, err := w.Write([]byte("\n")); err != nil {
				ErrorHandler(r, w, err)
				continue
			}

			// Write SSE event
			if res.HasRetry() {
				if _, err := w.Write([]byte("retry: " + strconv.Itoa(res.GetRetry()) + "\n")); err != nil {
					ErrorHandler(r, w, err)
					continue
				}
			}
			flusher.Flush()
		}
	}()

	// Set response headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := setHTTPReqResp(r.Context(), r, w)
	h(ctx, req, responses)
	close(responses)
	<-done
}
