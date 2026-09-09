package identity

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestPerceptualFingerprintSurvivesResizeAndRecompression(t *testing.T) {
	base := patternedImage(72, 64, false)
	resized := patternedImage(180, 160, false)
	basePNG := encodePNG(t, base)
	resizedJPEG := encodeJPEG(t, resized)

	first, err := FingerprintImage(basePNG, "https://i.ss.lv/base.png", "image/png", 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := FingerprintImage(resizedJPEG, "https://i.ss.lv/resized.jpg", "image/jpeg", 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExactHash == second.ExactHash {
		t.Fatal("recompressed image unexpectedly had the same exact hash")
	}
	if first.PerceptualHash != second.PerceptualHash {
		t.Fatalf("perceptual hashes differ: %s != %s", first.PerceptualHash, second.PerceptualHash)
	}
}

func TestExactFingerprintMatchesIdenticalContent(t *testing.T) {
	content := encodePNG(t, patternedImage(72, 64, false))
	first, err := FingerprintImage(content, "https://i.ss.lv/a.png", "image/png", 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := FingerprintImage(append([]byte(nil), content...), "https://i.ss.lv/b.png", "image/png", 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExactHash != second.ExactHash || first.PerceptualHash != second.PerceptualHash {
		t.Fatalf("identical content hashes differ: %+v / %+v", first, second)
	}
}

func TestPerceptualFingerprintDistinguishesDifferentImage(t *testing.T) {
	first, err := FingerprintImage(encodePNG(t, patternedImage(72, 64, false)), "https://i.ss.lv/a.png", "image/png", 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := FingerprintImage(encodePNG(t, patternedImage(72, 64, true)), "https://i.ss.lv/b.png", "image/png", 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.PerceptualHash == second.PerceptualHash {
		t.Fatalf("different images had hash %s", first.PerceptualHash)
	}
}

func TestPhotoDownloadEnforcesHostRedirectTypeSizeAndLimit(t *testing.T) {
	content := encodePNG(t, patternedImage(72, 64, false))
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return imageResponse(request, content), nil
	})}
	urls := make([]string, 10)
	for index := range urls {
		urls[index] = "https://i.ss.lv/gallery/photo-" + string(rune('a'+index)) + ".png"
	}
	photos, err := FingerprintPhotos(context.Background(), client, "ss.lv", urls, 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(photos) != DefaultPhotoLimit || calls.Load() != DefaultPhotoLimit {
		t.Fatalf("downloaded %d photos in %d calls", len(photos), calls.Load())
	}

	if _, err := FingerprintPhotos(context.Background(), client, "ss.lv", []string{"https://evil.example/photo.png"}, 1); err == nil {
		t.Fatal("disallowed host was accepted")
	}

	redirectClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://evil.example/photo.png"}}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}
	if _, err := FingerprintPhotos(context.Background(), redirectClient, "ss.lv", []string{"https://i.ss.lv/photo.png"}, 1); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("redirect error = %v", err)
	}

	typeClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := imageResponse(request, content)
		response.Header.Set("Content-Type", "text/html")
		return response, nil
	})}
	if _, err := FingerprintPhotos(context.Background(), typeClient, "ss.lv", []string{"https://i.ss.lv/photo.png"}, 1); err == nil {
		t.Fatal("non-image response was accepted")
	}

	sizeClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response := imageResponse(request, nil)
		response.Header.Set("Content-Length", "10485761")
		return response, nil
	})}
	if _, err := FingerprintPhotos(context.Background(), sizeClient, "ss.lv", []string{"https://i.ss.lv/photo.png"}, 1); err == nil {
		t.Fatal("oversize response was accepted")
	}
}

func TestFingerprintPhotosReusesUnchangedURLsAndDownloadsChangedURLs(t *testing.T) {
	content := encodePNG(t, patternedImage(72, 64, false))
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return imageResponse(request, content), nil
	})}
	reusedURL := "https://i.ss.lv/gallery/reused.png"
	changedURL := "https://i.ss.lv/gallery/changed.png"
	reusable := []domain.PhotoFingerprint{{
		Position:            7,
		SourceURL:           reusedURL,
		ExactHash:           strings.Repeat("a", 64),
		ExactAlgorithm:      ExactAlgorithm,
		PerceptualHash:      strings.Repeat("b", 16),
		PerceptualAlgorithm: PerceptualAlgorithm,
		MediaType:           "image/png",
		ByteSize:            100,
		Width:               72,
		Height:              64,
	}}

	photos, err := FingerprintPhotosWithReuse(context.Background(), client, "ss.lv", []string{reusedURL, changedURL}, 8, reusable)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(photos) != 2 {
		t.Fatalf("network calls=%d photos=%d", calls.Load(), len(photos))
	}
	if photos[0].SourceURL != reusedURL || photos[0].Position != 0 || photos[0].ExactHash != reusable[0].ExactHash {
		t.Fatalf("reused fingerprint=%+v", photos[0])
	}
	if photos[1].SourceURL != changedURL || photos[1].Position != 1 || photos[1].ExactHash == reusable[0].ExactHash {
		t.Fatalf("downloaded fingerprint=%+v", photos[1])
	}
}

func TestPhotoDownloadPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	_, err := FingerprintPhotos(ctx, client, "ss.lv", []string{"https://i.ss.lv/photo.png"}, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func patternedImage(width, height int, reverse bool) image.Image {
	result := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value := uint8(x * 255 / max(width-1, 1))
			if reverse {
				value = 255 - value
			}
			if y > height/2 {
				value /= 2
			}
			result.SetGray(x, y, color.Gray{Y: value})
		}
	}
	return result
}

func encodePNG(t *testing.T, value image.Image) []byte {
	t.Helper()
	var result bytes.Buffer
	if err := png.Encode(&result, value); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func encodeJPEG(t *testing.T, value image.Image) []byte {
	t.Helper()
	var result bytes.Buffer
	if err := jpeg.Encode(&result, value, &jpeg.Options{Quality: 78}); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func imageResponse(request *http.Request, content []byte) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"image/png"}},
		Body:       io.NopCloser(bytes.NewReader(content)),
		Request:    request,
	}
}
