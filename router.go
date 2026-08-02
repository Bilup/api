package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	tr "time"
)

// HandlerFunc mirrors an OSL *serve.Context handler.
type HandlerFunc func(c *Context)

// Router is a small Gin-like router on net/http ServeMux (Go 1.22+ method +
// "{param}" patterns). It mirrors the OSL `serve` package used by main.osl.
type Router struct {
	mux        *http.ServeMux
	middleware []HandlerFunc
	prefix     string
	maxBody    int64
}

func newRouter() *Router { return &Router{mux: http.NewServeMux(), maxBody: 32 << 20} }

func (rt *Router) use(h ...HandlerFunc) *Router {
	rt.middleware = append(rt.middleware, h...)
	return rt
}

func (rt *Router) group(prefix string) *Router {
	return &Router{
		mux:        rt.mux,
		prefix:     rt.prefix + prefix,
		middleware: append([]HandlerFunc{}, rt.middleware...),
		maxBody:    rt.maxBody,
	}
}

func (rt *Router) bodyLimit(maxBytes int64) *Router {
	rt.maxBody = maxBytes
	return rt
}

// Context mirrors OSL *serve.Context. Handlers call c.next() to advance the
// middleware+handler chain; returning without next() ends the chain.
type Context struct {
	w        http.ResponseWriter
	r        *http.Request
	keys     map[string]any
	bodyCache []byte
	bodyRead bool
	chain    []HandlerFunc
	index    int
	aborted  bool
}

func (rt *Router) buildContext(w http.ResponseWriter, r *http.Request) *Context {
	if rt.maxBody > 0 && r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, rt.maxBody)
	}
	return &Context{w: w, r: r}
}

func (c *Context) next() {
	if c.aborted {
		return
	}
	c.index++
	if c.index >= len(c.chain) {
		return
	}
	c.chain[c.index](c)
}

func (c *Context) readBody() []byte {
	if !c.bodyRead {
		c.bodyRead = true
		data, err := io.ReadAll(c.r.Body)
		if err != nil {
			c.bodyCache = []byte{}
		} else {
			c.bodyCache = data
		}
	}
	return c.bodyCache
}

func (c *Context) method() string { return c.r.Method }
func (c *Context) path() string   { return c.r.URL.Path }
func (c *Context) contentType() string {
	return strings.TrimSpace(strings.Split(c.r.Header.Get("Content-Type"), ";")[0])
}

func (c *Context) set(key string, v any) {
	if c.keys == nil {
		c.keys = map[string]any{}
	}
	c.keys[key] = v
}
func (c *Context) get(key string) any {
	if c.keys == nil {
		return nil
	}
	return c.keys[key]
}
func (c *Context) ip() string {
	host, _, err := net.SplitHostPort(c.r.RemoteAddr)
	if err == nil {
		return host
	}
	return c.r.RemoteAddr
}

func (c *Context) getString(key string) string {
	if v, ok := c.get(key).(string); ok {
		return v
	}
	return ""
}
func (c *Context) getBool(key string) bool {
	if v, ok := c.get(key).(bool); ok {
		return v
	}
	return false
}
func (c *Context) getInt(key string) int {
	switch v := c.get(key).(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
	}
	return 0
}

func (c *Context) status(code int) { c.w.WriteHeader(code) }

func (c *Context) json(code int, obj any) {
	writeAll(c.w, code, "application/json; charset=utf-8", jsonMust(obj))
}
func (c *Context) ok(obj any)      { c.json(200, obj) }
func (c *Context) created(obj any) { c.json(201, obj) }
func (c *Context) string(code int, body string) {
	writeAll(c.w, code, "text/plain; charset=utf-8", []byte(body))
}
func (c *Context) text(code int, body string) {
	writeAll(c.w, code, "text/plain; charset=utf-8", []byte(body))
}
func (c *Context) noContent()               { c.w.WriteHeader(204) }
func (c *Context) data(code int, ct string, b []byte) { writeAll(c.w, code, ct, b) }
func (c *Context) send(code int, ct, body string)     { writeAll(c.w, code, ct, []byte(body)) }
func (c *Context) redirect(code int, url string)      { http.Redirect(c.w, c.r, url, code) }
func (c *Context) file(path string)                   { http.ServeFile(c.w, c.r, path) }

