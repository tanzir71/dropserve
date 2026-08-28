package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/fcgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tanzir71/dropserve/internal/app"
	phpfastcgi "github.com/tanzir71/dropserve/internal/php"
	"github.com/tanzir71/dropserve/internal/runtimes"
	"github.com/tanzir71/dropserve/internal/scanner"
	dropserver "github.com/tanzir71/dropserve/internal/server"
)

type phpFixtureResponse struct {
	Get    string `json:"get"`
	Post   string `json:"post"`
	Upload struct {
		Name     string `json:"name"`
		Contents string `json:"contents"`
	} `json:"upload"`
	PathInfo string `json:"path_info"`
}

func TestPHPFixtureSupportsGetPostUploadAndPathInfo(t *testing.T) {
	pool, err := phpfastcgi.StartPool(context.Background(), phpfastcgi.PoolOptions{
		Workers: 2,
		Command: func(ctx context.Context, address string) *exec.Cmd {
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=TestPHPWorkerProcess", "--", "-b", address) // #nosec G204,G702 -- test executable and loopback address are controlled by this test.
			command.Env = append(os.Environ(), "DROPSERVE_TEST_PHP_WORKER=1")
			return command
		},
	})
	if err != nil {
		t.Fatalf("start fixture PHP pack: %v", err)
	}
	defer func() {
		if closeErr := pool.Close(); closeErr != nil {
			t.Errorf("close fixture PHP pool: %v", closeErr)
		}
	}()
	if pool.WorkerCount() != 2 {
		t.Fatalf("PHP worker count = %d, want 2", pool.WorkerCount())
	}
	assertPHPFixtureBehavior(t, pool)
}

func TestRealOfficialPHPPackFixture(t *testing.T) {
	if os.Getenv("DROPSERVE_REAL_PHP") != "1" {
		t.Skip("set DROPSERVE_REAL_PHP=1 for the official PHP pack smoke test")
	}
	pack, err := runtimes.CurrentPHPPack()
	if err != nil {
		t.Skip(err)
	}
	runtimeRoot := filepath.Join(t.TempDir(), "runtimes")
	installation, err := (runtimes.Installer{Root: runtimeRoot}).Install(context.Background(), pack)
	if err != nil {
		t.Fatalf("install official PHP pack: %v", err)
	}
	executable := filepath.Join(installation.Path, pack.Executable)
	iniPath := filepath.Join(t.TempDir(), "php.ini")
	if err := phpfastcgi.WriteINI(iniPath, time.Now().Location().String()); err != nil {
		t.Fatalf("generate PHP settings: %v", err)
	}
	pool, err := phpfastcgi.StartPool(context.Background(), phpfastcgi.PoolOptions{
		Executable: executable,
		INIPath:    iniPath,
		Workers:    2,
	})
	if err != nil {
		t.Fatalf("start official PHP pack: %v", err)
	}
	defer func() {
		if closeErr := pool.Close(); closeErr != nil {
			t.Errorf("close official PHP pool: %v", closeErr)
		}
	}()
	assertPHPFixtureBehavior(t, pool)
}

