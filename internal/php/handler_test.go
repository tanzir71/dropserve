package php

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/yookoala/gofast"
)

type recordingClient struct {
	parameters map[string]string
}

func (client *recordingClient) Do(request *gofast.Request) (*gofast.ResponsePipe, error) {
	client.parameters = make(map[string]string, len(request.Params))
	for name, value := range request.Params {
		client.parameters[name] = value
	}
	response := gofast.NewResponsePipe()
	response.Close()
	return response, nil
}

func (*recordingClient) Close() error { return nil }

func TestFastCGIParametersKeepDropservePrefix(t *testing.T) {
	documentRoot := t.TempDir()
	request := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://dropserve.test/php/index.php/extra/path?name=dropserve",
		nil,
	)
	// The first-segment router removes /php before dispatch but deliberately
	// preserves RequestURI so the FastCGI boundary can reconstruct public paths.
	request.URL.Path = "/index.php/extra/path"
	request.RequestURI = "/php/index.php/extra/path?name=dropserve"
	client := &recordingClient{}
	response, err := newSession(documentRoot, "php")(client, gofast.NewRequest(request))
	if err != nil {
		t.Fatalf("map FastCGI request: %v", err)
	}
	response.Close()

	want := map[string]string{
		"SCRIPT_FILENAME": filepath.Join(documentRoot, "index.php"),
		"DOCUMENT_ROOT":   documentRoot,
		"REQUEST_URI":     "/php/index.php/extra/path?name=dropserve",
		"SCRIPT_NAME":     "/php/index.php",
		"PATH_INFO":       "/extra/path",
	}
	got := make(map[string]string, len(want))
	for name := range want {
		got[name] = client.parameters[name]
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FastCGI parameters = %#v, want %#v", got, want)
	}
}
