// Package php serves PHP applications through a FastCGI runtime.
package php

import (
	"net/http"

	"github.com/yookoala/gofast"
)

// New creates a file-routed PHP FastCGI handler. The FastCGI process is owned
// by the runtime pool; this handler only performs protocol proxying.
func New(documentRoot, network, address string) http.Handler {
	connectionFactory := gofast.SimpleConnFactory(network, address)
	return gofast.NewHandler(
		gofast.NewPHPFS(documentRoot)(gofast.BasicSession),
		gofast.SimpleClientFactory(connectionFactory),
	)
}
