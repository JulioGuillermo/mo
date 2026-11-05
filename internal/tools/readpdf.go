package tools

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdfkit/extract"
	"github.com/go-pdfkit/reader"
	"github.com/go-pdfkit/render"
)

const (
	pdfMaxImagePages = 20
	pdfImageDPI      = 100
	pdfPageTimeout   = 20 * time.Second
)

type pageRange struct{ first, last int }

func isPDF(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

// readPDF gives the pages of a PDF as text, a section per page, or as
// image markers, one per page.
func readPDF(ctx context.Context, path, as, pages string) (output string, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			output, err = "", fmt.Errorf("the PDF could not be read: %v", failure)
		}
	}()
	if as != "" && as != "text" && as != "image" {
		return "", fmt.Errorf("as is text or image, not %q", as)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	document, err := reader.Open(data)
	if err != nil {
		return "", fmt.Errorf("not a PDF that can be read: %w", err)
	}
	total := document.PageCount()
	wanted, err := parsePages(pages, total)
	if err != nil {
		return "", err
	}
	if as == "image" {
		return pdfImages(ctx, path, document, wanted, total)
	}
	return pdfText(ctx, document, wanted)
}

// parsePages reads "5" or "2-7"; empty is every page.
func parsePages(pages string, total int) (pageRange, error) {
	if pages == "" {
		return pageRange{1, total}, nil
	}
	firstText, lastText, isRange := strings.Cut(pages, "-")
	if !isRange {
		lastText = firstText
	}
	first, firstErr := strconv.Atoi(strings.TrimSpace(firstText))
	last, lastErr := strconv.Atoi(strings.TrimSpace(lastText))
	if firstErr != nil || lastErr != nil || first < 1 || last < first {
		return pageRange{}, fmt.Errorf("pages is a page or a range like 2-7, not %q", pages)
	}
	if first > total {
		return pageRange{}, fmt.Errorf("the PDF has %d pages", total)
	}
	return pageRange{first, min(last, total)}, nil
}

func pdfText(ctx context.Context, document *reader.Document, wanted pageRange) (string, error) {
	var text strings.Builder
	for page := wanted.first; page <= wanted.last; page++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		pageText, err := extract.Text(document, page)
		if err != nil {
			return "", fmt.Errorf("page %d: %w", page, err)
		}
		pageText = strings.TrimSpace(pageText)
		if pageText == "" {
			pageText = "(no text on this page: a scan or a picture; read it with as image)"
		}
		fmt.Fprintf(&text, "## Page %d\n\n%s\n\n", page, pageText)
	}
	return strings.TrimSuffix(text.String(), "\n"), nil
}

// pdfImages draws each page wanted as a PNG in mo's cache, kept for the
// next read of the same PDF.
func pdfImages(ctx context.Context, path string, document *reader.Document, wanted pageRange, total int) (string, error) {
	if count := wanted.last - wanted.first + 1; count > pdfMaxImagePages {
		return "", fmt.Errorf("%d pages are too many images for one read: ask for at most %d with pages, like 1-%d", count, pdfMaxImagePages, pdfMaxImagePages)
	}
	dir, err := pdfCacheDir(path)
	if err != nil {
		return "", err
	}
	var markers []string
	for page := wanted.first; page <= wanted.last; page++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		pagePath, partial, err := pdfPageImage(document, dir, page)
		if err != nil {
			return "", fmt.Errorf("page %d: %w", page, err)
		}
		info, err := os.Stat(pagePath)
		if err != nil {
			return "", err
		}
		marker, err := describeImage(pagePath, info.Size())
		if err != nil {
			return "", err
		}
		if partial {
			marker += " (drawn only in part: the page took too long)"
		}
		markers = append(markers, fmt.Sprintf("page %d of %d: %s", page, total, marker))
	}
	return strings.Join(markers, "\n"), nil
}

// pdfPageImage gives the PNG of a page, drawing it unless the cache has it;
// partial tells a page drawn only as far as its time allowed, not kept.
func pdfPageImage(document *reader.Document, dir string, page int) (string, bool, error) {
	pagePath := filepath.Join(dir, fmt.Sprintf("page-%d.png", page))
	if _, err := os.Stat(pagePath); err == nil {
		return pagePath, false, nil
	}
	picture, err := render.Page(document, page, render.Options{DPI: pdfImageDPI, MaxDuration: pdfPageTimeout})
	partial := errors.Is(err, render.ErrTimedOut)
	if err != nil && !partial {
		return "", false, err
	}
	if partial {
		pagePath = filepath.Join(dir, fmt.Sprintf("page-%d-partial.png", page))
	}
	return pagePath, partial, writePNG(pagePath, picture.ToNRGBA())
}

func writePNG(path string, picture image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, picture); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// pdfCacheDir is a directory for the pages of this version of the PDF: a
// changed file gets a new one.
func pdfCacheDir(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	version := fmt.Sprintf("%s\x00%d\x00%d", path, info.Size(), info.ModTime().UnixNano())
	name := fmt.Sprintf("%x", sha256.Sum256([]byte(version)))[:16]
	dir := filepath.Join(cache, "mo", "pdf", name)
	return dir, os.MkdirAll(dir, 0o700)
}
