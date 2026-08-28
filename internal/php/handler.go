// Package php serves PHP applications through a FastCGI runtime.
package php

import (
	"bytes"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/yookoala/gofast"
)

// New creates a file-routed PHP FastCGI handler. The FastCGI process is owned
// by the runtime pool; this handler only performs protocol proxying.
func New(documentRoot, slug, network, address string) http.Handler {
	connectionFactory := gofast.SimpleConnFactory(network, address)
	handler := gofast.NewHandler(
		newSession(documentRoot, slug),
		gofast.SimpleClientFactory(connectionFactory),
	)
	return fatalAwareHandler{next: handler}
}

const fatalInspectionLimit = 256 << 10

type fatalAwareHandler struct {
	next http.Handler
}

func (handler fatalAwareHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	buffered := &phpResponseWriter{
		target: response,
		header: make(http.Header),
	}
	handler.next.ServeHTTP(buffered, request)
	buffered.finish()
}

type phpResponseWriter struct {
	target      http.ResponseWriter
	header      http.Header
	status      int
	body        bytes.Buffer
	passthrough bool
}

func (writer *phpResponseWriter) Header() http.Header { return writer.header }

func (writer *phpResponseWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}

func (writer *phpResponseWriter) Write(content []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	if writer.passthrough {
		return writer.target.Write(content)
	}
	if writer.body.Len()+len(content) <= fatalInspectionLimit {
		return writer.body.Write(content)
	}
	writer.publish(writer.status)
	writer.passthrough = true
	return writer.target.Write(content)
}

func (writer *phpResponseWriter) finish() {
	if writer.passthrough {
		return
	}
	status := writer.status
	if status == 0 {
		status = http.StatusOK
	}
	if status < http.StatusInternalServerError && isFatalPHP(writer.body.Bytes()) {
		status = http.StatusInternalServerError
		writer.header.Set("X-Dropserve-PHP-Error", "fatal")
	}
	writer.publish(status)
}

func (writer *phpResponseWriter) publish(status int) {
	for name, values := range writer.header {
		for _, value := range values {
			writer.target.Header().Add(name, value)
		}
	}
	writer.target.WriteHeader(status)
	if writer.body.Len() != 0 {
		_, _ = writer.target.Write(writer.body.Bytes())
		writer.body.Reset()
	}
}

func isFatalPHP(content []byte) bool {
	return bytes.Contains(content, []byte("Fatal error")) &&
		(bytes.Contains(content, []byte("Stack trace:")) || bytes.Contains(content, []byte("Uncaught ")))
}

func newSession(documentRoot, slug string) gofast.SessionHandler {
	return gofast.Chain(
		gofast.BasicParamsMap,
		gofast.MapHeader,
		mapPHPPaths(documentRoot, slug),
	)(gofast.BasicSession)
}

func mapPHPPaths(documentRoot, slug string) gofast.Middleware {
	root := filepath.Clean(documentRoot)
	prefix := "/" + strings.Trim(slug, "/")
	return func(inner gofast.SessionHandler) gofast.SessionHandler {
		return func(client gofast.Client, request *gofast.Request) (*gofast.ResponsePipe, error) {
			localPath := path.Clean("/" + strings.TrimPrefix(request.Raw.URL.Path, "/"))
			if strings.HasSuffix(request.Raw.URL.Path, "/") {
				localPath = path.Join(localPath, "index.php")
			}
			scriptPath, pathInfo := splitPHPPath(localPath)
			filename := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(scriptPath, "/")))
			relative, err := filepath.Rel(root, filename)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("PHP script path escapes the app root")
			}

			request.Params["PATH_INFO"] = pathInfo
			request.Params["PATH_TRANSLATED"] = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(pathInfo, "/")))
			request.Params["SCRIPT_NAME"] = prefix + scriptPath
			request.Params["SCRIPT_FILENAME"] = filename
			request.Params["DOCUMENT_URI"] = prefix + request.Raw.URL.Path
			request.Params["DOCUMENT_ROOT"] = root
			return inner(client, request)
		}
	}
}

func splitPHPPath(requestPath string) (string, string) {
	lower := strings.ToLower(requestPath)
	for offset := 0; ; {
		index := strings.Index(lower[offset:], ".php")
		if index < 0 {
			return requestPath, ""
		}
		end := offset + index + len(".php")
		if end == len(requestPath) || requestPath[end] == '/' {
			return requestPath[:end], requestPath[end:]
		}
		offset = end
	}
}
