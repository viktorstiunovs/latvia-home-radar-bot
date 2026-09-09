package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
	_ "golang.org/x/image/webp"
)

const (
	ExactAlgorithm      = "sha256-v1"
	PerceptualAlgorithm = "dhash-64-v1"
	DefaultPhotoLimit   = 8
	MaxPhotoBytes       = 10 * 1024 * 1024
	MaxPhotoPixels      = 40 * 1000 * 1000
	photoTimeout        = 15 * time.Second
)

var photoHosts = map[string]map[string]bool{
	"ss.lv": {
		"i.ss.lv":  true,
		"i.ss.com": true,
	},
	"city24.lv": {
		"static.img-city24.lv": true,
	},
}

func FingerprintPhotos(ctx context.Context, client *http.Client, source string, rawURLs []string, limit int) ([]domain.PhotoFingerprint, error) {
	return FingerprintPhotosWithReuse(ctx, client, source, rawURLs, limit, nil)
}

func FingerprintPhotosWithReuse(ctx context.Context, client *http.Client, source string, rawURLs []string, limit int, reusable []domain.PhotoFingerprint) ([]domain.PhotoFingerprint, error) {
	if limit <= 0 || limit > DefaultPhotoLimit {
		limit = DefaultPhotoLimit
	}
	urls := uniqueURLs(rawURLs, limit)
	cache := reusablePhotoCache(reusable)
	result := make([]domain.PhotoFingerprint, 0, len(urls))
	for position, rawURL := range urls {
		if fingerprint, ok := cache[rawURL]; ok {
			fingerprint.Position = position
			result = append(result, fingerprint)
			continue
		}
		fingerprint, err := downloadAndFingerprint(ctx, client, source, rawURL, position)
		if err != nil {
			return nil, fmt.Errorf("fingerprint photo %d: %w", position, err)
		}
		result = append(result, fingerprint)
	}
	return result, nil
}

func reusablePhotoCache(values []domain.PhotoFingerprint) map[string]domain.PhotoFingerprint {
	result := make(map[string]domain.PhotoFingerprint, len(values))
	for _, value := range values {
		if value.SourceURL == "" || value.ExactAlgorithm != ExactAlgorithm || value.PerceptualAlgorithm != PerceptualAlgorithm {
			continue
		}
		if _, exists := result[value.SourceURL]; !exists {
			result[value.SourceURL] = value
		}
	}
	return result
}

func FingerprintImage(content []byte, rawURL, mediaType string, position int) (domain.PhotoFingerprint, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return domain.PhotoFingerprint{}, fmt.Errorf("decode image config: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > MaxPhotoPixels/config.Height {
		return domain.PhotoFingerprint{}, fmt.Errorf("image exceeds %d pixels", MaxPhotoPixels)
	}
	decoded, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return domain.PhotoFingerprint{}, fmt.Errorf("decode image: %w", err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return domain.PhotoFingerprint{}, fmt.Errorf("image has invalid dimensions")
	}
	exact := sha256.Sum256(content)
	return domain.PhotoFingerprint{
		Position:            position,
		SourceURL:           rawURL,
		ExactHash:           hex.EncodeToString(exact[:]),
		ExactAlgorithm:      ExactAlgorithm,
		PerceptualHash:      differenceHash(decoded),
		PerceptualAlgorithm: PerceptualAlgorithm,
		MediaType:           mediaType,
		ByteSize:            len(content),
		Width:               bounds.Dx(),
		Height:              bounds.Dy(),
	}, nil
}

func downloadAndFingerprint(ctx context.Context, client *http.Client, source, rawURL string, position int) (domain.PhotoFingerprint, error) {
	if !allowedPhotoURL(source, rawURL) {
		return domain.PhotoFingerprint{}, fmt.Errorf("photo URL is not allowed for %s", source)
	}
	if client == nil {
		client = http.DefaultClient
	}
	safeClient := *client
	previousRedirect := safeClient.CheckRedirect
	safeClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if !allowedPhotoURL(source, request.URL.String()) {
			return fmt.Errorf("photo redirect is not allowed for %s", source)
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many photo redirects")
		}
		return nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, photoTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return domain.PhotoFingerprint{}, err
	}
	response, err := safeClient.Do(request)
	if err != nil {
		return domain.PhotoFingerprint{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.PhotoFingerprint{}, fmt.Errorf("photo HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "image/") {
		return domain.PhotoFingerprint{}, fmt.Errorf("photo has unsupported content type")
	}
	if rawLength := response.Header.Get("Content-Length"); rawLength != "" {
		length, parseErr := strconv.ParseInt(rawLength, 10, 64)
		if parseErr == nil && length > MaxPhotoBytes {
			return domain.PhotoFingerprint{}, fmt.Errorf("photo exceeds %d bytes", MaxPhotoBytes)
		}
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, MaxPhotoBytes+1))
	if err != nil {
		return domain.PhotoFingerprint{}, err
	}
	if len(content) > MaxPhotoBytes {
		return domain.PhotoFingerprint{}, fmt.Errorf("photo exceeds %d bytes", MaxPhotoBytes)
	}
	return FingerprintImage(content, rawURL, mediaType, position)
}

func allowedPhotoURL(source, rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return false
	}
	hosts := photoHosts[source]
	return hosts != nil && hosts[strings.ToLower(parsed.Hostname())]
}

func uniqueURLs(values []string, limit int) []string {
	result := make([]string, 0, min(len(values), limit))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
		if len(result) == limit {
			break
		}
	}
	return result
}

func differenceHash(value image.Image) string {
	bounds := value.Bounds()
	var hash uint64
	for y := range 8 {
		for x := range 8 {
			left := luminance(sample(value, bounds, x, y, 9, 8))
			right := luminance(sample(value, bounds, x+1, y, 9, 8))
			hash <<= 1
			if left > right {
				hash |= 1
			}
		}
	}
	return fmt.Sprintf("%016x", hash)
}

func sample(value image.Image, bounds image.Rectangle, x, y, width, height int) colorValue {
	px := bounds.Min.X + x*(bounds.Dx()-1)/max(width-1, 1)
	py := bounds.Min.Y + y*(bounds.Dy()-1)/max(height-1, 1)
	r, g, b, _ := value.At(px, py).RGBA()
	return colorValue{r: r, g: g, b: b}
}

type colorValue struct {
	r uint32
	g uint32
	b uint32
}

func luminance(value colorValue) uint64 {
	return uint64(value.r)*299 + uint64(value.g)*587 + uint64(value.b)*114
}
