// Package spa implements a tiny little single-page app HTTP handler
package spa

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	defaultWebpath    = "/index.html"
	tcpPacketDataSize = 1460
)

// NewHandler creates a new [http.Handler] that serves out of dir
func NewHandler(dir string) (http.Handler, error) {
	slog.Debug("spa: initializing handler")

	cache, err := appendDirEntries(nil, "/", dir)
	if err != nil {
		return nil, err
	}

	ret := handler{cache: make(map[string]cacheEntry, len(cache))}
	for _, entry := range cache {
		ret.cache[entry.urlpath] = entry
	}

	if _, ok := ret.cache[defaultWebpath]; !ok {
		return nil, errors.New("spa: root " + defaultWebpath + " not found")
	}

	return ret, nil
}

type handler struct {
	cache map[string]cacheEntry
}

// ServeHTTP implements [http.Handler]
func (h handler) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	originalPath := r.URL.Path

	p := r.URL.Path
	if !path.IsAbs(p) {
		p = "/" + p
	}

	p = path.Clean(p)

	entry, ok := h.cache[p]
	if !ok {
		p = "/index.html"
		entry, ok = h.cache[p]
	}

	if !ok {
		wr.WriteHeader(http.StatusInternalServerError)
		return
	}
	slog.Debug(fmt.Sprintf("spa: request for %s (original: %s", p, originalPath))

	if entry.gzipHandler != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		entry.gzipHandler(wr)
		return
	}

	entry.identityHandler(wr)
}

type cacheEntry struct {
	// path (as seen in the [http.Request]'s URL.Path field)
	urlpath string
	// mime type of this cached entry
	contentType string

	// size (in bytes) of content served by identityHandler
	identitySize int
	// handler that serves the content uncompressed
	identityHandler func(wr http.ResponseWriter)

	// true if this content should attempt to use a compressed encoding.
	// note - the caller must still consult the client's Accept-Encoding values
	shouldServeCompressed bool
	// size (in bytes) of content served by gzipHandler
	// will be 0 if shouldServeCompressed is false
	compressedSize int
	// handler that serves the content compressed
	// will be nil if shouldServeCompressed is false
	gzipHandler func(wr http.ResponseWriter)
}

// Implements [http.Handler]
func (ce cacheEntry) ServeHTTP(wr http.ResponseWriter, r *http.Request) {
	if ce.shouldServeCompressed && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		ce.gzipHandler(wr)
		return
	}

	ce.identityHandler(wr)
}

// appends (and returns) cacheEntrys to found at fpath to slice
func appendDirEntries(slice []cacheEntry, urlpath string, fpath string) ([]cacheEntry, error) {
	slog.Debug(fmt.Sprintf("spa: reading directory: %s", fpath))

	dirEntries, err := os.ReadDir(fpath)
	if err != nil {
		return nil, fmt.Errorf("spa: failed to read directory %s: %w", fpath, err)
	}

	for _, dirEntry := range dirEntries {
		name := dirEntry.Name()
		if strings.HasPrefix(name, ".") {
			slog.Debug(fmt.Sprintf("spa: skipping file: %s", filepath.Join(fpath, name)))
			continue
		}

		if strings.HasPrefix(name, "_") {
			slog.Debug(fmt.Sprintf("spa: skipping file: %s", filepath.Join(fpath, name)))
			continue
		}

		subFpath := filepath.Join(fpath, name)
		subUpath := path.Join(urlpath, name)
		if dirEntry.IsDir() {
			slice, err = appendDirEntries(slice, subUpath, subFpath)
			if err != nil {
				return nil, err
			}
			continue
		}

		slice, err = appendFileEntry(slice, subUpath, subFpath)
		if err != nil {
			return nil, err
		}
	}

	return slice, nil
}

// appends (and returns) cacheEntrys to found at fpath to slice
func appendFileEntry(slice []cacheEntry, urlpath string, fpath string) ([]cacheEntry, error) {
	slog.Debug(fmt.Sprintf("spa: found file: %s", fpath))

	f, err := os.Open(fpath)
	if err != nil {
		return nil, fmt.Errorf("spa: failed to open %s: %w", fpath, err)
	}
	defer f.Close()

	ext := filepath.Ext(fpath)
	ct := mime.TypeByExtension(ext)

	bs, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("spa: failed to read %s: %w", fpath, err)
	}

	ce := cacheEntry{
		urlpath:        urlpath,
		contentType:    ct,
		identitySize:   len(bs),
		compressedSize: -1,
	}

	ce.identityHandler = func(wr http.ResponseWriter) {
		wr.Header().Add("Content-Type", ce.contentType)
		_, err := io.Copy(wr, bytes.NewReader(bs))
		if err != nil {
			slog.Error("spa: error serving %s: %w", ce.urlpath, err)
			wr.WriteHeader(http.StatusInternalServerError)
			return
		}

		wr.WriteHeader(http.StatusOK)
	}

	var gbsb bytes.Buffer
	err = func() error {
		wr, err := gzip.NewWriterLevel(&gbsb, gzip.BestCompression)
		if err != nil {
			return fmt.Errorf("spa: error creating gzip compressor: %w", err)
		}
		defer wr.Close()

		_, err = io.Copy(wr, bytes.NewReader(bs))
		if err != nil {
			return fmt.Errorf("spa: error writing gzipped content: %w", err)
		}

		return nil
	}()
	if err != nil {
		return nil, err
	}

	gbs := gbsb.Bytes()
	compressedSize := len(gbs)
	ce.shouldServeCompressed = (ce.identitySize / tcpPacketDataSize) > (ce.compressedSize / tcpPacketDataSize)

	if contentTypeIsAlreadyCompressed(ce.contentType) {
		ce.shouldServeCompressed = false
	}

	if ce.shouldServeCompressed {
		ce.compressedSize = compressedSize
		ce.gzipHandler = func(wr http.ResponseWriter) {
			wr.Header().Add("Content-Type", ce.contentType)
			wr.Header().Add("Content-Encoding", "gzip")
			_, err := io.Copy(wr, bytes.NewReader(gbs))
			if err != nil {
				slog.Error("spa: error serving %s (gzipped): %w", ce.urlpath, err)
				wr.WriteHeader(http.StatusInternalServerError)
				return
			}

			wr.WriteHeader(http.StatusOK)
		}
	}

	slog.Info(fmt.Sprintf("spa: cached file %s (%s) (%d bytes, %d compressed)", ce.urlpath, ce.contentType, ce.identitySize, ce.compressedSize))
	return append(slice, ce), nil
}

// Reports whether the content type supports compression as part of its encoding.
// This can be used to prevent double-compressing content.
//
// TODO(a-jentleman) this list is not complete - if fact, it is not even close
func contentTypeIsAlreadyCompressed(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "audio/mpeg", "video/mp4":
		return true
	default:
		return false
	}
}
