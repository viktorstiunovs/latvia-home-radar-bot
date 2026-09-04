package telegram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"

	"github.com/clive00lewis/latvia-home-radar/internal/domain"
)

const maxPhotoBytes = 10 * 1024 * 1024

func DownloadPhotos(ctx context.Context, client *http.Client, listing domain.Listing, logger *slog.Logger) []Photo {
	urls := listing.PhotoURLs
	if len(urls) > 10 {
		urls = urls[:10]
	}
	if len(urls) == 0 && listing.ImageURL != "" {
		urls = []string{listing.ImageURL}
	}
	var result []Photo
	for index, rawURL := range urls {
		photo, err := downloadPhoto(ctx, client, rawURL, listing.ExternalID, index+1)
		if err != nil {
			logger.Warn("photo download failed", "listing", listing.ExternalID, "error", err)
			continue
		}
		if photo != nil {
			result = append(result, *photo)
		}
	}
	return result
}

func downloadPhoto(ctx context.Context, client *http.Client, rawURL, listingID string, index int) (*Photo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("photo HTTP %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); len(contentType) < 6 || contentType[:6] != "image/" {
		return nil, nil
	}
	if size := response.Header.Get("Content-Length"); size != "" {
		value, err := strconv.ParseInt(size, 10, 64)
		if err == nil && value > maxPhotoBytes {
			return nil, nil
		}
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maxPhotoBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxPhotoBytes {
		return nil, nil
	}
	parsed, _ := url.Parse(rawURL)
	filename := path.Base(parsed.Path)
	if filename == "" || filename == "." || filename == "/" {
		filename = fmt.Sprintf("%s-%d.jpg", listingID, index)
	}
	return &Photo{Content: content, Filename: filename}, nil
}
