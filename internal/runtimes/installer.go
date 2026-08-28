// Package runtimes downloads, verifies, unpacks, and removes optional runtime packs.
package runtimes

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Format names the supported runtime archive container.
type Format string

const (
	// FormatZIP is used by official Windows runtime packs.
	FormatZIP Format = "zip"
	// FormatTarGZ is used by official Unix runtime packs.
	FormatTarGZ Format = "tar.gz"
	// FormatFile registers one verified standalone executable or asset.
	FormatFile Format = "file"
)

// Pack is one immutable, platform-specific runtime artifact.
type Pack struct {
	Name       string
	Version    string
	OS         string
	Arch       string
	URL        string
	SHA256     string
	Format     Format
	Executable string
}

// Progress describes verified download progress without making the HTTP body public.
type Progress struct {
	Downloaded int64
	Total      int64
}

// Installation identifies one atomically registered pack.
type Installation struct {
	Pack Pack
	Path string
}

// Installer owns runtime files below Root. It never writes inside an app root.
type Installer struct {
	Root     string
	Client   *http.Client
	Progress func(Progress)
}

// Install downloads a pinned pack, rejects and deletes any checksum mismatch,
// unpacks into a private staging directory, and atomically registers it.
func (installer Installer) Install(ctx context.Context, pack Pack) (Installation, error) {
	if err := validatePack(pack); err != nil {
		return Installation{}, err
	}
	expected, err := hex.DecodeString(pack.SHA256)
	if err != nil || len(expected) != sha256.Size {
		return Installation{}, fmt.Errorf("runtime pack %s has an invalid pinned SHA-256", pack.Name)
	}
	if err := os.MkdirAll(installer.Root, 0o700); err != nil {
		return Installation{}, fmt.Errorf("create runtime directory: %w", err)
	}
	staging, err := os.MkdirTemp(installer.Root, "."+pack.Name+"-install-")
	if err != nil {
		return Installation{}, fmt.Errorf("create %s staging directory: %w", pack.Name, err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	payloadPath := filepath.Join(staging, "download")
	actual, err := installer.download(ctx, pack, payloadPath)
	if err != nil {
		return Installation{}, err
	}
	if !bytes.Equal(actual, expected) {
		return Installation{}, fmt.Errorf(
			"runtime pack %s failed SHA-256 verification: expected %s, downloaded %s; the tampered download was deleted",
			pack.Name,
			hex.EncodeToString(expected),
			hex.EncodeToString(actual),
		)
	}
	contentPath := filepath.Join(staging, "content")
	if err := os.Mkdir(contentPath, 0o700); err != nil {
		return Installation{}, fmt.Errorf("create %s unpack directory: %w", pack.Name, err)
	}
	if err := unpack(pack.Format, payloadPath, contentPath, pack.Executable); err != nil {
		return Installation{}, fmt.Errorf("unpack runtime pack %s: %w", pack.Name, err)
	}
	destination := filepath.Join(installer.Root, pack.Name, pack.Version, pack.OS+"-"+pack.Arch)
	if _, err := os.Stat(destination); err == nil {
		return Installation{Pack: pack, Path: destination}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Installation{}, fmt.Errorf("inspect runtime destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return Installation{}, fmt.Errorf("create runtime registration directory: %w", err)
	}
	if err := os.Rename(contentPath, destination); err != nil {
		return Installation{}, fmt.Errorf("register runtime pack %s: %w", pack.Name, err)
	}
	return Installation{Pack: pack, Path: destination}, nil
}

// Remove deletes only one registered runtime artifact. App files are outside
// this boundary and are never accepted as a removal target.
func (installer Installer) Remove(pack Pack) error {
	if err := validatePackIdentity(pack); err != nil {
		return err
	}
	root, err := filepath.Abs(installer.Root)
	if err != nil {
		return fmt.Errorf("resolve runtime directory: %w", err)
	}
	target, err := filepath.Abs(filepath.Join(root, pack.Name, pack.Version, pack.OS+"-"+pack.Arch))
	if err != nil {
		return fmt.Errorf("resolve runtime pack %s: %w", pack.Name, err)
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refuse to remove runtime pack %s outside the runtime directory", pack.Name)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove runtime pack %s: %w", pack.Name, err)
	}
	// Prune only empty registration parents; other versions and platforms remain.
	_ = os.Remove(filepath.Dir(target))
	_ = os.Remove(filepath.Dir(filepath.Dir(target)))
	return nil
}

func (installer Installer) download(ctx context.Context, pack Pack, path string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pack.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("create %s download request: %w", pack.Name, err)
	}
	client := installer.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request) // #nosec G107 -- URL is selected from Dropserve's pinned runtime manifest.
	if err != nil {
		return nil, fmt.Errorf("download runtime pack %s: %w", pack.Name, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("download runtime pack %s: server returned %s", pack.Name, response.Status)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- path is the installer's private staging filename.
	if err != nil {
		return nil, fmt.Errorf("create %s download: %w", pack.Name, err)
	}
	hash := sha256.New()
	reader := io.Reader(response.Body)
	if installer.Progress != nil {
		reader = &progressReader{reader: response.Body, total: response.ContentLength, report: installer.Progress}
	}
	_, copyErr := io.Copy(io.MultiWriter(file, hash), reader)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		return nil, fmt.Errorf("save runtime pack %s: %w", pack.Name, err)
	}
	return hash.Sum(nil), nil
}

func validatePack(pack Pack) error {
	if err := validatePackIdentity(pack); err != nil {
		return err
	}
	if pack.URL == "" {
		return fmt.Errorf("runtime pack %s has no download URL", pack.Name)
	}
	switch pack.Format {
	case FormatZIP, FormatTarGZ, FormatFile:
		return nil
	default:
		return fmt.Errorf("runtime pack %s has unsupported format %q", pack.Name, pack.Format)
	}
}

func validatePackIdentity(pack Pack) error {
	for label, value := range map[string]string{
		"name": pack.Name, "version": pack.Version, "OS": pack.OS, "architecture": pack.Arch,
	} {
		if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\:`) {
			return fmt.Errorf("runtime pack has an invalid %s %q", label, value)
		}
	}
	return nil
}

type progressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	report     func(Progress)
}

func (reader *progressReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	reader.downloaded += int64(count)
	reader.report(Progress{Downloaded: reader.downloaded, Total: reader.total})
	return count, err
}

func unpack(format Format, payloadPath, destination, executable string) error {
	switch format {
	case FormatZIP:
		return unpackZIP(payloadPath, destination)
	case FormatTarGZ:
		return unpackTarGZ(payloadPath, destination)
	case FormatFile:
		if executable == "" {
			executable = "runtime"
		}
		return copyRuntimeFile(payloadPath, filepath.Join(destination, filepath.Base(executable)), 0o700)
	default:
		return fmt.Errorf("unsupported archive format %q", format)
	}
}

func unpackZIP(path, destination string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer func() { _ = archive.Close() }()
	for _, entry := range archive.File {
		target, err := archiveTarget(destination, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("archive entry %q is not a regular file", entry.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		err = copyRuntimeReader(source, target, entry.Mode().Perm())
		_ = source.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func unpackTarGZ(path, destination string) error {
	file, err := os.Open(path) // #nosec G304 -- path is the installer's private verified staging file.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = compressed.Close() }()
	archive := tar.NewReader(compressed)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := archiveTarget(destination, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, 0:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			if err := copyRuntimeReader(archive, target, os.FileMode(header.Mode&0o777)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("archive entry %q has unsupported type", header.Name)
		}
	}
}

func archiveTarget(root, name string) (string, error) {
	portable := strings.ReplaceAll(name, `\`, "/")
	clean := filepath.Clean(filepath.FromSlash(portable))
	if clean == "." || filepath.IsAbs(clean) {
		return "", fmt.Errorf("archive entry %q has an unsafe path", name)
	}
	target := filepath.Join(root, clean)
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q escapes the runtime directory", name)
	}
	return target, nil
}

func copyRuntimeFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source) // #nosec G304 -- source is the installer's private verified staging file.
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	return copyRuntimeReader(input, destination, mode)
}

func copyRuntimeReader(source io.Reader, destination string, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode) // #nosec G304 -- destination is confined below the installer's private staging directory.
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, source)
	syncErr := output.Sync()
	closeErr := output.Close()
	return errors.Join(copyErr, syncErr, closeErr)
}
