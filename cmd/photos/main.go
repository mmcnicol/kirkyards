// Command photos prepares the stone photographs for the web from the 2013
// backup zip. For every photo referenced in data/, it writes:
//
//	<out>/<imageDir>/<photo>         web size (default 1200px on the long side)
//	<out>/thumbs/<imageDir>/<photo>  thumbnail (default 400px)
//
// Both are made from the full-size original where the zip has one, and
// re-encoded, which strips camera metadata. Existing outputs are skipped, so
// an interrupted run can be resumed.
//
// Usage:
//
//	go run ./cmd/photos -zip InscriptionImages_new.zip -out photos
package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/image/draw"

	"github.com/mmcnicol/kirkyards/internal/archive"
)

const resourcesPrefix = "InscriptionImages/resources/"

type job struct {
	place *archive.Place
	rel   string
}

func main() {
	zipPath := flag.String("zip", "", "path to InscriptionImages zip")
	dataDir := flag.String("data", "data", "data directory written by cmd/import")
	outDir := flag.String("out", "photos", "output directory for web photos and thumbnails")
	webSize := flag.Int("web", 1200, "long side of web photos, in pixels")
	thumbSize := flag.Int("thumb", 400, "long side of thumbnails, in pixels")
	quality := flag.Int("quality", 82, "JPEG quality")
	flag.Parse()
	if *zipPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	a, err := archive.Load(os.DirFS(*dataDir))
	if err != nil {
		log.Fatal(err)
	}
	zr, err := zip.OpenReader(*zipPath)
	if err != nil {
		log.Fatal(err)
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}

	var jobs []job
	seen := map[string]bool{}
	for _, p := range a.Places {
		add := func(rel string) {
			if key := p.ImageDir + "/" + rel; !seen[key] {
				seen[key] = true
				jobs = append(jobs, job{p, rel})
			}
		}
		for _, rel := range p.Photos {
			add(rel)
		}
		for _, s := range a.Stones(p.Slug) {
			for _, rel := range s.Photos() {
				add(rel)
			}
		}
	}

	var done, skipped, missing, failed atomic.Int64
	ch := make(chan job)
	var wg sync.WaitGroup
	// The zip reader is safe for concurrent Open calls on different files.
	for range runtime.NumCPU() {
		wg.Go(func() {
			for j := range ch {
				switch err := process(files, j, *outDir, *webSize, *thumbSize, *quality); {
				case err == errSkipped:
					skipped.Add(1)
				case err == errMissing:
					missing.Add(1)
				case err != nil:
					failed.Add(1)
					log.Printf("%s/%s: %v", j.place.ImageDir, j.rel, err)
				}
				if n := done.Add(1); n%100 == 0 {
					fmt.Printf("%d/%d\n", n, len(jobs))
				}
			}
		})
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	fmt.Printf("%d photos: %d written, %d already done, %d missing from zip, %d failed\n",
		len(jobs), len(jobs)-int(skipped.Load()+missing.Load()+failed.Load()), skipped.Load(), missing.Load(), failed.Load())
	if failed.Load() > 0 {
		os.Exit(1)
	}
}

var (
	errSkipped = fmt.Errorf("already done")
	errMissing = fmt.Errorf("not in zip")
)

func process(files map[string]*zip.File, j job, outDir string, webSize, thumbSize, quality int) error {
	webOut := filepath.Join(outDir, j.place.ImageDir, j.rel)
	thumbOut := filepath.Join(outDir, "thumbs", j.place.ImageDir, j.rel)
	if exists(webOut) && exists(thumbOut) {
		return errSkipped
	}

	// Originals sit beside the web copies: monument/image/X.JPG -> monument/image_orig/X.JPG.
	base := resourcesPrefix + j.place.ImageDir + "/"
	origName := base + strings.Replace(j.rel, "image/", "image_orig/", 1)
	src := files[origName]
	if src == nil {
		src = files[base+j.rel]
	}
	if src == nil {
		return errMissing
	}
	raw, err := readZipFile(src)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	for _, out := range []struct {
		name string
		size int
	}{{webOut, webSize}, {thumbOut, thumbSize}} {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, fit(img, out.size), &jpeg.Options{Quality: quality}); err != nil {
			return err
		}
		if err := writeFile(out.name, buf.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

// fit scales img down so its long side is at most size. It never enlarges.
func fit(img image.Image, size int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= size && h <= size {
		return img
	}
	if w >= h {
		w, h = size, h*size/w
	} else {
		w, h = w*size/h, size
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// writeFile writes via a temporary file so an interrupted run never leaves
// a truncated photo that a resumed run would skip.
func writeFile(name string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	tmp := name + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, name)
}

func exists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}