func (c *Context) abort(values ...any) {
	c.aborted = true
	if c.written() {
		return
	}
	if len(values) == 0 {
		return
	}
	code := 500
	switch v := values[0].(type) {
	case int:
		code = v
	case int64:
		code = int(v)
	case float64:
		code = int(v)
	case string:
		if n, err := parseInt(v); err == nil {
			code = n
		}
	}
	if code <= 0 {
		code = 500
	}
	message := ""
	if len(values) > 1 {
		message = toString(values[1])
	}
	if message == "" {
		c.w.WriteHeader(code)
		return
	}
	c.json(code, map[string]any{"error": message})
}
func (c *Context) badRequest(m string)    { c.abort(400, m) }
func (c *Context) unauthorized(m string)   { c.abort(401, m) }
func (c *Context) forbidden(m string)      { c.abort(403, m) }
func (c *Context) notFound(m string)       { c.abort(404, m) }
func (c *Context) internalError(m string)  { c.abort(500, m) }

func (c *Context) query(key, def string) string {
	if v := c.r.URL.Query().Get(key); v != "" {
		return v
	}
	return def
}
func (c *Context) queryDefault(key, def string) string {
	if v := c.r.URL.Query().Get(key); v != "" {
		return v
	}
	return def
}
func (c *Context) queryInt(key string, def int) int {
	if v := c.r.URL.Query().Get(key); v != "" {
		if n, err := parseInt(v); err == nil {
			return n
		}
	}
	return def
}
func (c *Context) queryBool(key string, def bool) bool {
	if v := c.r.URL.Query().Get(key); v != "" {
		if b, err := parseBool(v); err == nil {
			return b
		}
	}
	return def
}
func (c *Context) queryArray(key string) []string { return c.r.URL.Query()[key] }

func (c *Context) param(key string) string { return c.r.PathValue(key) }
func (c *Context) paramInt(key string, def int) int {
	if v := c.r.PathValue(key); v != "" {
		if n, err := parseInt(v); err == nil {
			return n
		}
	}
	return def
}

func (c *Context) headerVal(key string) string { return c.r.Header.Get(key) }
func (c *Context) setHeader(key, value string) { c.w.Header().Set(key, value) }
func (c *Context) addHeader(key, value string) { c.w.Header().Add(key, value) }
func (c *Context) hasHeader(key, value string) bool {
	return strings.EqualFold(c.r.Header.Get(key), value)
}

func (c *Context) bearer() string {
	auth := c.r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

func (c *Context) body() string      { return string(c.readBody()) }
func (c *Context) bodyBytes() []byte { return c.readBody() }
func (c *Context) bodyJSON() map[string]any {
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(string(c.readBody())))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return map[string]any{}
	}
	return obj
}
func (c *Context) bodyJSONArray() []any {
	var arr []any
	dec := json.NewDecoder(strings.NewReader(string(c.readBody())))
	dec.UseNumber()
	if err := dec.Decode(&arr); err != nil {
		return []any{}
	}
	return arr
}

func (c *Context) formValue(key string) string { return c.r.FormValue(key) }
func (c *Context) formValueDefault(key, def string) string {
	if v := c.r.FormValue(key); v != "" {
		return v
	}
	return def
}

type formFile struct {
	Data     []byte
	Filename string
	Size     int64
}

