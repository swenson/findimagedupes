// Copyright (c) 2023 Christopher Swenson
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"io/fs"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"slices"
	"strings"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

type fingerprint [32]byte

var (
	thresholdFlag  = flag.Float64("threshold", 10.0, "percentage match for threshold")
	verboseFlag    = flag.Bool("verbose", false, "verbose")
	extensionsFlag = flag.String("extensions", "jpg,jpeg,gif,png", "file extensions to consider, comma-separated")
)

var zeroFingerprint = fingerprint([32]byte{})

// diffbits counts the number of bits that the two fingerprints differ by
func (a fingerprint) diffbits(b fingerprint) int {
	x := 0
	for i := 0; i < 32; i++ {
		x += bits.OnesCount8(a[i] ^ b[i])
	}
	return x
}

// resample resizes the image using nearest-neighbor so that additional colors are not introduced.
func resample(im image.Image, cols, rows int) image.Image {
	w := im.Bounds().Size().X
	h := im.Bounds().Size().Y
	newim := image.NewRGBA(image.Rect(0, 0, cols, rows))
	for x := 0; x < cols; x++ {
		for y := 0; y < rows; y++ {
			c := im.At(int(math.Round(float64(x*w)/float64(cols))),
				int(math.Round(float64(y*h)/float64(rows))))
			newim.Set(x, y, c)
		}
	}
	return newim
}

// resampleGray resamples grayscale images.
func resampleGray(im image.Image, cols, rows int) image.Image {
	if im.ColorModel() != color.GrayModel {
		panic("resampleGray only implemented for image.Gray")
	}
	gray := im.(*image.Gray)
	w := im.Bounds().Size().X
	h := im.Bounds().Size().Y
	newim := image.NewGray(image.Rect(0, 0, cols, rows))
	for x := 0; x < cols; x++ {
		for y := 0; y < rows; y++ {
			c := gray.GrayAt(int(math.Round(float64(x*w)/float64(cols))),
				int(math.Round(float64(y*h)/float64(rows))))
			newim.SetGray(x, y, c)
		}
	}
	return newim
}

// grayscale converts an image to grayscale.
func grayscale(im image.Image) image.Image {
	w := im.Bounds().Size().X
	h := im.Bounds().Size().Y
	newim := image.NewGray(im.Bounds())
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			c := im.At(x, y)
			r, g, b, _ := c.RGBA()
			gray := 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)
			newim.SetGray(x, y, color.Gray{Y: uint8(math.Round(gray / 65535.0 * 255.0))})
		}
	}
	return newim
}

// blur blurs each pixel with its 49 nearest neighbors using a simplified algorhtm
// that is mostly equivalent to gaussian blur with a high sigma.
func blur(im image.Image) image.Image {
	if im.ColorModel() != color.GrayModel {
		panic("normalize only implemented for image.Gray")
	}
	gray := im.(*image.Gray)
	const radius = 3

	w := im.Bounds().Size().X
	h := im.Bounds().Size().Y
	newim := image.NewGray(im.Bounds())
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			s := 0
			cy := 0
			for ai := -radius; ai <= radius; ai++ {
				a := x + ai
				if a < 0 || a >= w {
					continue
				}
				for bi := -radius; bi <= radius; bi++ {
					bb := y + bi
					if bb < 0 || bb >= h {
						continue
					}
					s++
					y := gray.GrayAt(a, bb).Y
					cy += int(y)
				}
			}
			newim.SetGray(x, y, color.Gray{Y: uint8(cy / s)})
		}
	}
	return newim
}

// normalize normalizes the contrast of the image.
func normalize(im image.Image) image.Image {
	if im.ColorModel() != color.GrayModel {
		panic("normalize only implemented for image.Gray")
	}
	gray := im.(*image.Gray)
	w := im.Bounds().Size().X
	h := im.Bounds().Size().Y
	newim := image.NewGray(im.Bounds())
	minVal := uint8(255)
	maxVal := uint8(0)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			c := gray.GrayAt(x, y)
			if c.Y < minVal {
				minVal = c.Y
			}
			if c.Y > maxVal {
				maxVal = c.Y
			}
		}
	}
	newMin := 0.02 * float64(minVal)
	newMax := 0.99 * float64(maxVal)
	scale := (newMax - newMin) / float64(maxVal-minVal)

	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			c := gray.GrayAt(x, y).Y
			cn := (float64(c)-float64(minVal))*scale + newMin
			cn = math.Round(cn)
			if cn < 0 {
				cn = 0.0
			} else if cn >= 255.0 {
				cn = 255.0
			}
			newim.Set(x, y, color.Gray{Y: uint8(cn)})
		}
	}

	return newim
}