func assertPHPFixtureBehavior(t *testing.T, pool *phpfastcgi.Pool) {
	t.Helper()

	fixture := phpFixtureRoot(t)
	server, err := dropserver.NewWithOptions(dropserver.Options{
		Scanner: scanner.Options{Registered: []string{fixture}},
		PHPHandler: func(application app.App) (http.Handler, error) {
			return pool.Handler(application.Path, application.Slug), nil
		},
	})
	if err != nil {
		t.Fatalf("mount PHP fixture: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			t.Errorf("close PHP fixture server: %v", closeErr)
		}
	}()
	current := server.Scan()
	if len(current.Apps) != 1 || current.Apps[0].Kind != app.KindPHP || current.Apps[0].Runtime != "php" {
		t.Fatalf("PHP fixture detection = %#v", current.Apps)
	}

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	get := requestPHPFixture(t, httpServer.Client(), http.MethodGet, httpServer.URL+"/php/?name=dropserve", "", nil)
	if get.Get != "dropserve" {
		t.Fatalf("GET value = %q, want dropserve", get.Get)
	}

	form := url.Values{"message": {"fastcgi post"}}
	post := requestPHPFixture(t, httpServer.Client(), http.MethodPost, httpServer.URL+"/php/", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if post.Post != "fastcgi post" {
		t.Fatalf("POST value = %q, want fastcgi post", post.Post)
	}

	var uploadBody bytes.Buffer
	upload := multipart.NewWriter(&uploadBody)
	part, err := upload.CreateFormFile("attachment", "hello.txt")
	if err != nil {
		t.Fatalf("create upload part: %v", err)
	}
	if _, err := io.WriteString(part, "hello from upload"); err != nil {
		t.Fatalf("write upload fixture: %v", err)
	}
	if err := upload.Close(); err != nil {
		t.Fatalf("close upload body: %v", err)
	}
	uploaded := requestPHPFixture(t, httpServer.Client(), http.MethodPost, httpServer.URL+"/php/", upload.FormDataContentType(), &uploadBody)
	if uploaded.Upload.Name != "hello.txt" || uploaded.Upload.Contents != "hello from upload" {
		t.Fatalf("upload = %#v", uploaded.Upload)
	}

	pathInfo := requestPHPFixture(t, httpServer.Client(), http.MethodGet, httpServer.URL+"/php/index.php/extra/path", "", nil)
	if pathInfo.PathInfo != "/extra/path" {
		t.Fatalf("PATH_INFO = %q, want /extra/path", pathInfo.PathInfo)
	}
}

func TestPHPWorkerProcess(t *testing.T) {
	if os.Getenv("DROPSERVE_TEST_PHP_WORKER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || len(os.Args) <= separator+2 || os.Args[separator+1] != "-b" {
		t.Fatalf("PHP worker arguments = %q", os.Args)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", os.Args[separator+2])
	if err != nil {
		t.Fatalf("listen for fixture FastCGI worker: %v", err)
	}
	if err := fcgi.Serve(listener, http.HandlerFunc(servePHPFixture)); err != nil {
		t.Fatalf("serve fixture FastCGI worker: %v", err)
	}
}

func servePHPFixture(response http.ResponseWriter, request *http.Request) {
	pathInfo := strings.TrimPrefix(request.URL.Path, "/php/index.php")
	if pathInfo == request.URL.Path {
		pathInfo = ""
	}
	result := phpFixtureResponse{
		Get:      request.URL.Query().Get("name"),
		Post:     request.FormValue("message"),
		PathInfo: pathInfo,
	}
	file, header, err := request.FormFile("attachment")
	if err == nil {
		defer func() {
			_ = file.Close()
		}()
		contents, readErr := io.ReadAll(file)
		if readErr != nil {
			http.Error(response, readErr.Error(), http.StatusInternalServerError)
			return
		}
		result.Upload.Name = header.Filename
		result.Upload.Contents = string(contents)
	}
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(result); err != nil {
		http.Error(response, err.Error(), http.StatusInternalServerError)
	}
}

func requestPHPFixture(t *testing.T, client *http.Client, method, requestURL, contentType string, body io.Reader) phpFixtureResponse {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), method, requestURL, body)
	if err != nil {
		t.Fatalf("create PHP request: %v", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("request PHP fixture: %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read PHP response: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("PHP response = %d, want 200; body=%s", response.StatusCode, contents)
	}
	var result phpFixtureResponse
	if err := json.Unmarshal(contents, &result); err != nil {
		t.Fatalf("decode PHP response %q: %v", contents, err)
	}
	return result
}

func phpFixtureRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate PHP fixture")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "testdata", "fixtures", "php"))
	if _, err := os.Stat(filepath.Join(root, "index.php")); err != nil {
		t.Fatal(fmt.Errorf("locate PHP fixture: %w", err))
	}
	return root
}
