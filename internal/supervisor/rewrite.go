package supervisor

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

const maximumHTMLRewriteBytes = 2 << 20

func rewriteHTMLResponse(response *http.Response, prefix, mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "never" {
		return nil
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "text/html") {
		return nil
	}
	if encoding := strings.TrimSpace(response.Header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return nil
	}
	if response.Request != nil && response.Request.Method == http.MethodHead {
		return nil
	}
	if response.Body == nil || response.ContentLength > maximumHTMLRewriteBytes {
		return nil
	}
	body := response.Body
	content, err := io.ReadAll(io.LimitReader(body, maximumHTMLRewriteBytes+1))
	if err != nil {
		_ = body.Close()
		return err
	}
	if len(content) > maximumHTMLRewriteBytes {
		response.Body = &prefixedReadCloser{
			Reader: io.MultiReader(bytes.NewReader(content), body),
			Closer: body,
		}
		return nil
	}
	if err := body.Close(); err != nil {
		return err
	}
	rewritten, changed := injectBaseElement(content, prefix, mode == "always")
	response.Body = io.NopCloser(bytes.NewReader(rewritten))
	if !changed {
		return nil
	}
	response.ContentLength = int64(len(rewritten))
	response.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
	response.Header.Del("ETag")
	response.Header.Del("Content-MD5")
	response.Header.Del("Accept-Ranges")
	return nil
}

func injectBaseElement(document []byte, prefix string, always bool) ([]byte, bool) {
	lower := bytes.ToLower(document)
	if _, found := openingTagEnd(lower, "base"); found && !always {
		return document, false
	}
	headEnd, found := openingTagEnd(lower, "head")
	if !found {
		return document, false
	}
	base := []byte(`<base href="` + prefix + `/">`)
	rewritten := make([]byte, 0, len(document)+len(base))
	rewritten = append(rewritten, document[:headEnd]...)
	rewritten = append(rewritten, base...)
	rewritten = append(rewritten, document[headEnd:]...)
	return rewritten, true
}

func openingTagEnd(document []byte, name string) (int, bool) {
	needle := []byte("<" + name)
	searchFrom := 0
	for searchFrom < len(document) {
		index := bytes.Index(document[searchFrom:], needle)
		if index < 0 {
			return 0, false
		}
		index += searchFrom
		afterName := index + len(needle)
		if afterName < len(document) && document[afterName] != '>' &&
			document[afterName] != ' ' && document[afterName] != '\t' &&
			document[afterName] != '\r' && document[afterName] != '\n' {
			searchFrom = afterName
			continue
		}
		end := bytes.IndexByte(document[afterName:], '>')
		if end < 0 {
			return 0, false
		}
		return afterName + end + 1, true
	}
	return 0, false
}

type prefixedReadCloser struct {
	io.Reader
	io.Closer
}