func (c *Context) formFile(key string) (formFile, bool) {
	f, h, err := c.r.FormFile(key)
	if err != nil {
		return formFile{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return formFile{}, false
	}
	return formFile{Data: data, Filename: h.Filename, Size: h.Size}, true
}

func (c *Context) cookie(name string) string {
	if ck, err := c.r.Cookie(name); err == nil {
		return ck.Value
	}
	return ""
}
func (c *Context) setCookie(name, value string, maxAge int, path, domain string, secure, httpOnly bool) {
	if path == "" {
		path = "/"
	}
	http.SetCookie(c.w, &http.Cookie{
		Name: name, Value: value, MaxAge: maxAge, Path: path, Domain: domain,
		Secure: secure, HttpOnly: httpOnly, SameSite: http.SameSiteLaxMode,
	})
}
func (c *Context) clearCookie(name string) {
	http.SetCookie(c.w, &http.Cookie{Name: name, Value: "", MaxAge: -1, Path: "/", Expires: tr.Unix(0, 0)})
}

func (c *Context) remoteIP() string {
	if fwd := c.r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	if real := c.r.Header.Get("X-Real-IP"); real != "" {
		return strings.TrimSpace(real)
	}
	host, _, err := splitHostPort(c.r.RemoteAddr)
	if err == nil {
		return host
	}
	return c.r.RemoteAddr
}

func (c *Context) userAgent() string { return c.r.Header.Get("User-Agent") }
func (c *Context) request() *http.Request   { return c.r }
func (c *Context) writer() http.ResponseWriter { return c.w }
func (c *Context) written() bool {
	if rw, ok := c.w.(*responseWriter); ok {
		return rw.written
	}
	// Detect writes via the wrapped writer if used.
	return false
}

type responseWriter struct {
	http.ResponseWriter
	status  int
	size    int
	written bool
}

func (w *responseWriter) WriteHeader(code int) {
	if !w.written {
		w.status = code
		w.written = true
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.status = 200
		w.written = true
		w.ResponseWriter.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(b)
	w.size += n
	return n, err
}

func writeAll(w http.ResponseWriter, status int, ct string, body []byte) {
	if ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(status)
	w.Write(body)
}

// handle registers a method+route and runs the middleware+handler chain.
func (rt *Router) handle(method, pattern string, handlers ...HandlerFunc) {
	full := rt.prefix + pattern
	chain := make([]HandlerFunc, 0, len(rt.middleware)+len(handlers))
	chain = append(chain, rt.middleware...)
	chain = append(chain, handlers...)
	rt.mux.HandleFunc(method+" "+full, func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if r.MultipartForm != nil {
				r.MultipartForm.RemoveAll()
			}
		}()
		rw := &responseWriter{ResponseWriter: w, status: 200}
		ctx := rt.buildContext(rw, r)
		ctx.chain = chain
		ctx.index = 0
		ctx.chain[0](ctx)
	})
}

func (rt *Router) GET(pattern string, handlers ...HandlerFunc)    { rt.handle("GET", pattern, handlers...) }
func (rt *Router) POST(pattern string, handlers ...HandlerFunc)   { rt.handle("POST", pattern, handlers...) }
func (rt *Router) PUT(pattern string, handlers ...HandlerFunc)    { rt.handle("PUT", pattern, handlers...) }
func (rt *Router) DELETE(pattern string, handlers ...HandlerFunc) { rt.handle("DELETE", pattern, handlers...) }
func (rt *Router) PATCH(pattern string, handlers ...HandlerFunc)  { rt.handle("PATCH", pattern, handlers...) }
func (rt *Router) ANY(pattern string, handlers ...HandlerFunc) {
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"} {
		rt.handle(m, pattern, handlers...)
	}
}

func (rt *Router) static(prefix, dir string) {
	full := rt.prefix + prefix
	fs := http.StripPrefix(full, http.FileServer(http.Dir(dir)))
	chain := make([]HandlerFunc, 0, len(rt.middleware)+1)
	chain = append(chain, rt.middleware...)
	chain = append(chain, func(c *Context) { fs.ServeHTTP(c.w, c.r) })
	rt.mux.HandleFunc(full+"/", func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriter{ResponseWriter: w, status: 200}
		ctx := rt.buildContext(rw, r)
		ctx.chain = chain
		ctx.index = 0
		ctx.chain[0](ctx)
	})
}

func (rt *Router) serve(addr string) error {
	return (&http.Server{
		Addr:              addr,
		Handler:           rt.mux,
		ReadHeaderTimeout: 10 * tr.Second,
		IdleTimeout:       120 * tr.Second,
	}).ListenAndServe()
}

func maxBodySize(mb int64) HandlerFunc {
	return func(c *Context) {
		c.r.Body = http.MaxBytesReader(c.w, c.r.Body, mb)
		c.next()
	}
}

func loggerMiddleware() HandlerFunc {
	return func(c *Context) {
		start := tr.Now()
		c.next()
		fmt.Printf("[%s] %s %s %d %d %s\n",
			tr.Now().Format("2006-01-02 15:04:05"),
			c.method(), c.path(),
			statusOf(c), sizeOf(c), tr.Since(start).String(),
		)
	}
}

func statusOf(c *Context) int {
	if rw, ok := c.w.(*responseWriter); ok {
		return rw.status
	}
	return 200
}
func sizeOf(c *Context) int {
	if rw, ok := c.w.(*responseWriter); ok {
		return rw.size
	}
	return 0
}