// equalize adjusts the distribution of the pixel values to have an even histogram.
func equalize(im image.Image) image.Image {
	if im.ColorModel() != color.GrayModel {
		panic("equalize only implemented for image.Gray")
	}
	gray := im.(*image.Gray)
	w := im.Bounds().Size().X
	h := im.Bounds().Size().Y
	newim := image.NewGray(im.Bounds())
	cdf := make([]int, 256)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			c := gray.GrayAt(x, y).Y
			cdf[int(c)]++
		}
	}
	last := 0
	for i := 0; i < 256; i++ {
		if cdf[i] > 0 {
			cdf[i] += last
			last = cdf[i]
		}
	}
	mmin := w * h
	for _, x := range cdf {
		if x > 0 {
			if x < mmin {
				mmin = x
			}
		}
	}
	hh := make([]uint8, 256)
	for i := 0; i < 256; i++ {
		x := float64(cdf[i]-mmin) / (float64(w*h) - float64(mmin)) * 255.0
		x = math.Round(x)
		hh[i] = uint8(math.Max(0.0, math.Min(255.0, x)))
	}
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			c := gray.GrayAt(x, y).Y
			newim.SetGray(x, y, color.Gray{Y: hh[c]})
		}
	}
	return newim
}

// threshold does a basic 50/50 threshold to convert grayscale to monochrome.
func threshold(im image.Image) image.Image {
	if im.ColorModel() != color.GrayModel {
		panic("threshold only implemented for image.Gray")
	}
	gray := im.(*image.Gray)
	w := im.Bounds().Size().X
	h := im.Bounds().Size().Y
	newim := image.NewGray(im.Bounds())
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			c := gray.GrayAt(x, y).Y
			if c < 128 {
				newim.SetGray(x, y, color.Gray{Y: 0})
			} else {
				newim.SetGray(x, y, color.Gray{Y: 255})

			}
		}
	}
	return newim
}

// fingerprintImage computes a 256-bit monochrome reduction of an image
func fingerprintImage(name string) (fingerprint, error) {
	imf, err := os.Open(name)
	if err != nil {
		return zeroFingerprint, err
	}
	defer imf.Close()
	im, _, err := image.Decode(imf)
	if err != nil {
		return zeroFingerprint, err
	}
	im = resample(im, 160, 160)
	im = grayscale(im)
	im = blur(im)
	im = normalize(im)
	im = equalize(im)
	im = resampleGray(im, 16, 16)
	im = threshold(im)

	gray := im.(*image.Gray)
	data := [32]byte{}
	for y := 0; y < 16; y++ {
		for i := 0; i < 2; i++ {
			for j := 0; j < 8; j++ {
				if gray.GrayAt(i*8+j, y).Y < 128 {
					data[y*2+i] |= 1 << (7 - j)
				}
			}
		}
	}
	return data, nil
}

// findEquiv finds things in m that are equivalent to x. It is not very efficient.
func findEquiv(m map[int][]int, x int) []int {
	equiv := map[int]bool{}
	equiv[x] = true

	modified := true
	for modified {
		modified = false
		for k, v := range m {
			if equiv[k] {
				for _, vv := range v {
					if !equiv[vv] {
						equiv[vv] = true
						modified = true
					}
				}
			}
		}
	}
	var keys []int
	for k := range equiv {
		keys = append(keys, k)
	}
	return keys
}

func main() {
	flag.Parse()
	args := flag.Args()

	if len(args) == 0 {
		return
	}

	verbose := *verboseFlag

	extensions := strings.Split(*extensionsFlag, ",")
	for i := 0; i < len(extensions); i++ {
		extensions[i] = strings.ToLower(strings.TrimSpace(extensions[i]))
	}
	if verbose {
		fmt.Printf("Scanning for exentions: %s\n", strings.Join(extensions, " "))
	}

	var fingerprints []fingerprint
	var fingerprintPaths []string

	for _, arg := range args {
		if verbose {
			fmt.Printf("Scanning %s\n", arg)
		}

		_ = filepath.Walk(arg, func(path string, info fs.FileInfo, err error) error {
			if info.IsDir() {
				return nil
			}
			ext := strings.TrimPrefix(filepath.Ext(strings.ToLower(path)), ".")
			if slices.Contains(extensions, ext) {
				f, err := fingerprintImage(path)
				if err != nil {
					if err != nil {
						_, _ = fmt.Fprintf(os.Stderr, "Error decoding image %s; ignoring. %v\n", path, err)
					}
				}
				fingerprints = append(fingerprints, f)
				fingerprintPaths = append(fingerprintPaths, path)
			}
			return nil
		})
	}
	if verbose {
		fmt.Printf("Cross-matching %d files\n", len(fingerprints))
	}
	matches := map[int][]int{}
	thresholdBits := int(math.Round(256 * (*thresholdFlag / 100.0)))
	for i := 0; i < len(fingerprints); i++ {
		a := fingerprints[i]
		for j := i + 1; j < len(fingerprints); j++ {
			b := fingerprints[j]
			if a.diffbits(b) < thresholdBits {
				if _, ok := matches[i]; ok {
					matches[i] = append(matches[i], j)
				} else {
					matches[i] = []int{j}
				}
				if _, ok := matches[j]; ok {
					matches[j] = append(matches[j], i)
				} else {
					matches[j] = []int{i}
				}
			}
		}
	}
	for i := 0; i < len(fingerprints); i++ {
		if _, ok := matches[i]; !ok {
			continue
		}
		equiv := findEquiv(matches, i)
		var names []string
		for _, j := range equiv {
			names = append(names, fingerprintPaths[j])
			delete(matches, j)
		}
		fmt.Printf("Possible matches:\n%s\n", strings.Join(names, "\n"))
		fmt.Printf("\n")
	}
}
