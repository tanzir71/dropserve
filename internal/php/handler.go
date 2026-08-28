// Package php serves PHP applications through a FastCGI runtime.
package php

import (
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
	return gofast.NewHandler(
		newSession(documentRoot, slug),
		gofast.SimpleClientFactory(connectionFactory),
	)
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
