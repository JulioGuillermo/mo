package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	pdfMaxImagePages = 20
	pdfImageDPI      = "100"
)

type pageRange struct{ first, last int }

func isPDF(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".pdf")
}

// readPDF gives the pages of a PDF as text, a section per page, or as
// image markers, one per page.
func readPDF(ctx context.Context, path, as, pages string) (string, error) {
	total, err := pdfPageCount(ctx, path)
	if err != nil {
		return "", err
	}
	wanted, err := parsePages(pages, total)
	if err != nil {
		return "", err
	}
	switch as {
	case "", "text":
		return pdfText(ctx, path, wanted)
	case "image":
		return pdfImages(ctx, path, wanted, total)
	}
	return "", fmt.Errorf("as is text or image, not %q", as)
}

func pdfPageCount(ctx context.Context, path string) (int, error) {
	output, err := runPoppler(ctx, "pdfinfo", path)
	if err != nil {
		return 0, err
	}
	for line := range strings.SplitSeq(output, "\n") {
		if count, found := strings.CutPrefix(line, "Pages:"); found {
			return strconv.Atoi(strings.TrimSpace(count))
		}
	}
	return 0, errors.New("pdfinfo did not say how many pages the PDF has")
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

func pdfText(ctx context.Context, path string, wanted pageRange) (string, error) {
	output, err := runPoppler(ctx, "pdftotext", "-layout", "-f", strconv.Itoa(wanted.first), "-l", strconv.Itoa(wanted.last), path, "-")
	if err != nil {
		return "", err
	}
	pageTexts := strings.Split(strings.TrimSuffix(output, "\f"), "\f")
	var text strings.Builder
	for i, pageText := range pageTexts {
		pageText = strings.TrimSpace(pageText)
		if pageText == "" {
			pageText = "(no text on this page: a scan or a picture; read it with as image)"
		}
		fmt.Fprintf(&text, "## Page %d\n\n%s\n\n", wanted.first+i, pageText)
	}
	return strings.TrimSuffix(text.String(), "\n"), nil
}

// pdfImages draws each page wanted as a PNG in mo's cache, kept for the
// next read of the same PDF.
func pdfImages(ctx context.Context, path string, wanted pageRange, total int) (string, error) {
	if count := wanted.last - wanted.first + 1; count > pdfMaxImagePages {
		return "", fmt.Errorf("%d pages are too many images for one read: ask for at most %d with pages, like 1-%d", count, pdfMaxImagePages, pdfMaxImagePages)
	}
	dir, err := pdfCacheDir(path)
	if err != nil {
		return "", err
	}
	var markers []string
	for page := wanted.first; page <= wanted.last; page++ {
		pagePath, err := pdfPageImage(ctx, path, dir, page)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(pagePath)
		if err != nil {
			return "", err
		}
		marker, err := describeImage(pagePath, info.Size())
		if err != nil {
			return "", err
		}
		markers = append(markers, fmt.Sprintf("page %d of %d: %s", page, total, marker))
	}
	return strings.Join(markers, "\n"), nil
}

func pdfPageImage(ctx context.Context, path, dir string, page int) (string, error) {
	base := filepath.Join(dir, fmt.Sprintf("page-%d", page))
	pagePath := base + ".png"
	if _, err := os.Stat(pagePath); err == nil {
		return pagePath, nil
	}
	number := strconv.Itoa(page)
	_, err := runPoppler(ctx, "pdftoppm", "-png", "-r", pdfImageDPI, "-f", number, "-l", number, "-singlefile", path, base)
	return pagePath, err
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

func runPoppler(ctx context.Context, program string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, program, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("reading a PDF needs %s, from poppler: install poppler-utils (or poppler)", program)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %s", program, strings.TrimSpace(stderr.String()+" "+err.Error()))
	}
	return string(output), nil
}
